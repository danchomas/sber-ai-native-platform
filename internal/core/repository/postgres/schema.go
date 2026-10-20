package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func EnsureRawSchema(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	if schema == "" {
		return fmt.Errorf("имя схемы сырых данных не задано")
	}
	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+QuoteIdent(schema)); err != nil {
		return fmt.Errorf("создание схемы %s: %w", schema, MapError(err))
	}
	return nil
}
