package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/chart"
	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/sqlguard"
)

const (
	widgetConcurrency = 4
	purgeInterval     = time.Hour
)

type Catalog interface {
	List(ctx context.Context) ([]domain.Dataset, error)
	Relations(ctx context.Context) ([]domain.Relation, error)
	Scopes(ctx context.Context) ([]domain.Scope, error)
	Schema() string
}

type Executor interface {
	Query(ctx context.Context, target domain.QueryTarget, sql string, maxRows int, args ...any) (*domain.ResultSet, error)
}

type Service struct {
	repo    DashboardRepository
	catalog Catalog
	exec    Executor
	cfg     config.Query

	allowSQL  bool
	retention time.Duration
	log       *slog.Logger
}

func New(
	repo DashboardRepository,
	catalog Catalog,
	exec Executor,
	cfg config.Query,
	allowSQL bool,
	retention time.Duration,
	log *slog.Logger,
) *Service {
	return &Service{repo: repo, catalog: catalog, exec: exec, cfg: cfg, allowSQL: allowSQL, retention: retention, log: log}
}

func (s *Service) CreateFromWidgets(
	ctx context.Context,
	sessionID *uuid.UUID,
	title, description string,
	widgets []domain.Widget,
) (*domain.Dashboard, error) {
	if strings.TrimSpace(title) == "" {
		return nil, apperr.NewValidationError("title", "у дашборда должен быть заголовок")
	}
	if len(widgets) == 0 {
		return nil, apperr.NewValidationError("widgets", "дашборд не может быть пустым")
	}

	d := &domain.Dashboard{
		ID:          uuid.New(),
		SessionID:   sessionID,
		Title:       strings.TrimSpace(title),
		Description: strings.TrimSpace(description),
		Widgets:     widgets,
	}
	if err := s.repo.Create(ctx, d); err != nil {
		return nil, err
	}
	s.log.InfoContext(ctx, "дашборд сохранён", "dashboard", d.ID, "widgets", len(widgets))
	return d, nil
}

