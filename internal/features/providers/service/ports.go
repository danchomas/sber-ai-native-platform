package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/domain"
)

type Providers interface {
	Create(ctx context.Context, p *domain.Provider) error
	Update(ctx context.Context, p *domain.Provider) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Provider, error)
	GetByName(ctx context.Context, name string) (*domain.Provider, error)
	Active(ctx context.Context) (*domain.Provider, error)
	List(ctx context.Context) ([]domain.Provider, error)
	Activate(ctx context.Context, id uuid.UUID) error
	Deactivate(ctx context.Context) error
	Delete(ctx context.Context, id uuid.UUID) error
}
