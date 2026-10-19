package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
	corepg "github.com/heinousness/aibi/internal/core/repository/postgres"
)

type DatasetRepository struct {
	pool *pgxpool.Pool
}

func NewDatasetRepository(pool *pgxpool.Pool) *DatasetRepository {
	return &DatasetRepository{pool: pool}
}

const datasetColumns = `d.id, d.name, d.origin, d.source_id, COALESCE(s.name, ''),
	d.schema_name, d.table_name, d.source_file, d.delimiter, d.row_count,
	d.size_bytes, d.status, d.fail_reason, d.description, d.created_at, d.updated_at`

const datasetFrom = ` FROM datasets d LEFT JOIN sources s ON s.id = d.source_id`

func (r *DatasetRepository) Create(ctx context.Context, d *domain.Dataset) error {
	const q = `
		INSERT INTO datasets (id, name, origin, source_id, schema_name, table_name,
		                      source_file, delimiter, row_count, size_bytes,
		                      status, fail_reason, description)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		d.ID, d.Name, d.Origin, d.SourceID, d.Schema, d.TableName,
		d.SourceFile, d.Delimiter, d.RowCount, d.SizeBytes,
		d.Status, d.FailReason, d.Description,
	).Scan(&d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return fmt.Errorf("создание датасета: %w", corepg.MapError(err))
	}
	return nil
}

func (r *DatasetRepository) Update(ctx context.Context, d *domain.Dataset) error {
	const q = `
		UPDATE datasets
		   SET name = $2, row_count = $3, size_bytes = $4, status = $5,
		       fail_reason = $6, description = $7, updated_at = now()
		 WHERE id = $1
		RETURNING updated_at`
	err := r.pool.QueryRow(ctx, q,
		d.ID, d.Name, d.RowCount, d.SizeBytes, d.Status, d.FailReason, d.Description,
	).Scan(&d.UpdatedAt)
	if err != nil {
		return fmt.Errorf("обновление датасета: %w", corepg.MapError(err))
	}
	return nil
}

func (r *DatasetRepository) SetStatus(ctx context.Context, id uuid.UUID, status domain.DatasetStatus, failReason string) error {
	const q = `UPDATE datasets SET status = $2, fail_reason = $3, updated_at = now() WHERE id = $1`
	tag, err := r.pool.Exec(ctx, q, id, status, failReason)
	if err != nil {
		return fmt.Errorf("смена статуса датасета: %w", corepg.MapError(err))
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *DatasetRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Dataset, error) {
	q := `SELECT ` + datasetColumns + datasetFrom + ` WHERE d.id = $1`
	d, err := r.scanOne(ctx, q, id)
	if err != nil {
		return nil, err
	}
	cols, err := r.listColumns(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	d.Columns = cols
	return d, nil
}

func (r *DatasetRepository) GetByName(ctx context.Context, name string) (*domain.Dataset, error) {
	q := `SELECT ` + datasetColumns + datasetFrom + ` WHERE d.name = $1`
	d, err := r.scanOne(ctx, q, name)
	if err != nil {
		return nil, err
	}
	cols, err := r.listColumns(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	d.Columns = cols
	return d, nil
}

func (r *DatasetRepository) List(ctx context.Context) ([]domain.Dataset, error) {
	q := `SELECT ` + datasetColumns + datasetFrom + ` ORDER BY d.created_at`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("список датасетов: %w", corepg.MapError(err))
	}
	defer rows.Close()

	var out []domain.Dataset
	for rows.Next() {
		d, err := scanDataset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, corepg.MapError(err)
	}

	byDataset, err := r.allColumns(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Columns = byDataset[out[i].ID]
	}
	return out, nil
}

func (r *DatasetRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM datasets WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("удаление датасета: %w", corepg.MapError(err))
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *DatasetRepository) ReplaceColumns(ctx context.Context, datasetID uuid.UUID, cols []domain.Column) error {
	return corepg.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM dataset_columns WHERE dataset_id = $1`, datasetID); err != nil {
			return corepg.MapError(err)
		}
		batch := &pgx.Batch{}
		for _, c := range cols {
			var profile []byte
			if c.Profile != nil {
				b, err := json.Marshal(c.Profile)
				if err != nil {
					return fmt.Errorf("сериализация профиля колонки %s: %w", c.Name, err)
				}
				profile = b
			}
			batch.Queue(`
				INSERT INTO dataset_columns (id, dataset_id, position, source_name, name,
				                             data_type, role, nullable, comment, profile)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
				c.ID, datasetID, c.Position, c.SourceName, c.Name,
				c.Type, c.Role, c.Nullable, c.Comment, profile)
		}
		return corepg.MapError(tx.SendBatch(ctx, batch).Close())
	})
}

func (r *DatasetRepository) SaveColumnProfile(ctx context.Context, columnID uuid.UUID, p *domain.ColumnProfile) error {
	payload, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("сериализация профиля: %w", err)
	}
	const q = `UPDATE dataset_columns SET profile = $2 WHERE id = $1`
	tag, err := r.pool.Exec(ctx, q, columnID, payload)
	if err != nil {
		return fmt.Errorf("сохранение профиля колонки: %w", corepg.MapError(err))
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *DatasetRepository) SaveColumnRole(ctx context.Context, columnID uuid.UUID, role domain.SemanticRole, comment string) error {
	const q = `UPDATE dataset_columns SET role = $2, comment = CASE WHEN $3 = '' THEN comment ELSE $3 END WHERE id = $1`
	tag, err := r.pool.Exec(ctx, q, columnID, role, comment)
	if err != nil {
		return fmt.Errorf("сохранение роли колонки: %w", corepg.MapError(err))
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *DatasetRepository) scanOne(ctx context.Context, q string, args ...any) (*domain.Dataset, error) {
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
	return scanDataset(rows)
}

func scanDataset(rows pgx.Rows) (*domain.Dataset, error) {
	var d domain.Dataset
	err := rows.Scan(&d.ID, &d.Name, &d.Origin, &d.SourceID, &d.SourceName,
		&d.Schema, &d.TableName, &d.SourceFile, &d.Delimiter,
		&d.RowCount, &d.SizeBytes, &d.Status, &d.FailReason, &d.Description,
		&d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("чтение датасета: %w", corepg.MapError(err))
	}
	return &d, nil
}

const columnFields = `id, dataset_id, position, source_name, name, data_type,
	role, nullable, comment, profile`

func (r *DatasetRepository) listColumns(ctx context.Context, datasetID uuid.UUID) ([]domain.Column, error) {
	q := `SELECT ` + columnFields + ` FROM dataset_columns WHERE dataset_id = $1 ORDER BY position`
	rows, err := r.pool.Query(ctx, q, datasetID)
	if err != nil {
		return nil, fmt.Errorf("список колонок: %w", corepg.MapError(err))
	}
	defer rows.Close()
	return scanColumns(rows)
}

func (r *DatasetRepository) allColumns(ctx context.Context) (map[uuid.UUID][]domain.Column, error) {
	q := `SELECT ` + columnFields + ` FROM dataset_columns ORDER BY dataset_id, position`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("список колонок каталога: %w", corepg.MapError(err))
	}
	defer rows.Close()

	cols, err := scanColumns(rows)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID][]domain.Column, 8)
	for _, c := range cols {
		out[c.DatasetID] = append(out[c.DatasetID], c)
	}
	return out, nil
}

func scanColumns(rows pgx.Rows) ([]domain.Column, error) {
	var out []domain.Column
	for rows.Next() {
		var (
			c       domain.Column
			profile []byte
		)
		if err := rows.Scan(&c.ID, &c.DatasetID, &c.Position, &c.SourceName, &c.Name,
			&c.Type, &c.Role, &c.Nullable, &c.Comment, &profile); err != nil {
			return nil, fmt.Errorf("чтение колонки: %w", corepg.MapError(err))
		}
		if len(profile) > 0 {
			var p domain.ColumnProfile
			if err := json.Unmarshal(profile, &p); err == nil {
				c.Profile = &p
			}
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, corepg.MapError(err)
	}
	return out, nil
}
