package domain

import (
	"time"

	"github.com/google/uuid"
)

type SourceKind string

const (
	SourceKindPostgres SourceKind = "postgres"
)

type SourceStatus string

const (
	SourceStatusReady  SourceStatus = "ready"
	SourceStatusFailed SourceStatus = "failed"
)

type Source struct {
	ID         uuid.UUID    `json:"id"`
	Name       string       `json:"name"`
	Kind       SourceKind   `json:"kind"`
	DSN        string       `json:"-"`
	Status     SourceStatus `json:"status"`
	FailReason string       `json:"fail_reason,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
}

func (s *Source) Ready() bool { return s.Status == SourceStatusReady }

type DatasetOrigin string

const (
	OriginUploaded DatasetOrigin = "uploaded"
	OriginAttached DatasetOrigin = "attached"
)
