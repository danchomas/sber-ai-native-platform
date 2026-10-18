package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
)

type Profiler interface {
	ProfileDataset(ctx context.Context, datasetID uuid.UUID) error
	DiscoverRelations(ctx context.Context) error
}

type Service struct {
	datasets domain.DatasetRepository
	raw      domain.RawDataRepository
	profiler Profiler
	schema   string
	cfg      config.Ingest
	log      *slog.Logger
}

func New(
	datasets domain.DatasetRepository,
	raw domain.RawDataRepository,
	profiler Profiler,
	schema string,
	cfg config.Ingest,
	log *slog.Logger,
) *Service {
	return &Service{
		datasets: datasets, raw: raw, profiler: profiler,
		schema: schema, cfg: cfg, log: log,
	}
}

type Request struct {
	Name     string
	Filename string
	Reader   io.Reader
}

type Report struct {
	Dataset     *domain.Dataset `json:"dataset"`
	RowsLoaded  int64           `json:"rows_loaded"`
	SkippedRows int             `json:"skipped_rows"`
	BadValues   int             `json:"bad_values"`
	Dialect     string          `json:"dialect"`
	Encoding    string          `json:"encoding"`
	Warnings    []string        `json:"warnings,omitempty"`
	ElapsedMS   int64           `json:"elapsed_ms"`
}

func (s *Service) Upload(ctx context.Context, req Request) (*Report, error) {
	started := time.Now()

	name, err := s.datasetName(ctx, req)
	if err != nil {
		return nil, err
	}

	spooled, size, err := s.spool(req.Reader)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = spooled.Close()
		_ = os.Remove(spooled.Name())
	}()

	dialect, err := sniffFile(spooled)
	if err != nil {
		return nil, err
	}

	header, parsers, sampleWarnings, err := s.inferSchema(spooled, dialect)
	if err != nil {
		return nil, err
	}

	dataset := &domain.Dataset{
		ID:         uuid.New(),
		Name:       name,
		Origin:     domain.OriginUploaded,
		Schema:     s.schema,
		TableName:  TableName(name, uuid.NewString()[:8]),
		SourceFile: filepath.Base(req.Filename),
		Delimiter:  dialect.String(),
		SizeBytes:  size,
		Status:     domain.DatasetStatusPending,
	}
	columns := buildColumns(dataset.ID, header, parsers)

	if err := s.datasets.Create(ctx, dataset); err != nil {
		return nil, err
	}
	if err := s.datasets.ReplaceColumns(ctx, dataset.ID, columns); err != nil {
		return nil, s.abort(ctx, dataset, err)
	}
	if err := s.raw.CreateTable(ctx, dataset.TableName, columns); err != nil {
		return nil, s.abort(ctx, dataset, err)
	}

	loaded, stats, err := s.copyData(ctx, spooled, dialect, dataset.TableName, columns, parsers)
	if err != nil {
		return nil, s.abort(ctx, dataset, err)
	}

	dataset.RowCount = loaded
	dataset.Status = domain.DatasetStatusReady
	if physical, err := s.raw.TableSizeBytes(ctx, dataset.Ref()); err == nil {
		dataset.SizeBytes = physical
	}
	if err := s.datasets.Update(ctx, dataset); err != nil {
		return nil, s.abort(ctx, dataset, err)
	}
	if err := s.raw.Analyze(ctx, dataset.TableName); err != nil {
		s.log.WarnContext(ctx, "ANALYZE не выполнен", "dataset", dataset.Name, "error", err)
	}

	report := &Report{
		Dataset:     dataset,
		RowsLoaded:  loaded,
		SkippedRows: stats.skipped,
		BadValues:   stats.badValues,
		Dialect:     dialect.String(),
		Encoding:    dialect.Encoding,
		Warnings:    append(sampleWarnings, stats.warnings...),
	}

	if err := s.profiler.ProfileDataset(ctx, dataset.ID); err != nil {
		s.log.ErrorContext(ctx, "профилирование не выполнено", "dataset", dataset.Name, "error", err)
		report.Warnings = append(report.Warnings, "Статистика по колонкам будет неполной: "+err.Error())
	}
	if err := s.profiler.DiscoverRelations(ctx); err != nil {
		s.log.ErrorContext(ctx, "поиск связей не выполнен", "error", err)
	}

	if fresh, err := s.datasets.Get(ctx, dataset.ID); err == nil {
		report.Dataset = fresh
	}
	report.ElapsedMS = time.Since(started).Milliseconds()

	s.log.InfoContext(ctx, "датасет загружен",
		"dataset", dataset.Name, "table", dataset.TableName,
		"rows", loaded, "columns", len(columns), "elapsed_ms", report.ElapsedMS)
	return report, nil
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	dataset, err := s.datasets.Get(ctx, id)
	if err != nil {
		return err
	}
	if !dataset.Attached() {
		if err := s.raw.DropTable(ctx, dataset.TableName); err != nil {
			return err
		}
	}
	if err := s.datasets.Delete(ctx, id); err != nil {
		return err
	}
	if err := s.profiler.DiscoverRelations(ctx); err != nil {
		s.log.WarnContext(ctx, "пересчёт связей после удаления не выполнен", "error", err)
	}
	s.log.InfoContext(ctx, "датасет удалён", "dataset", dataset.Name)
	return nil
}

func (s *Service) datasetName(ctx context.Context, req Request) (string, error) {
	base := strings.TrimSpace(req.Name)
	if base == "" {
		base = strings.TrimSuffix(filepath.Base(req.Filename), filepath.Ext(req.Filename))
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return "", domain.NewValidationError("name", "не удалось определить имя датасета")
	}
	if len([]rune(base)) > 120 {
		base = string([]rune(base)[:120])
	}

	candidate := base
	for i := 2; i < 100; i++ {
		_, err := s.datasets.GetByName(ctx, candidate)
		if errors.Is(err, domain.ErrNotFound) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("%s (%d)", base, i)
	}
	return "", domain.NewValidationError("name", "слишком много датасетов с таким именем")
}

