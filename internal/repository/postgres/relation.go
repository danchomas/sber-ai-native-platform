package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heinousness/aibi/internal/domain"
)

type RelationRepository struct {
	pool *pgxpool.Pool
}

func NewRelationRepository(pool *pgxpool.Pool) *RelationRepository {
	return &RelationRepository{pool: pool}
}

var _ domain.RelationRepository = (*RelationRepository)(nil)

func (r *RelationRepository) Replace(ctx context.Context, rels []domain.Relation) error {
	return withTx(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM dataset_relations`); err != nil {
			return mapError(err)
		}
		if len(rels) == 0 {
			return nil
		}
		batch := &pgx.Batch{}
		for _, rel := range rels {
			batch.Queue(`
				INSERT INTO dataset_relations (left_dataset_id, left_column, right_dataset_id,
				                               right_column, overlap, matched_values, cardinality, confidence)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
				ON CONFLICT (left_dataset_id, left_column, right_dataset_id, right_column)
				DO UPDATE SET overlap = EXCLUDED.overlap,
				              matched_values = EXCLUDED.matched_values,
				              cardinality = EXCLUDED.cardinality,
				              confidence = EXCLUDED.confidence`,
				rel.LeftDatasetID, rel.LeftColumn, rel.RightDatasetID, rel.RightColumn,
				rel.Overlap, rel.MatchedValues, rel.Cardinality, rel.Confidence)
		}
		return mapError(tx.SendBatch(ctx, batch).Close())
	})
}

const relationQuery = `
	SELECT r.left_dataset_id, dl.name, r.left_column,
	       r.right_dataset_id, dr.name, r.right_column,
	       r.overlap, r.matched_values, r.cardinality, r.confidence
	  FROM dataset_relations r
	  JOIN datasets dl ON dl.id = r.left_dataset_id
	  JOIN datasets dr ON dr.id = r.right_dataset_id`

func (r *RelationRepository) List(ctx context.Context) ([]domain.Relation, error) {
	rows, err := r.pool.Query(ctx, relationQuery+` ORDER BY r.confidence DESC, dl.name, dr.name`)
	if err != nil {
		return nil, fmt.Errorf("список связей: %w", mapError(err))
	}
	defer rows.Close()
	return scanRelations(rows)
}

func (r *RelationRepository) ListForDataset(ctx context.Context, datasetID uuid.UUID) ([]domain.Relation, error) {
	rows, err := r.pool.Query(ctx,
		relationQuery+` WHERE r.left_dataset_id = $1 OR r.right_dataset_id = $1
		                ORDER BY r.confidence DESC`, datasetID)
	if err != nil {
		return nil, fmt.Errorf("связи датасета: %w", mapError(err))
	}
	defer rows.Close()
	return scanRelations(rows)
}

func (r *RelationRepository) DeleteForDataset(ctx context.Context, datasetID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM dataset_relations WHERE left_dataset_id = $1 OR right_dataset_id = $1`, datasetID)
	return mapError(err)
}

func scanRelations(rows pgx.Rows) ([]domain.Relation, error) {
	var out []domain.Relation
	for rows.Next() {
		var rel domain.Relation
		if err := rows.Scan(&rel.LeftDatasetID, &rel.LeftDataset, &rel.LeftColumn,
			&rel.RightDatasetID, &rel.RightDataset, &rel.RightColumn,
			&rel.Overlap, &rel.MatchedValues, &rel.Cardinality, &rel.Confidence); err != nil {
			return nil, mapError(err)
		}
		out = append(out, rel)
	}
	return out, mapError(rows.Err())
}
