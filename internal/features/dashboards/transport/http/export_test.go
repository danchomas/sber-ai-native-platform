package http

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/domain"
)

type fakeDashboards struct {
	Dashboards
	dashboard *domain.Dashboard
	period    domain.DateRange
}

func (f *fakeDashboards) Get(_ context.Context, _ uuid.UUID, _ bool, period domain.DateRange) (*domain.Dashboard, error) {
	f.period = period
	return f.dashboard, nil
}

func sampleDashboard() *domain.Dashboard {
	change := 12.5
	return &domain.Dashboard{
		ID:    uuid.New(),
		Title: "Продажи / квартал",
		Widgets: []domain.Widget{
			{
				ID:    uuid.New(),
				Title: "Выручка по месяцам",
				Result: &domain.ResultSet{
					Columns: []domain.ResultColumn{{Name: "Месяц", Type: domain.ColumnTypeDate}, {Name: "Выручка", Type: domain.ColumnTypeNumeric}},
					Rows:    [][]any{{"2024-01-01", 100.5}, {"2024-02-01", 113.0}},
				},
				Headline: &domain.Headline{Label: "Выручка", Value: 113, ChangePct: &change},
			},
			{ID: uuid.New(), Title: "Сломанный", Error: "датасет удалён"},
		},
	}
}

func serve(t *testing.T, h *Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	h.Register(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestExportXLSX(t *testing.T) {
	d := sampleDashboard()
	fake := &fakeDashboards{dashboard: d}
	rec := serve(t, New(fake), "/dashboards/"+d.ID.String()+"/export?from=2024-01-01&to=2024-03-31")

	if rec.Code != http.StatusOK {
		t.Fatalf("статус %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != mimeXLSX {
		t.Fatalf("Content-Type %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "filename*=UTF-8''") || strings.Contains(cd, "/") {
		t.Fatalf("Content-Disposition %q", cd)
	}
	if fake.period.From != "2024-01-01" || fake.period.To != "2024-03-31" {
		t.Fatalf("период не передан: %+v", fake.period)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("не xlsx: %v", err)
	}
	sheets := 0
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/") {
			sheets++
		}
	}
	if sheets != 3 {
		t.Fatalf("листов %d, ожидалось 3: сводка и два виджета", sheets)
	}
}

func TestExportWidgetCSV(t *testing.T) {
	d := sampleDashboard()
	rec := serve(t, New(&fakeDashboards{dashboard: d}), "/dashboards/"+d.ID.String()+"/export?format=csv&widget="+d.Widgets[0].ID.String())

	if rec.Code != http.StatusOK {
		t.Fatalf("статус %d: %s", rec.Code, rec.Body.String())
	}
	want := "\xEF\xBB\xBFМесяц;Выручка\r\n2024-01-01;100,5\r\n2024-02-01;113\r\n"
	if rec.Body.String() != want {
		t.Fatalf("получено %q", rec.Body.String())
	}
}

func TestExportRejectsBadInput(t *testing.T) {
	d := sampleDashboard()
	h := New(&fakeDashboards{dashboard: d})
	for target, status := range map[string]int{
		"/dashboards/" + d.ID.String() + "/export?format=pdf":                    http.StatusBadRequest,
		"/dashboards/" + d.ID.String() + "/export?widget=nope":                   http.StatusBadRequest,
		"/dashboards/" + d.ID.String() + "/export?widget=" + uuid.New().String(): http.StatusNotFound,
	} {
		if rec := serve(t, h, target); rec.Code != status {
			t.Errorf("%s: статус %d, ожидался %d", target, rec.Code, status)
		}
	}
}