func (s *Service) List(ctx context.Context, limit int) ([]domain.Dashboard, error) {
	return s.repo.List(ctx, limit)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID, refresh bool, period domain.DateRange) (*domain.Dashboard, error) {
	if err := validatePeriod(period); err != nil {
		return nil, err
	}
	d, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !refresh || len(d.Widgets) == 0 {
		return d, nil
	}

	scopes, err := s.catalog.Scopes(ctx)
	if err != nil {
		return nil, err
	}
	catalog, err := s.analyticsCatalog(ctx)
	if err != nil {
		return nil, err
	}

	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(widgetConcurrency)

	for i := range d.Widgets {
		g.Go(func() error {
			w := &d.Widgets[i]
			result, applied, err := s.refreshWidget(gctx, *w, catalog, scopes, period)
			mu.Lock()
			defer mu.Unlock()
			w.PeriodApplied = applied
			if err != nil {
				w.Error = s.widgetError(gctx, w.ID, err)
				return nil
			}
			w.Result = result
			w.Headline = chart.Headline(w.Chart, result)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	d.RefreshedAt = &now
	d.DateRange = dateRange(d.Widgets, catalog)
	if period.From != "" || period.To != "" {
		d.Period = &period
	}
	return d, nil
}

func (s *Service) refreshWidget(
	ctx context.Context,
	w domain.Widget,
	catalog analytics.Catalog,
	scopes []domain.Scope,
	period domain.DateRange,
) (*domain.ResultSet, bool, error) {
	sql, args, applied, err := widgetQuery(w, catalog, s.cfg.MaxRows, period)
	if err != nil {
		return nil, false, err
	}
	scope, checked, err := sqlguard.ResolveScope(sql, scopes, s.catalog.Schema(), s.cfg.MaxRows)
	if err != nil {
		return nil, applied, err
	}
	result, err := s.exec.Query(ctx, scope.Target, checked.SQL, s.cfg.MaxRows, args...)
	return result, applied, err
}

func (s *Service) widgetError(ctx context.Context, widgetID uuid.UUID, err error) string {
	switch {
	case errors.Is(err, apperr.ErrValidation), errors.Is(err, apperr.ErrUnsafeQuery):
		return "запрос больше недоступен: " + err.Error()
	case errors.Is(err, apperr.ErrQueryTimeout):
		return "показатель не успел пересчитаться: сузьте период"
	}
	s.log.ErrorContext(ctx, "виджет не пересчитан", "widget", widgetID, "error", err)
	return "не удалось пересчитать показатель, попробуйте обновить панель"
}

const dateLayout = "2006-01-02"

func validatePeriod(p domain.DateRange) error {
	var from, to time.Time
	var err error
	if p.From != "" {
		if from, err = time.Parse(dateLayout, p.From); err != nil {
			return apperr.NewValidationError("from", "дата начала должна быть в формате ГГГГ-ММ-ДД")
		}
	}
	if p.To != "" {
		if to, err = time.Parse(dateLayout, p.To); err != nil {
			return apperr.NewValidationError("to", "дата окончания должна быть в формате ГГГГ-ММ-ДД")
		}
	}
	if p.From != "" && p.To != "" && to.Before(from) {
		return apperr.NewValidationError("to", "дата окончания раньше даты начала")
	}
	return nil
}

func (s *Service) analyticsCatalog(ctx context.Context) (analytics.Catalog, error) {
	datasets, err := s.catalog.List(ctx)
	if err != nil {
		return analytics.Catalog{}, err
	}
	relations, err := s.catalog.Relations(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "связи каталога недоступны", "error", err)
	}
	return analytics.Catalog{Datasets: datasets, Relations: relations, DefaultSchema: s.catalog.Schema()}, nil
}

func widgetQuery(w domain.Widget, catalog analytics.Catalog, maxRows int, period domain.DateRange) (string, []any, bool, error) {
	if len(w.Query) == 0 {
		return w.SQL, nil, false, nil
	}
	var q analytics.Query
	if err := json.Unmarshal(w.Query, &q); err != nil {
		return "", nil, false, fmt.Errorf("параметры виджета повреждены: %w", err)
	}

	applied := false
	if q.Time != nil && (period.From != "" || period.To != "") {
		if period.From != "" {
			q.Filters = append(q.Filters, analytics.Filter{Column: q.Time.Column, Op: analytics.OpGte, Value: period.From})
		}
		if period.To != "" {
			to, _ := time.Parse(dateLayout, period.To)
			q.Filters = append(q.Filters, analytics.Filter{
				Column: q.Time.Column, Op: analytics.OpLt, Value: to.AddDate(0, 0, 1).Format(dateLayout),
			})
		}
		applied = true
	}

	compiled, err := analytics.Compile(q, catalog, maxRows)
	if err != nil {
		return "", nil, applied, err
	}
	return compiled.SQL, compiled.Args, applied, nil
}

func dateRange(widgets []domain.Widget, catalog analytics.Catalog) *domain.DateRange {
	var out domain.DateRange
	for _, w := range widgets {
		var q analytics.Query
		if len(w.Query) == 0 || json.Unmarshal(w.Query, &q) != nil || q.Time == nil {
			continue
		}
		p := timeProfile(catalog, q.Dataset, q.Time.Column)
		if p == nil || p.MinValue == nil || p.MaxValue == nil {
			continue
		}
		minDay, maxDay := day(*p.MinValue), day(*p.MaxValue)
		if minDay == "" || maxDay == "" {
			continue
		}
		if out.From == "" || minDay < out.From {
			out.From = minDay
		}
		if maxDay > out.To {
			out.To = maxDay
		}
	}
	if out.From == "" {
		return nil
	}
	return &out
}

func timeProfile(catalog analytics.Catalog, dataset, column string) *domain.ColumnProfile {
	for _, d := range catalog.Datasets {
		if !strings.EqualFold(d.Name, dataset) && !strings.EqualFold(d.TableName, dataset) {
			continue
		}
		for _, c := range d.Columns {
			if strings.EqualFold(c.Name, column) || strings.EqualFold(c.SourceName, column) {
				return c.Profile
			}
		}
	}
	return nil
}

func day(v string) string {
	if len(v) < len(dateLayout) {
		return ""
	}
	if _, err := time.Parse(dateLayout, v[:len(dateLayout)]); err != nil {
		return ""
	}
	return v[:len(dateLayout)]
}

func (s *Service) Create(ctx context.Context, title, description string, widgets []domain.Widget) (*domain.Dashboard, error) {
	checked := make([]domain.Widget, 0, len(widgets))
	for _, w := range widgets {
		w, err := s.checkWidget(ctx, w)
		if err != nil {
			return nil, err
		}
		checked = append(checked, w)
	}
	return s.CreateFromWidgets(ctx, nil, title, description, checked)
}

func (s *Service) AddWidget(ctx context.Context, dashboardID uuid.UUID, w domain.Widget) (*domain.Widget, error) {
	w, err := s.checkWidget(ctx, w)
	if err != nil {
		return nil, err
	}
	if err := s.repo.AddWidget(ctx, dashboardID, &w); err != nil {
		return nil, err
	}
	s.log.InfoContext(ctx, "виджет добавлен на дашборд", "dashboard", dashboardID, "widget", w.ID)
	return &w, nil
}

func (s *Service) RemoveWidget(ctx context.Context, dashboardID, widgetID uuid.UUID) error {
	return s.repo.DeleteWidget(ctx, dashboardID, widgetID)
}

func (s *Service) Rename(ctx context.Context, id uuid.UUID, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return apperr.NewValidationError("title", "у дашборда должен быть заголовок")
	}
	return s.repo.Rename(ctx, id, title)
}

func (s *Service) checkWidget(ctx context.Context, w domain.Widget) (domain.Widget, error) {
	w.Title = strings.TrimSpace(w.Title)
	if w.Title == "" {
		return w, apperr.NewValidationError("title", "у виджета должен быть заголовок")
	}
	if len(w.Query) == 0 && strings.TrimSpace(w.SQL) == "" {
		return w, apperr.NewValidationError("query", "у виджета нет запроса")
	}

	if len(w.Query) == 0 && !s.allowSQL {
		return w, fmt.Errorf("%w: виджет должен описываться параметрами запроса, свободный SQL отключён",
			apperr.ErrUnsafeQuery)
	}

	catalog, err := s.analyticsCatalog(ctx)
	if err != nil {
		return w, err
	}
	scopes, err := s.catalog.Scopes(ctx)
	if err != nil {
		return w, err
	}
	sql, _, _, err := widgetQuery(w, catalog, s.cfg.MaxRows, domain.DateRange{})
	if err != nil {
		return w, err
	}
	if _, _, err := sqlguard.ResolveScope(sql, scopes, s.catalog.Schema(), s.cfg.MaxRows); err != nil {
		return w, err
	}
	if len(w.Query) > 0 {
		w.SQL = sql
	}
	w.ID, w.Result, w.Error, w.Headline = uuid.Nil, nil, "", nil
	return w, nil
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}

func (s *Service) Trash(ctx context.Context) ([]domain.TrashItem, error) {
	items, err := s.repo.Trash(ctx, s.retention)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].ExpiresAt = items[i].DeletedAt.Add(s.retention)
	}
	return items, nil
}

func (s *Service) RestoreDashboard(ctx context.Context, id uuid.UUID) error {
	return s.repo.RestoreDashboard(ctx, id, s.retention)
}

func (s *Service) RestoreWidget(ctx context.Context, dashboardID, widgetID uuid.UUID) error {
	return s.repo.RestoreWidget(ctx, dashboardID, widgetID, s.retention)
}

func (s *Service) PurgeLoop(ctx context.Context) {
	ticker := time.NewTicker(purgeInterval)
	defer ticker.Stop()
	for {
		purged, err := s.repo.PurgeDeleted(ctx, s.retention)
		switch {
		case err != nil && ctx.Err() == nil:
			s.log.WarnContext(ctx, "корзина дашбордов не очищена", "error", err)
		case purged > 0:
			s.log.InfoContext(ctx, "корзина дашбордов очищена", "deleted", purged)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
