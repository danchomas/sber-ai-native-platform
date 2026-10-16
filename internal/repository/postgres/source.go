package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heinousness/aibi/internal/domain"
)

type SourceRepository struct {
	pool *pgxpool.Pool
}

func NewSourceRepository(pool *pgxpool.Pool) *SourceRepository {
	return &SourceRepository{pool: pool}
}

var _ domain.SourceRepository = (*SourceRepository)(nil)

const sourceColumns = `id, name, kind, dsn, status, fail_reason, created_at, updated_at`

func (r *SourceRepository) Create(ctx context.Context, s *domain.Source) error {
	const q = `
		INSERT INTO sources (id, name, kind, dsn, status, fail_reason)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		s.ID, s.Name, s.Kind, s.DSN, s.Status, s.FailReason,
	).Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("регистрация источника: %w", mapError(err))
	}
	return nil
}

func (r *SourceRepository) Update(ctx context.Context, s *domain.Source) error {
	const q = `
		UPDATE sources
		   SET name = $2, dsn = $3, status = $4, fail_reason = $5, updated_at = now()
		 WHERE id = $1
		RETURNING updated_at`
	err := r.pool.QueryRow(ctx, q,
		s.ID, s.Name, s.DSN, s.Status, s.FailReason,
	).Scan(&s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("обновление источника: %w", mapError(err))
	}
	return nil
}

func (r *SourceRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Source, error) {
	q := `SELECT ` + sourceColumns + ` FROM sources WHERE id = $1`
	return r.scanOne(ctx, q, id)
}

func (r *SourceRepository) GetByName(ctx context.Context, name string) (*domain.Source, error) {
	q := `SELECT ` + sourceColumns + ` FROM sources WHERE name = $1`
	return r.scanOne(ctx, q, name)
}

func (r *SourceRepository) List(ctx context.Context) ([]domain.Source, error) {
	q := `SELECT ` + sourceColumns + ` FROM sources ORDER BY created_at`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("список источников: %w", mapError(err))
	}
	defer rows.Close()

	var out []domain.Source
	for rows.Next() {
		s, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return out, nil
}

func (r *SourceRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM sources WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("удаление источника: %w", mapError(err))
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *SourceRepository) scanOne(ctx context.Context, q string, args ...any) (*domain.Source, error) {
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, mapError(err)
		}
		return nil, domain.ErrNotFound
	}
	return scanSource(rows)
}

func scanSource(rows pgx.Rows) (*domain.Source, error) {
	var s domain.Source
	err := rows.Scan(&s.ID, &s.Name, &s.Kind, &s.DSN, &s.Status,
		&s.FailReason, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("чтение источника: %w", mapError(err))
	}
	return &s, nil
}
