package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
)

type Profiler interface {
	ProfileDataset(ctx context.Context, datasetID uuid.UUID) error
	DiscoverRelations(ctx context.Context) error
}

type Service struct {
	sources    SourceRepository
	datasets   Datasets
	raw        domain.RawDataRepository
	introspect SourceIntrospector
	profiler   Profiler
	forgetPool func(uuid.UUID)
	log        *slog.Logger
}

func New(
	sources SourceRepository,
	datasets Datasets,
	raw domain.RawDataRepository,
	introspect SourceIntrospector,
	profiler Profiler,
	forgetPool func(uuid.UUID),
	log *slog.Logger,
) *Service {
	return &Service{
		sources: sources, datasets: datasets, raw: raw, introspect: introspect,
		profiler: profiler, forgetPool: forgetPool, log: log,
	}
}

func (s *Service) Attach(ctx context.Context, name, dsn string) (*domain.Source, error) {
	name = strings.TrimSpace(name)
	dsn = strings.TrimSpace(dsn)
	if name == "" {
		return nil, apperr.NewValidationError("name", "имя источника не может быть пустым")
	}
	if dsn == "" {
		return nil, apperr.NewValidationError("dsn", "строка подключения не может быть пустой")
	}
	if _, err := s.sources.GetByName(ctx, name); err == nil {
		return nil, fmt.Errorf("источник %q: %w", name, apperr.ErrAlreadyExists)
	}

	src := &domain.Source{
		ID:     uuid.New(),
		Name:   name,
		Kind:   domain.SourceKindPostgres,
		DSN:    dsn,
		Status: domain.SourceStatusReady,
	}
	if err := s.introspect.Ping(ctx, src); err != nil {
		return nil, fmt.Errorf("подключение не удалось: %w", err)
	}
	if err := s.sources.Create(ctx, src); err != nil {
		return nil, err
	}

	s.log.InfoContext(ctx, "источник подключён", "source", src.Name)
	return src, nil
}

func (s *Service) List(ctx context.Context) ([]domain.Source, error) {
	return s.sources.List(ctx)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Source, error) {
	return s.sources.Get(ctx, id)
}

func (s *Service) Tables(ctx context.Context, id uuid.UUID) ([]domain.ExternalTable, error) {
	src, err := s.sources.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	tables, err := s.introspect.Tables(ctx, src)
	if err != nil {
		s.markFailed(ctx, src, err)
		return nil, err
	}

	attached, err := s.attachedTables(ctx, id)
	if err != nil {
		return nil, err
	}
	for i := range tables {
		_, ok := attached[tables[i].Schema+"."+tables[i].Name]
		tables[i].Attached = ok
	}
	return tables, nil
}

func (s *Service) AttachTable(ctx context.Context, id uuid.UUID, schema, table, name string) (*domain.Dataset, error) {
	src, err := s.sources.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(schema) == "" || strings.TrimSpace(table) == "" {
		return nil, apperr.NewValidationError("table", "нужно указать схему и имя таблицы")
	}

	columns, err := s.introspect.Columns(ctx, src, schema, table)
	if err != nil {
		return nil, err
	}

	datasetName, err := s.uniqueName(ctx, name, table, src.Name)
	if err != nil {
		return nil, err
	}

	dataset := &domain.Dataset{
		ID:         uuid.New(),
		Name:       datasetName,
		Origin:     domain.OriginAttached,
		SourceID:   &src.ID,
		SourceName: src.Name,
		Schema:     schema,
		TableName:  table,
		Status:     domain.DatasetStatusPending,
	}
	for i := range columns {
		columns[i].DatasetID = dataset.ID
	}

	if err := s.datasets.Create(ctx, dataset); err != nil {
		return nil, err
	}
	if err := s.datasets.ReplaceColumns(ctx, dataset.ID, columns); err != nil {
		return nil, err
	}

	started := time.Now()
	if n, err := s.raw.RowCount(ctx, dataset.Ref()); err == nil {
		dataset.RowCount = n
	} else {
		s.log.WarnContext(ctx, "число строк подключённой таблицы не посчитано",
			"source", src.Name, "table", table, "error", err)
	}
	if size, err := s.raw.TableSizeBytes(ctx, dataset.Ref()); err == nil {
		dataset.SizeBytes = size
	}

	dataset.Status = domain.DatasetStatusReady
	dataset.Columns = columns
	if err := s.datasets.Update(ctx, dataset); err != nil {
		return nil, err
	}

	if err := s.profiler.ProfileDataset(ctx, dataset.ID); err != nil {
		s.log.WarnContext(ctx, "подключённая таблица не профилирована",
			"source", src.Name, "table", table, "error", err)
	}
	if err := s.profiler.DiscoverRelations(ctx); err != nil {
		s.log.WarnContext(ctx, "связи каталога не пересчитаны", "error", err)
	}

	s.log.InfoContext(ctx, "таблица подключена",
		"source", src.Name, "table", schema+"."+table, "dataset", dataset.Name,
		"elapsed_ms", time.Since(started).Milliseconds())
	return dataset, nil
}

func (s *Service) Detach(ctx context.Context, id uuid.UUID) error {
	if _, err := s.sources.Get(ctx, id); err != nil {
		return err
	}
	if err := s.sources.Delete(ctx, id); err != nil {
		return err
	}
	if s.forgetPool != nil {
		s.forgetPool(id)
	}
	s.log.InfoContext(ctx, "источник отключён", "source", id)
	return nil
}

func (s *Service) attachedTables(ctx context.Context, id uuid.UUID) (map[string]struct{}, error) {
	datasets, err := s.datasets.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(datasets))
	for _, d := range datasets {
		if d.SourceID != nil && *d.SourceID == id {
			out[d.Schema+"."+d.TableName] = struct{}{}
		}
	}
	return out, nil
}

func (s *Service) uniqueName(ctx context.Context, wanted, table, sourceName string) (string, error) {
	candidates := []string{strings.TrimSpace(wanted), table, sourceName + "." + table}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := s.datasets.GetByName(ctx, c); err != nil {
			return c, nil
		}
	}
	for i := 2; i < 100; i++ {
		c := fmt.Sprintf("%s.%s_%d", sourceName, table, i)
		if _, err := s.datasets.GetByName(ctx, c); err != nil {
			return c, nil
		}
	}
	return "", apperr.NewValidationError("name", "не удалось подобрать свободное имя датасета")
}

func (s *Service) markFailed(ctx context.Context, src *domain.Source, cause error) {
	src.Status = domain.SourceStatusFailed
	src.FailReason = cause.Error()
	if err := s.sources.Update(ctx, src); err != nil {
		s.log.WarnContext(ctx, "статус источника не сохранён", "source", src.Name, "error", err)
	}
}
