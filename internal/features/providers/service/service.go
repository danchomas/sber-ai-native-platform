package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/llm"
)

type Service struct {
	repo     Providers
	fallback domain.LLM
	cfg      config.LLM
	log      *slog.Logger

	mu      sync.RWMutex
	current domain.LLM
}

func New(repo Providers, fallback domain.LLM, cfg config.LLM, log *slog.Logger) *Service {
	return &Service{repo: repo, fallback: fallback, cfg: cfg, log: log}
}

func (s *Service) Current(context.Context) domain.LLM {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current != nil {
		return s.current
	}
	return s.fallback
}

func (s *Service) Restore(ctx context.Context) {
	if err := s.refresh(ctx); err != nil {
		s.log.WarnContext(ctx, "активный провайдер не восстановлен, работаем на модели из конфигурации",
			"error", err)
	}
}

func (s *Service) refresh(ctx context.Context) error {
	p, err := s.repo.Active(ctx)
	if errors.Is(err, apperr.ErrNotFound) {
		s.set(nil)
		return nil
	}
	if err != nil {
		return err
	}

	s.set(s.client(p))
	s.log.InfoContext(ctx, "активный провайдер модели",
		"provider", p.Name, "kind", p.Kind, "model", p.Model)
	return nil
}

func (s *Service) set(m domain.LLM) {
	s.mu.Lock()
	s.current = m
	s.mu.Unlock()
}

func (s *Service) client(p *domain.Provider) *llm.Client {
	cfg := s.cfg
	cfg.BaseURL = p.BaseURL
	cfg.APIKey = p.APIKey
	cfg.Model = p.Model

	if p.Kind == domain.ProviderOpenRouter {
		return llm.NewOpenRouter(cfg)
	}
	return llm.New(cfg)
}

func (s *Service) List(ctx context.Context) ([]domain.Provider, error) {
	return s.repo.List(ctx)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Provider, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) Fallback() string {
	if s.fallback == nil {
		return ""
	}
	return s.fallback.Model()
}

func normalized(v string) string { return strings.TrimSpace(v) }
