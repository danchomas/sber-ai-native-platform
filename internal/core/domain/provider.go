package domain

import (
	"time"

	"github.com/google/uuid"
)

type ProviderKind string

const (
	ProviderOpenAICompatible ProviderKind = "openai_compatible"
	ProviderOpenRouter       ProviderKind = "openrouter"
)

type ProviderStatus string

const (
	ProviderStatusReady  ProviderStatus = "ready"
	ProviderStatusFailed ProviderStatus = "failed"
)

type Provider struct {
	ID         uuid.UUID      `json:"id"`
	Name       string         `json:"name"`
	Kind       ProviderKind   `json:"kind"`
	BaseURL    string         `json:"base_url"`
	APIKey     string         `json:"-"`
	Model      string         `json:"model"`
	Active     bool           `json:"active"`
	Status     ProviderStatus `json:"status"`
	FailReason string         `json:"fail_reason,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

func (p *Provider) Ready() bool { return p.Status == ProviderStatusReady }

func (p *Provider) HasKey() bool { return p.APIKey != "" }
