package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/heinousness/aibi/internal/config"
	"github.com/heinousness/aibi/internal/domain"
)

const minOverlap = 0.25

const profileConcurrency = 4

type Service struct {
	datasets  domain.DatasetRepository
	relations domain.RelationRepository
	raw       domain.RawDataRepository
	schema    string
	cfg       config.Ingest
	query     config.Query
	log       *slog.Logger
}

func New(
	datasets domain.DatasetRepository,
	relations domain.RelationRepository,
	raw domain.RawDataRepository,
	schema string,
	cfg config.Ingest,
	query config.Query,
	log *slog.Logger,
) *Service {
	return &Service{
		datasets: datasets, relations: relations, raw: raw,
		schema: schema, cfg: cfg, query: query, log: log,
	}
}

func (s *Service) List(ctx context.Context) ([]domain.Dataset, error) {
	return s.datasets.List(ctx)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Dataset, error) {
	return s.datasets.Get(ctx, id)
}

func (s *Service) Relations(ctx context.Context) ([]domain.Relation, error) {
	return s.relations.List(ctx)
}

func (s *Service) Tables(ctx context.Context) ([]string, error) {
	datasets, err := s.datasets.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(datasets))
	for _, d := range datasets {
		if d.Ready() {
			out = append(out, d.TableName)
		}
	}
	return out, nil
}

func (s *Service) Schema() string { return s.schema }

func (s *Service) Preview(ctx context.Context, id uuid.UUID, limit int) (*domain.ResultSet, error) {
	dataset, err := s.datasets.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !dataset.Ready() {
		return nil, domain.ErrDatasetNotReady
	}
	if limit <= 0 {
		limit = s.query.PreviewRows
	}
	return s.raw.Sample(ctx, dataset.TableName, limit)
}

func (s *Service) ProfileDataset(ctx context.Context, id uuid.UUID) error {
	dataset, err := s.datasets.Get(ctx, id)
	if err != nil {
		return err
	}
	started := time.Now()

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(profileConcurrency)

	for _, col := range dataset.Columns {
		col := col
		g.Go(func() error {
			p, err := s.raw.ProfileColumn(gctx, dataset.TableName, col, s.cfg.ProfileTopValues)
			if err != nil {
				return fmt.Errorf("колонка %s: %w", col.Name, err)
			}
			if err := s.datasets.SaveColumnProfile(gctx, col.ID, p); err != nil {
				return err
			}
			return s.datasets.SaveColumnRole(gctx, col.ID, inferRole(col, p), "")
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	s.log.InfoContext(ctx, "датасет профилирован",
		"dataset", dataset.Name, "columns", len(dataset.Columns),
		"elapsed_ms", time.Since(started).Milliseconds())
	return nil
}

func (s *Service) Profile(ctx context.Context, id uuid.UUID) (*domain.Profile, error) {
	dataset, err := s.datasets.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !dataset.Ready() {
		return nil, domain.ErrDatasetNotReady
	}

	out := &domain.Profile{Dataset: *dataset}

	for _, col := range dataset.Columns {
		if !col.Type.IsTemporal() {
			continue
		}
		span, err := s.raw.TimeSpan(ctx, dataset.TableName, col)
		if err != nil {
			s.log.WarnContext(ctx, "временной охват не посчитан",
				"dataset", dataset.Name, "column", col.Name, "error", err)
			continue
		}
		if span.From != nil {
			out.TimeCoverage = append(out.TimeCoverage, *span)
		}
	}

	for _, col := range dataset.Columns {
		if col.Profile != nil && col.Profile.Unique() {
			out.CandidateKey = append(out.CandidateKey, col.Name)
		}
	}

	if dups, err := s.raw.DuplicateRows(ctx, dataset.TableName, dataset.Columns); err == nil {
		out.DuplicateRows = dups
	} else {
		s.log.WarnContext(ctx, "дубликаты не посчитаны", "dataset", dataset.Name, "error", err)
	}

	if rels, err := s.relations.ListForDataset(ctx, dataset.ID); err == nil {
		out.Relations = rels
	}

	if sample, err := s.raw.Sample(ctx, dataset.TableName, 10); err == nil {
		out.Sample = sample
	}
	return out, nil
}

func (s *Service) DiscoverRelations(ctx context.Context) error {
	datasets, err := s.datasets.List(ctx)
	if err != nil {
		return err
	}

	type candidate struct {
		dataset domain.Dataset
		column  domain.Column
	}
	var candidates []candidate
	for _, d := range datasets {
		if !d.Ready() {
			continue
		}
		for _, c := range d.Columns {
			if c.Role == domain.RoleIdentifier || looksLikeIdentifier(c.Name, c.SourceName) {
				candidates = append(candidates, candidate{dataset: d, column: c})
			}
		}
	}

	var relations []domain.Relation
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			left, right := candidates[i], candidates[j]
			if left.dataset.ID == right.dataset.ID {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}

			rel, ok, err := s.evaluatePair(ctx, left.dataset, left.column, right.dataset, right.column)
			if err != nil {
				s.log.WarnContext(ctx, "связь не проверена",
					"left", left.dataset.Name+"."+left.column.Name,
					"right", right.dataset.Name+"."+right.column.Name, "error", err)
				continue
			}
			if ok {
				relations = append(relations, rel)
			}
		}
	}

	sort.Slice(relations, func(a, b int) bool { return relations[a].Confidence > relations[b].Confidence })
	if err := s.relations.Replace(ctx, relations); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "связи каталога обновлены", "relations", len(relations))
	return nil
}

