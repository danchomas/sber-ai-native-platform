package domain

import (
	"context"

	"github.com/google/uuid"
)

type SourceRepository interface {
	Create(ctx context.Context, s *Source) error
	Update(ctx context.Context, s *Source) error
	Get(ctx context.Context, id uuid.UUID) (*Source, error)
	GetByName(ctx context.Context, name string) (*Source, error)
	List(ctx context.Context) ([]Source, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type SourceIntrospector interface {
	Ping(ctx context.Context, s *Source) error
	Tables(ctx context.Context, s *Source) ([]ExternalTable, error)
	Columns(ctx context.Context, s *Source, schema, table string) ([]Column, error)
}

type ExternalTable struct {
	Schema       string `json:"schema"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	EstimateRows int64  `json:"estimate_rows"`
	Attached     bool   `json:"attached"`
}

type DatasetRepository interface {
	Create(ctx context.Context, d *Dataset) error
	Update(ctx context.Context, d *Dataset) error
	SetStatus(ctx context.Context, id uuid.UUID, status DatasetStatus, failReason string) error
	Get(ctx context.Context, id uuid.UUID) (*Dataset, error)
	GetByName(ctx context.Context, name string) (*Dataset, error)
	List(ctx context.Context) ([]Dataset, error)
	Delete(ctx context.Context, id uuid.UUID) error

	ReplaceColumns(ctx context.Context, datasetID uuid.UUID, cols []Column) error
	SaveColumnProfile(ctx context.Context, columnID uuid.UUID, p *ColumnProfile) error
	SaveColumnRole(ctx context.Context, columnID uuid.UUID, role SemanticRole, comment string) error
}

type RelationRepository interface {
	Replace(ctx context.Context, rels []Relation) error
	List(ctx context.Context) ([]Relation, error)
	ListForDataset(ctx context.Context, datasetID uuid.UUID) ([]Relation, error)
	DeleteForDataset(ctx context.Context, datasetID uuid.UUID) error
}

type RawDataRepository interface {
	CreateTable(ctx context.Context, table string, cols []Column) error
	DropTable(ctx context.Context, table string) error
	CopyRows(ctx context.Context, table string, cols []Column, rows [][]any) (int64, error)
	Analyze(ctx context.Context, table string) error
	RowCount(ctx context.Context, table string) (int64, error)
	TableSizeBytes(ctx context.Context, table string) (int64, error)

	ProfileColumn(ctx context.Context, table string, col Column, topN int) (*ColumnProfile, error)
	TimeSpan(ctx context.Context, table string, col Column) (*TimeSpan, error)
	DuplicateRows(ctx context.Context, table string, cols []Column) (int64, error)
	Sample(ctx context.Context, table string, limit int) (*ResultSet, error)
	ValueOverlap(ctx context.Context, left, right TableRef) (share float64, matched int64, err error)

	Query(ctx context.Context, sql string, maxRows int) (*ResultSet, error)
	ExplainCost(ctx context.Context, sql string) (float64, error)
}

type SessionRepository interface {
	CreateSession(ctx context.Context, s *Session) error
	GetSession(ctx context.Context, id uuid.UUID) (*Session, error)
	ListSessions(ctx context.Context, limit int) ([]Session, error)
	RenameSession(ctx context.Context, id uuid.UUID, title string) error
	TouchSession(ctx context.Context, id uuid.UUID) error
	DeleteSession(ctx context.Context, id uuid.UUID) error

	AddMessage(ctx context.Context, m *Message) error
	ListMessages(ctx context.Context, sessionID uuid.UUID, limit int) ([]Message, error)
	LastTurns(ctx context.Context, sessionID uuid.UUID, limit int) ([]Turn, error)
}

type DashboardRepository interface {
	Create(ctx context.Context, d *Dashboard) error
	Get(ctx context.Context, id uuid.UUID) (*Dashboard, error)
	List(ctx context.Context, limit int) ([]Dashboard, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type Completion struct {
	System      string
	Messages    []ChatMessage
	Temperature float64
	MaxTokens   int
	JSONOnly    bool
}

type ChatMessage struct {
	Role    MessageRole
	Content string
}

type LLM interface {
	Complete(ctx context.Context, req Completion) (string, error)
	Model() string
	Ping(ctx context.Context) error
}
