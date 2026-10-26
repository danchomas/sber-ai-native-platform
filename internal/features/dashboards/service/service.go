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

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}
