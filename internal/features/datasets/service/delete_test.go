package service

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
)

type stubDatasets struct {
	DatasetRepository
	dataset *domain.Dataset
	deleted []uuid.UUID
}

func (s *stubDatasets) Get(context.Context, uuid.UUID) (*domain.Dataset, error) {
	return s.dataset, nil
}

func (s *stubDatasets) List(context.Context) ([]domain.Dataset, error) {
	return nil, nil
}

func (s *stubDatasets) Delete(_ context.Context, id uuid.UUID) error {
	s.deleted = append(s.deleted, id)
	return nil
}

type stubRaw struct {
	domain.RawDataRepository
	dropped []string
}

func (s *stubRaw) DropTable(_ context.Context, table string) error {
	s.dropped = append(s.dropped, table)
	return nil
}

type stubRelations struct {
	RelationRepository
}

func (stubRelations) Replace(context.Context, []domain.Relation) error { return nil }

func newDeleteService(d *domain.Dataset) (*Service, *stubRaw, *stubDatasets) {
	datasets := &stubDatasets{dataset: d}
	raw := &stubRaw{}
	svc := New(datasets, stubRelations{}, raw, "raw", config.Ingest{}, config.Query{},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	return svc, raw, datasets
}

func TestDeleteDropsTableOfUploadedDataset(t *testing.T) {
	id := uuid.New()
	svc, raw, datasets := newDeleteService(&domain.Dataset{
		ID: id, Name: "orders", Origin: domain.OriginUploaded,
		Schema: "raw", TableName: "ds_orders",
	})

	if err := svc.Delete(context.Background(), id); err != nil {
		t.Fatalf("удаление загруженного датасета: %v", err)
	}
	if len(raw.dropped) != 1 || raw.dropped[0] != "ds_orders" {
		t.Errorf("таблица загруженного датасета должна быть удалена, снесено: %v", raw.dropped)
	}
	if len(datasets.deleted) != 1 {
		t.Errorf("метаданные датасета должны быть удалены, удалено: %v", datasets.deleted)
	}
}

func TestDeleteKeepsTableOfAttachedDataset(t *testing.T) {
	id := uuid.New()
	sourceID := uuid.New()
	svc, raw, datasets := newDeleteService(&domain.Dataset{
		ID: id, Name: "orders", Origin: domain.OriginAttached,
		SourceID: &sourceID, Schema: "sales", TableName: "orders",
	})

	if err := svc.Delete(context.Background(), id); err != nil {
		t.Fatalf("удаление подключённого датасета: %v", err)
	}
	if len(raw.dropped) != 0 {
		t.Errorf("таблица чужой базы не должна удаляться, снесено: %v", raw.dropped)
	}
	if len(datasets.deleted) != 1 {
		t.Errorf("метаданные датасета должны быть удалены, удалено: %v", datasets.deleted)
	}
}
