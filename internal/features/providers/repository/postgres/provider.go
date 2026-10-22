package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
	corepg "github.com/heinousness/aibi/internal/core/repository/postgres"
)

type ProviderRepository struct {
	pool *pgxpool.Pool
}

func NewProviderRepository(pool *pgxpool.Pool) *ProviderRepository {
	return &ProviderRepository{pool: pool}
}

const providerColumns = `id, name, kind, base_url, api_key, model, is_active,
	status, fail_reason, created_at, updated_at`

func (r *ProviderRepository) Create(ctx context.Context, p *domain.Provider) error {
	const q = `
		INSERT INTO llm_providers (id, name, kind, base_url, api_key, model,
		                           is_active, status, fail_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		p.ID, p.Name, p.Kind, p.BaseURL, p.APIKey, p.Model,
		p.Active, p.Status, p.FailReason,
	).Scan(&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("сохранение провайдера: %w", corepg.MapError(err))
	}
	return nil
}

func (r *ProviderRepository) Update(ctx context.Context, p *domain.Provider) error {
	const q = `
		UPDATE llm_providers
		   SET name = $2, base_url = $3, api_key = $4, model = $5,
		       status = $6, fail_reason = $7, updated_at = now()
		 WHERE id = $1
		RETURNING updated_at`
	err := r.pool.QueryRow(ctx, q,
		p.ID, p.Name, p.BaseURL, p.APIKey, p.Model, p.Status, p.FailReason,
	).Scan(&p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("обновление провайдера: %w", corepg.MapError(err))
	}
	return nil
}

func (r *ProviderRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Provider, error) {
	q := `SELECT ` + providerColumns + ` FROM llm_providers WHERE id = $1`
	return r.scanOne(ctx, q, id)
}

func (r *ProviderRepository) GetByName(ctx context.Context, name string) (*domain.Provider, error) {
	q := `SELECT ` + providerColumns + ` FROM llm_providers WHERE name = $1`
	return r.scanOne(ctx, q, name)
}

func (r *ProviderRepository) Active(ctx context.Context) (*domain.Provider, error) {
	q := `SELECT ` + providerColumns + ` FROM llm_providers WHERE is_active LIMIT 1`
	return r.scanOne(ctx, q)
}

func (r *ProviderRepository) List(ctx context.Context) ([]domain.Provider, error) {
	q := `SELECT ` + providerColumns + ` FROM llm_providers ORDER BY created_at`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("список провайдеров: %w", corepg.MapError(err))
	}
	defer rows.Close()

	var out []domain.Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, corepg.MapError(rows.Err())
}

func (r *ProviderRepository) Activate(ctx context.Context, id uuid.UUID) error {
	return corepg.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE llm_providers SET is_active = false, updated_at = now() WHERE is_active`); err != nil {
			return corepg.MapError(err)
		}
		tag, err := tx.Exec(ctx,
			`UPDATE llm_providers SET is_active = true, updated_at = now() WHERE id = $1`, id)
		if err != nil {
			return corepg.MapError(err)
		}
		if tag.RowsAffected() == 0 {
			return apperr.ErrNotFound
		}
		return nil
	})
}

func (r *ProviderRepository) Deactivate(ctx context.Context) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE llm_providers SET is_active = false, updated_at = now() WHERE is_active`)
	if err != nil {
		return fmt.Errorf("отключение провайдера: %w", corepg.MapError(err))
	}
	return nil
}

func (r *ProviderRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM llm_providers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("удаление провайдера: %w", corepg.MapError(err))
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *ProviderRepository) scanOne(ctx context.Context, q string, args ...any) (*domain.Provider, error) {
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, corepg.MapError(err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, corepg.MapError(err)
		}
		return nil, apperr.ErrNotFound
	}
	return scanProvider(rows)
}

func scanProvider(rows pgx.Rows) (*domain.Provider, error) {
	var p domain.Provider
	err := rows.Scan(&p.ID, &p.Name, &p.Kind, &p.BaseURL, &p.APIKey, &p.Model,
		&p.Active, &p.Status, &p.FailReason, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("чтение провайдера: %w", corepg.MapError(err))
	}
	return &p, nil
}