func (s *Service) evaluatePair(
	ctx context.Context,
	leftDS domain.Dataset, leftCol domain.Column,
	rightDS domain.Dataset, rightCol domain.Column,
) (domain.Relation, bool, error) {
	leftRef := domain.TableRef{Schema: s.schema, Table: leftDS.TableName, Column: leftCol.Name}
	rightRef := domain.TableRef{Schema: s.schema, Table: rightDS.TableName, Column: rightCol.Name}

	forward, matched, err := s.raw.ValueOverlap(ctx, leftRef, rightRef)
	if err != nil {
		return domain.Relation{}, false, err
	}
	if forward == 0 {
		return domain.Relation{}, false, nil
	}
	backward, _, err := s.raw.ValueOverlap(ctx, rightRef, leftRef)
	if err != nil {
		return domain.Relation{}, false, err
	}
	if forward < minOverlap && backward < minOverlap {
		return domain.Relation{}, false, nil
	}

	if backward > forward {
		leftDS, rightDS = rightDS, leftDS
		leftCol, rightCol = rightCol, leftCol
		forward, backward = backward, forward
	}

	rel := domain.Relation{
		LeftDatasetID:  leftDS.ID,
		LeftDataset:    leftDS.Name,
		LeftColumn:     leftCol.Name,
		RightDatasetID: rightDS.ID,
		RightDataset:   rightDS.Name,
		RightColumn:    rightCol.Name,
		Overlap:        round4(forward),
		MatchedValues:  matched,
		Cardinality:    cardinality(leftCol, rightCol),
		Confidence:     round4(confidence(forward, leftCol, rightCol)),
	}
	return rel, true, nil
}

func cardinality(left, right domain.Column) string {
	leftUnique := left.Profile != nil && left.Profile.Unique()
	rightUnique := right.Profile != nil && right.Profile.Unique()

	switch {
	case leftUnique && rightUnique:
		return "1:1"
	case rightUnique:
		return "N:1"
	case leftUnique:
		return "1:N"
	default:
		return "N:M"
	}
}

func confidence(overlap float64, left, right domain.Column) float64 {
	score := overlap
	if strings.EqualFold(left.Name, right.Name) {
		score = min(1, score+0.15)
	}
	if right.Profile != nil && right.Profile.Unique() {
		score = min(1, score+0.05)
	}
	return score
}

func round4(f float64) float64 {
	return float64(int(f*10000+0.5)) / 10000
}
