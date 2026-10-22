package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/llm"
)

func (s *Service) Models(ctx context.Context, id uuid.UUID) ([]llm.CatalogEntry, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.client(p).Catalog(ctx)
}
