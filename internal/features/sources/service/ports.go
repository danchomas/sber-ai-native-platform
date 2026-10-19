package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/domain"
)

type SourceRepository interface {
	Create(ctx context.Context, s *domain.Source) error
	Update(ctx context.Context, s *domain.Source) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Source, error)
	GetByName(ctx context.Context, name string) (*domain.Source, error)
	List(ctx context.Context) ([]domain.Source, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type SourceIntrospector interface {
	Ping(ctx context.Context, s *domain.Source) error
	Tables(ctx context.Context, s *domain.Source) ([]domain.ExternalTable, error)
	Columns(ctx context.Context, s *domain.Source, schema, table string) ([]domain.Column, error)
}

type Datasets interface {
	Create(ctx context.Context, d *domain.Dataset) error
	Update(ctx context.Context, d *domain.Dataset) error
	SetStatus(ctx context.Context, id uuid.UUID, status domain.DatasetStatus, failReason string) error
	GetByName(ctx context.Context, name string) (*domain.Dataset, error)
	List(ctx context.Context) ([]domain.Dataset, error)
	ReplaceColumns(ctx context.Context, datasetID uuid.UUID, cols []domain.Column) error
}
