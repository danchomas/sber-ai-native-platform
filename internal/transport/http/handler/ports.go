package handler

import (
	"context"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/domain"
	"github.com/heinousness/aibi/internal/service/ingest"
)

type Ingester interface {
	Upload(ctx context.Context, req ingest.Request) (*ingest.Report, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type Catalog interface {
	List(ctx context.Context) ([]domain.Dataset, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.Dataset, error)
	Profile(ctx context.Context, id uuid.UUID) (*domain.Profile, error)
	Preview(ctx context.Context, id uuid.UUID, limit int) (*domain.ResultSet, error)
	Relations(ctx context.Context) ([]domain.Relation, error)
	Scopes(ctx context.Context) ([]domain.Scope, error)
	Schema() string
}

type Sources interface {
	Attach(ctx context.Context, name, dsn string) (*domain.Source, error)
	List(ctx context.Context) ([]domain.Source, error)
	Tables(ctx context.Context, id uuid.UUID) ([]domain.ExternalTable, error)
	AttachTable(ctx context.Context, id uuid.UUID, schema, table, name string) (*domain.Dataset, error)
	Detach(ctx context.Context, id uuid.UUID) error
}

type Chat interface {
	Create(ctx context.Context, title string, datasetIDs []uuid.UUID) (*domain.Session, error)
	List(ctx context.Context, limit int) ([]domain.Session, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.Session, error)
	Messages(ctx context.Context, id uuid.UUID, limit int) ([]domain.Message, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Ask(ctx context.Context, sessionID uuid.UUID, question string) (*domain.Answer, error)
}

type Dashboards interface {
	List(ctx context.Context, limit int) ([]domain.Dashboard, error)
	Get(ctx context.Context, id uuid.UUID, refresh bool) (*domain.Dashboard, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type Executor interface {
	Query(ctx context.Context, target domain.QueryTarget, sql string, maxRows int) (*domain.ResultSet, error)
}
