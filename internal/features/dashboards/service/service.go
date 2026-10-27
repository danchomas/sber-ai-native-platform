package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/sqlguard"
)

const widgetConcurrency = 4

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
	log     *slog.Logger
}

func New(
	repo DashboardRepository,
	catalog Catalog,
	exec Executor,
	cfg config.Query,
	log *slog.Logger,
) *Service {
	return &Service{repo: repo, catalog: catalog, exec: exec, cfg: cfg, log: log}
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

func (s *Service) Get(ctx context.Context, id uuid.UUID, refresh bool) (*domain.Dashboard, error) {
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
			sql, args, err := widgetQuery(d.Widgets[i], catalog, s.cfg.MaxRows)
			if err != nil {
				mu.Lock()
				d.Widgets[i].Error = "запрос больше недоступен: " + err.Error()
				mu.Unlock()
				return nil
			}
			scope, checked, err := sqlguard.ResolveScope(sql, scopes, s.catalog.Schema(), s.cfg.MaxRows)
			if err != nil {
				mu.Lock()
				d.Widgets[i].Error = "запрос больше недоступен: " + err.Error()
				mu.Unlock()
				return nil
			}
			result, err := s.exec.Query(gctx, scope.Target, checked.SQL, s.cfg.MaxRows, args...)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				d.Widgets[i].Error = err.Error()
				return nil
			}
			d.Widgets[i].Result = result
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return d, nil
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

// widgetQuery пересобирает SQL виджета из сохранённых параметров: так
// виджет проходит текущую проверку каталога и переживает переименование
// таблиц. Виджеты, у которых есть только SQL, выполняются как раньше.
func widgetQuery(w domain.Widget, catalog analytics.Catalog, maxRows int) (string, []any, error) {
	if len(w.Query) == 0 {
		return w.SQL, nil, nil
	}
	var q analytics.Query
	if err := json.Unmarshal(w.Query, &q); err != nil {
		return "", nil, fmt.Errorf("параметры виджета повреждены: %w", err)
	}
	compiled, err := analytics.Compile(q, catalog, maxRows)
	if err != nil {
		return "", nil, err
	}
	return compiled.SQL, compiled.Args, nil
}

// Create собирает панель из виджетов, присланных интерфейсом: например,
// из ответа в чате, закреплённого пользователем.
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

// AddWidget дописывает виджет на существующую панель. Запрос виджета
// проверяется сразу, а не при первом открытии: на панель не попадёт то,
// что нельзя выполнить.
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

	catalog, err := s.analyticsCatalog(ctx)
	if err != nil {
		return w, err
	}
	scopes, err := s.catalog.Scopes(ctx)
	if err != nil {
		return w, err
	}
	sql, _, err := widgetQuery(w, catalog, s.cfg.MaxRows)
	if err != nil {
		return w, err
	}
	if _, _, err := sqlguard.ResolveScope(sql, scopes, s.catalog.Schema(), s.cfg.MaxRows); err != nil {
		return w, err
	}
	if len(w.Query) > 0 {
		w.SQL = sql
	}
	w.ID, w.Result, w.Error = uuid.Nil, nil, ""
	return w, nil
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}
