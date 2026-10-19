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

type QueryTarget struct {
	SourceID *uuid.UUID
	Schemas  []string
}

func LocalTarget(schema string) QueryTarget {
	return QueryTarget{Schemas: []string{schema}}
}

type Scope struct {
	SourceName string
	Target     QueryTarget
	Datasets   []Dataset
}

func (s Scope) Local() bool { return s.Target.SourceID == nil }

type DatasetOrigin string

const (
	OriginUploaded DatasetOrigin = "uploaded"
	OriginAttached DatasetOrigin = "attached"
)

type ExternalTable struct {
	Schema       string `json:"schema"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	EstimateRows int64  `json:"estimate_rows"`
	Attached     bool   `json:"attached"`
}
