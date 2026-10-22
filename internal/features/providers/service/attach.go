package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/llm"
)

func (s *Service) Attach(ctx context.Context, p domain.Provider) (*domain.Provider, error) {
	p.Name = normalized(p.Name)
	p.Model = normalized(p.Model)
	p.BaseURL = normalized(p.BaseURL)
	p.APIKey = normalized(p.APIKey)

	if p.Name == "" {
		return nil, apperr.NewValidationError("name", "имя провайдера не может быть пустым")
	}
	if p.Model == "" {
		return nil, apperr.NewValidationError("model", "нужно указать модель")
	}

	switch p.Kind {
	case domain.ProviderOpenRouter:
		if p.BaseURL == "" {
			p.BaseURL = llm.OpenRouterBaseURL
		}
		if p.APIKey == "" {
			return nil, apperr.NewValidationError("api_key", "OpenRouter требует ключ доступа")
		}
	case domain.ProviderOpenAICompatible:
		if p.BaseURL == "" {
			return nil, apperr.NewValidationError("base_url", "нужно указать адрес сервера")
		}
	default:
		return nil, apperr.NewValidationError("kind",
			"поддерживаются openai_compatible и openrouter")
	}

	if _, err := s.repo.GetByName(ctx, p.Name); err == nil {
		return nil, fmt.Errorf("провайдер %q: %w", p.Name, apperr.ErrAlreadyExists)
	}

	p.ID = uuid.New()
	p.Status = domain.ProviderStatusReady
	p.Active = false

	if err := s.client(&p).Ping(ctx); err != nil {
		return nil, fmt.Errorf("подключение не удалось: %w", err)
	}
	if err := s.repo.Create(ctx, &p); err != nil {
		return nil, err
	}

	s.log.InfoContext(ctx, "провайдер модели подключён",
		"provider", p.Name, "kind", p.Kind, "model", p.Model)
	return &p, nil
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	if p.Active {
		s.set(nil)
		s.log.InfoContext(ctx, "активный провайдер удалён, работаем на модели из конфигурации",
			"provider", p.Name)
	}
	return nil
}