func (s *Service) spool(r io.Reader) (*os.File, int64, error) {
	if r == nil {
		return nil, 0, domain.NewValidationError("file", "файл не передан")
	}

	f, err := os.CreateTemp("", "aibi-upload-*.csv")
	if err != nil {
		return nil, 0, fmt.Errorf("временный файл: %w", err)
	}

	limit := s.cfg.MaxUploadBytes
	written, err := io.Copy(f, io.LimitReader(r, limit+1))
	if err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, 0, fmt.Errorf("запись файла: %w", err)
	}
	if written > limit {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, 0, domain.NewValidationError("file",
			fmt.Sprintf("размер превышает предел %d МБ", limit>>20))
	}
	if written == 0 {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, 0, domain.NewValidationError("file", "файл пуст")
	}
	return f, written, nil
}

func sniffFile(f *os.File) (Dialect, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Dialect{}, err
	}
	head := make([]byte, 64<<10)
	n, err := f.Read(head)
	if err != nil && !errors.Is(err, io.EOF) {
		return Dialect{}, fmt.Errorf("чтение заголовка файла: %w", err)
	}
	return SniffDialect(head[:n]), nil
}

func (s *Service) inferSchema(f *os.File, d Dialect) ([]string, []ValueParser, []string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, nil, nil, err
	}

	reader := NewReader(f, d)
	header, err := reader.Read()
	if err != nil {
		return nil, nil, nil, domain.NewValidationError("file", "не удалось прочитать заголовок CSV: "+err.Error())
	}
	if len(header) == 0 {
		return nil, nil, nil, domain.NewValidationError("file", "пустой заголовок CSV")
	}

	samples := make([][]string, len(header))
	var warnings []string
	ragged := 0

	for i := 0; i < s.cfg.TypeSampleRows; i++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			ragged++
			continue
		}
		if len(record) != len(header) {
			ragged++
		}
		for col := range header {
			if col < len(record) {
				samples[col] = append(samples[col], record[col])
			}
		}
	}

	if ragged > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"В выборке %d строк(и) с числом полей, отличным от заголовка — лишние значения отброшены, недостающие считаны как пропуски.", ragged))
	}

	parsers := make([]ValueParser, len(header))
	for i := range header {
		parsers[i] = InferType(samples[i])
	}
	return header, parsers, warnings, nil
}

type copyStats struct {
	skipped   int
	badValues int
	warnings  []string
}

func (s *Service) copyData(
	ctx context.Context,
	f *os.File,
	d Dialect,
	table string,
	columns []domain.Column,
	parsers []ValueParser,
) (int64, copyStats, error) {
	var stats copyStats

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, stats, err
	}
	reader := NewReader(f, d)
	if _, err := reader.Read(); err != nil {
		return 0, stats, fmt.Errorf("чтение заголовка: %w", err)
	}

	batchSize := s.cfg.CopyBatchRows
	if batchSize <= 0 {
		batchSize = 5000
	}

	var (
		batch  = make([][]any, 0, batchSize)
		loaded int64
	)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n, err := s.raw.CopyRows(ctx, table, columns, batch)
		if err != nil {
			return err
		}
		loaded += n
		batch = batch[:0]
		return nil
	}

	for {
		if err := ctx.Err(); err != nil {
			return loaded, stats, err
		}

		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			stats.skipped++
			continue
		}
		if len(record) == 1 && strings.TrimSpace(record[0]) == "" {
			continue
		}

		row := make([]any, len(columns))
		for i := range columns {
			if i >= len(record) {
				continue
			}
			value, err := parsers[i].Parse(record[i])
			if err != nil {
				stats.badValues++
				continue
			}
			row[i] = value
		}
		batch = append(batch, row)

		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return loaded, stats, err
			}
		}
	}
	if err := flush(); err != nil {
		return loaded, stats, err
	}

	if stats.skipped > 0 {
		stats.warnings = append(stats.warnings,
			fmt.Sprintf("%d строк(и) не удалось разобрать как CSV — они не загружены.", stats.skipped))
	}
	if stats.badValues > 0 {
		stats.warnings = append(stats.warnings,
			fmt.Sprintf("%d значени(я) не соответствуют выведенному типу колонки и сохранены как пропуски.", stats.badValues))
	}
	return loaded, stats, nil
}

func buildColumns(datasetID uuid.UUID, header []string, parsers []ValueParser) []domain.Column {
	names := UniqueNames(header)
	out := make([]domain.Column, len(header))
	for i := range header {
		out[i] = domain.Column{
			ID:         uuid.New(),
			DatasetID:  datasetID,
			Position:   i,
			SourceName: strings.TrimSpace(header[i]),
			Name:       names[i],
			Type:       parsers[i].Type,
			Role:       domain.RoleUnknown,
			Nullable:   true,
		}
	}
	return out
}

func (s *Service) abort(ctx context.Context, d *domain.Dataset, cause error) error {
	cleanup := context.WithoutCancel(ctx)
	if err := s.raw.DropTable(cleanup, d.TableName); err != nil {
		s.log.ErrorContext(cleanup, "не удалось удалить таблицу после сбоя загрузки",
			"table", d.TableName, "error", err)
	}
	if err := s.datasets.Delete(cleanup, d.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
		s.log.ErrorContext(cleanup, "не удалось удалить метаданные после сбоя загрузки",
			"dataset", d.Name, "error", err)
	}
	return fmt.Errorf("загрузка датасета %q не выполнена: %w", d.Name, cause)
}
