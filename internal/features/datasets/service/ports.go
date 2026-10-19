package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/domain"
)

type DatasetRepository interface {
	Create(ctx context.Context, d *domain.Dataset) error
	Update(ctx context.Context, d *domain.Dataset) error
	SetStatus(ctx context.Context, id uuid.UUID, status domain.DatasetStatus, failReason string) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Dataset, error)
	GetByName(ctx context.Context, name string) (*domain.Dataset, error)
	List(ctx context.Context) ([]domain.Dataset, error)
	Delete(ctx context.Context, id uuid.UUID) error

	ReplaceColumns(ctx context.Context, datasetID uuid.UUID, cols []domain.Column) error
	SaveColumnProfile(ctx context.Context, columnID uuid.UUID, p *domain.ColumnProfile) error
	SaveColumnRole(ctx context.Context, columnID uuid.UUID, role domain.SemanticRole, comment string) error
}

type RelationRepository interface {
	Replace(ctx context.Context, rels []domain.Relation) error
	List(ctx context.Context) ([]domain.Relation, error)
	ListForDataset(ctx context.Context, datasetID uuid.UUID) ([]domain.Relation, error)
	DeleteForDataset(ctx context.Context, datasetID uuid.UUID) error
}
