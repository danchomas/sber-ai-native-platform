package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
)

type fakeRepo struct {
	dashboard *domain.Dashboard
	added     []domain.Widget
}

func (r *fakeRepo) Create(context.Context, *domain.Dashboard) error { return nil }
func (r *fakeRepo) Get(context.Context, uuid.UUID) (*domain.Dashboard, error) {
	if r.dashboard == nil {
		return nil, apperr.ErrNotFound
	}
	d := *r.dashboard
	d.Widgets = append([]domain.Widget(nil), r.dashboard.Widgets...)
	return &d, nil
}
func (r *fakeRepo) List(context.Context, int) ([]domain.Dashboard, error) { return nil, nil }
func (r *fakeRepo) Delete(context.Context, uuid.UUID) error               { return nil }
func (r *fakeRepo) AddWidget(_ context.Context, _ uuid.UUID, w *domain.Widget) error {
	r.added = append(r.added, *w)
	return nil
}
func (r *fakeRepo) DeleteWidget(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (r *fakeRepo) Rename(context.Context, uuid.UUID, string) error          { return nil }

type fakeCatalog struct{ datasets []domain.Dataset }

func newFakeCatalog() *fakeCatalog {
	minV, maxV := "2023-01-01", "2024-06-30 18:00:00+00"
	return &fakeCatalog{datasets: []domain.Dataset{{
		ID: uuid.New(), Name: "orders", TableName: "ds_orders", Status: domain.DatasetStatusReady,
		Origin: domain.OriginUploaded, Schema: "raw",
		Columns: []domain.Column{
			{Name: "created_at", Type: domain.ColumnTypeTimestamp, Role: domain.RoleTemporal,
				Profile: &domain.ColumnProfile{MinValue: &minV, MaxValue: &maxV}},
			{Name: "status", Type: domain.ColumnTypeText, Role: domain.RoleDimension},
			{Name: "amount", Type: domain.ColumnTypeNumeric, Role: domain.RoleMeasure},
		},
	}}}
}

func (c *fakeCatalog) List(context.Context) ([]domain.Dataset, error)       { return c.datasets, nil }
func (c *fakeCatalog) Relations(context.Context) ([]domain.Relation, error) { return nil, nil }
func (c *fakeCatalog) Schema() string                                       { return "raw" }
func (c *fakeCatalog) Scopes(context.Context) ([]domain.Scope, error) {
	return []domain.Scope{{Target: domain.QueryTarget{Schemas: []string{"raw"}}, Datasets: c.datasets}}, nil
}

// fakeExecutor вызывается из параллельного пересчёта виджетов.
type fakeExecutor struct {
	mu      sync.Mutex
	result  *domain.ResultSet
	err     error
	queries []string
	args    [][]any
}

func (e *fakeExecutor) Query(_ context.Context, _ domain.QueryTarget, sql string, _ int, args ...any) (*domain.ResultSet, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.queries = append(e.queries, sql)
	e.args = append(e.args, args)
	return e.result, e.err
}

func newTestService(repo *fakeRepo, exec *fakeExecutor, allowSQL bool) *Service {
	return New(repo, newFakeCatalog(), exec, config.Query{MaxRows: 1000}, allowSQL,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func monthly() *domain.ResultSet {
	month := func(m int) time.Time { return time.Date(2024, time.Month(m), 1, 0, 0, 0, 0, time.UTC) }
	return &domain.ResultSet{
		Columns: []domain.ResultColumn{{Name: "Месяц", Type: domain.ColumnTypeDate}, {Name: "Выручка", Type: domain.ColumnTypeNumeric}},
		Rows:    [][]any{{month(5), 100.0}, {month(6), 150.0}},
	}
}

func widgetWith(t *testing.T, title string, q map[string]any, spec domain.ChartSpec) domain.Widget {
	t.Helper()
	raw, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("параметры виджета: %v", err)
	}
	return domain.Widget{ID: uuid.New(), Title: title, Query: raw, Chart: spec, Width: 6}
}

func TestGetAppliesPeriodToTimeWidgets(t *testing.T) {
	trend := widgetWith(t, "Выручка по месяцам", map[string]any{
		"dataset": "orders", "time": map[string]any{"column": "created_at", "grain": "month"},
		"metrics": []any{map[string]any{"fn": "sum", "column": "amount", "label": "Выручка"}},
	}, domain.ChartSpec{Type: domain.ChartTypeLine, X: "Месяц", Y: []string{"Выручка"}})
	byStatus := widgetWith(t, "Заказы по статусам", map[string]any{"dataset": "orders", "group_by": []any{"status"}},
		domain.ChartSpec{Type: domain.ChartTypeBar})

	repo := &fakeRepo{dashboard: &domain.Dashboard{ID: uuid.New(), Title: "Продажи", Widgets: []domain.Widget{trend, byStatus}}}
	exec := &fakeExecutor{result: monthly()}
	svc := newTestService(repo, exec, false)

	d, err := svc.Get(context.Background(), repo.dashboard.ID, true, domain.DateRange{From: "2024-01-01", To: "2024-03-31"})
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	if !d.Widgets[0].PeriodApplied || d.Widgets[1].PeriodApplied {
		t.Fatalf("фильтр периода только для виджета с динамикой: %v %v", d.Widgets[0].PeriodApplied, d.Widgets[1].PeriodApplied)
	}
	filtered := 0
	for i, q := range exec.queries {
		if !strings.Contains(q, `"created_at" >= $`) || !strings.Contains(q, `"created_at" < $`) {
			continue
		}
		filtered++
		if len(exec.args[i]) != 2 {
			t.Fatalf("границы периода должны уйти параметрами: %#v", exec.args[i])
		}
	}
	if filtered != 1 {
		t.Fatalf("фильтр периода должен попасть ровно в один запрос: %v", exec.queries)
	}
}

func TestGetFillsHeadlineRangeAndRefreshTime(t *testing.T) {
	trend := widgetWith(t, "Выручка", map[string]any{
		"dataset": "orders", "time": map[string]any{"column": "created_at", "grain": "month"},
		"metrics": []any{map[string]any{"fn": "sum", "column": "amount", "label": "Выручка"}},
	}, domain.ChartSpec{Type: domain.ChartTypeLine, X: "Месяц", Y: []string{"Выручка"}})
	repo := &fakeRepo{dashboard: &domain.Dashboard{ID: uuid.New(), Title: "Продажи", Widgets: []domain.Widget{trend}}}
	svc := newTestService(repo, &fakeExecutor{result: monthly()}, false)

	d, err := svc.Get(context.Background(), repo.dashboard.ID, true, domain.DateRange{})
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if d.RefreshedAt == nil || d.Period != nil {
		t.Fatalf("время пересчёта и отсутствие фильтра: %+v", d)
	}
	if d.DateRange == nil || d.DateRange.From != "2023-01-01" || d.DateRange.To != "2024-06-30" {
		t.Fatalf("период данных по профилю каталога: %+v", d.DateRange)
	}
	h := d.Widgets[0].Headline
	if h == nil || h.Value != 150 || h.ChangePct == nil || *h.ChangePct != 50 {
		t.Fatalf("главное число виджета: %+v", h)
	}
}

func TestGetHidesDatabaseErrors(t *testing.T) {
	w := widgetWith(t, "Заказы", map[string]any{"dataset": "orders"}, domain.ChartSpec{Type: domain.ChartTypeKPI})
	broken := widgetWith(t, "Сломанный", map[string]any{"dataset": "missing"}, domain.ChartSpec{Type: domain.ChartTypeKPI})
	repo := &fakeRepo{dashboard: &domain.Dashboard{ID: uuid.New(), Widgets: []domain.Widget{w, broken}}}
	exec := &fakeExecutor{err: errors.New(`ERROR: permission denied for table secret_keys (SQLSTATE 42501)`)}
	svc := newTestService(repo, exec, false)

	d, err := svc.Get(context.Background(), repo.dashboard.ID, true, domain.DateRange{})
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if strings.Contains(d.Widgets[0].Error, "secret_keys") || d.Widgets[0].Error == "" {
		t.Fatalf("текст ошибки базы не должен доходить до пользователя: %q", d.Widgets[0].Error)
	}
	if !strings.Contains(d.Widgets[1].Error, "missing") {
		t.Fatalf("ошибка проверки платформы объясняет причину: %q", d.Widgets[1].Error)
	}
}

func TestGetRejectsBadPeriod(t *testing.T) {
	repo := &fakeRepo{dashboard: &domain.Dashboard{ID: uuid.New()}}
	svc := newTestService(repo, &fakeExecutor{}, false)

	for _, p := range []domain.DateRange{{From: "01.01.2024"}, {To: "2024-13-01"}, {From: "2024-06-01", To: "2024-01-01"}} {
		if _, err := svc.Get(context.Background(), repo.dashboard.ID, true, p); !errors.Is(err, apperr.ErrValidation) {
			t.Errorf("период %+v должен быть отклонён, получено %v", p, err)
		}
	}
}

func TestAddWidgetRejectsRawSQLByDefault(t *testing.T) {
	repo := &fakeRepo{dashboard: &domain.Dashboard{ID: uuid.New()}}
	raw := domain.Widget{Title: "SQL", SQL: "SELECT count(*) FROM raw.ds_orders"}

	if _, err := newTestService(repo, &fakeExecutor{}, false).AddWidget(context.Background(), repo.dashboard.ID, raw); !errors.Is(err, apperr.ErrUnsafeQuery) {
		t.Fatalf("виджет с готовым SQL должен быть отклонён, получено %v", err)
	}
	if _, err := newTestService(repo, &fakeExecutor{}, true).AddWidget(context.Background(), repo.dashboard.ID, raw); err != nil {
		t.Fatalf("при AGENT_ALLOW_RAW_SQL виджет с SQL принимается после проверки: %v", err)
	}

	unsafe := domain.Widget{Title: "SQL", SQL: "SELECT pg_read_file('/etc/passwd')"}
	if _, err := newTestService(repo, &fakeExecutor{}, true).AddWidget(context.Background(), repo.dashboard.ID, unsafe); !errors.Is(err, apperr.ErrUnsafeQuery) {
		t.Fatalf("опасный SQL не проходит sqlguard и при разрешённом SQL, получено %v", err)
	}
}
