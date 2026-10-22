package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/domain"
)

func (s *Service) Activate(ctx context.Context, id uuid.UUID, model string) (*domain.Provider, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if m := normalized(model); m != "" && m != p.Model {
		p.Model = m
	}

	if err := s.client(p).Ping(ctx); err != nil {
		p.Status = domain.ProviderStatusFailed
		p.FailReason = err.Error()
		if saveErr := s.repo.Update(ctx, p); saveErr != nil {
			s.log.WarnContext(ctx, "состояние провайдера не сохранено",
				"provider", p.Name, "error", saveErr)
		}
		return nil, fmt.Errorf("провайдер не отвечает: %w", err)
	}

	p.Status = domain.ProviderStatusReady
	p.FailReason = ""
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	if err := s.repo.Activate(ctx, id); err != nil {
		return nil, err
	}
	if err := s.refresh(ctx); err != nil {
		return nil, err
	}

	p.Active = true
	return p, nil
}

func (s *Service) Deactivate(ctx context.Context) error {
	if err := s.repo.Deactivate(ctx); err != nil {
		return err
	}
	s.set(nil)
	s.log.InfoContext(ctx, "провайдер отключён, работаем на модели из конфигурации")
	return nil
}
