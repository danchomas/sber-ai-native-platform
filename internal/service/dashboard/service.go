package dashboard

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/heinousness/aibi/internal/config"
	"github.com/heinousness/aibi/internal/domain"
	"github.com/heinousness/aibi/internal/sqlguard"
)

const widgetConcurrency = 4

type Catalog interface {
	Scopes(ctx context.Context) ([]domain.Scope, error)
	Schema() string
}

type Executor interface {
	Query(ctx context.Context, target domain.QueryTarget, sql string, maxRows int) (*domain.ResultSet, error)
}

type Service struct {
	repo    domain.DashboardRepository
	catalog Catalog
	exec    Executor
	cfg     config.Query
	log     *slog.Logger
}

func New(
	repo domain.DashboardRepository,
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
		return nil, domain.NewValidationError("title", "у дашборда должен быть заголовок")
	}
	if len(widgets) == 0 {
		return nil, domain.NewValidationError("widgets", "дашборд не может быть пустым")
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

	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(widgetConcurrency)

	for i := range d.Widgets {
		i := i
		g.Go(func() error {
			scope, checked, err := sqlguard.ResolveScope(
				d.Widgets[i].SQL, scopes, s.catalog.Schema(), s.cfg.MaxRows)
			if err != nil {
				mu.Lock()
				d.Widgets[i].Error = "запрос больше недоступен: " + err.Error()
				mu.Unlock()
				return nil
			}
			result, err := s.exec.Query(gctx, scope.Target, checked.SQL, s.cfg.MaxRows)
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

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}
