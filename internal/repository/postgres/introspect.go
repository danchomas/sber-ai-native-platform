package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
)

type Introspector struct {
	registry *Registry
}

func NewIntrospector(registry *Registry) *Introspector {
	return &Introspector{registry: registry}
}

var _ domain.SourceIntrospector = (*Introspector)(nil)

func (i *Introspector) Ping(ctx context.Context, s *domain.Source) error {
	pool, err := i.registry.Open(ctx, s)
	if err != nil {
		return err
	}
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("источник недоступен: %w", queryError(err))
	}
	return nil
}

func (i *Introspector) Tables(ctx context.Context, s *domain.Source) ([]domain.ExternalTable, error) {
	pool, err := i.registry.Open(ctx, s)
	if err != nil {
		return nil, err
	}

	const q = `
		SELECT n.nspname,
		       c.relname,
		       CASE c.relkind WHEN 'v' THEN 'view' WHEN 'm' THEN 'view' ELSE 'table' END,
		       GREATEST(c.reltuples, 0)::bigint
		  FROM pg_class c
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
		   AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		   AND n.nspname NOT LIKE 'pg_temp%'
		   AND n.nspname NOT LIKE 'pg_toast_temp%'
		   AND has_table_privilege(c.oid, 'SELECT')
		 ORDER BY n.nspname, c.relname`

	rows, err := pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("чтение списка таблиц источника: %w", queryError(err))
	}
	defer rows.Close()

	var out []domain.ExternalTable
	for rows.Next() {
		var t domain.ExternalTable
		if err := rows.Scan(&t.Schema, &t.Name, &t.Kind, &t.EstimateRows); err != nil {
			return nil, fmt.Errorf("чтение таблицы источника: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, queryError(err)
	}
	return out, nil
}

func (i *Introspector) Columns(ctx context.Context, s *domain.Source, schema, table string) ([]domain.Column, error) {
	pool, err := i.registry.Open(ctx, s)
	if err != nil {
		return nil, err
	}

	const q = `
		SELECT a.attname,
		       format_type(a.atttypid, a.atttypmod),
		       NOT a.attnotnull,
		       a.attnum
		  FROM pg_attribute a
		  JOIN pg_class c      ON c.oid = a.attrelid
		  JOIN pg_namespace n  ON n.oid = c.relnamespace
		 WHERE n.nspname = $1
		   AND c.relname = $2
		   AND a.attnum > 0
		   AND NOT a.attisdropped
		 ORDER BY a.attnum`

	rows, err := pool.Query(ctx, q, schema, table)
	if err != nil {
		return nil, fmt.Errorf("чтение колонок таблицы %s.%s: %w", schema, table, queryError(err))
	}
	defer rows.Close()

	var out []domain.Column
	for rows.Next() {
		var (
			name     string
			pgType   string
			nullable bool
			position int
		)
		if err := rows.Scan(&name, &pgType, &nullable, &position); err != nil {
			return nil, fmt.Errorf("чтение колонки источника: %w", err)
		}
		out = append(out, domain.Column{
			ID:         uuid.New(),
			Position:   position,
			SourceName: name,
			Name:       name,
			Type:       columnTypeFromPG(pgType),
			Nullable:   nullable,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, queryError(err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("таблица %s.%s: %w", schema, table, apperr.ErrNotFound)
	}
	return out, nil
}

func columnTypeFromPG(pgType string) domain.ColumnType {
	t := strings.ToLower(strings.TrimSpace(pgType))

	if strings.HasSuffix(t, "[]") {
		return domain.ColumnTypeText
	}

	if i := strings.IndexByte(t, '('); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	t = strings.TrimPrefix(t, "pg_catalog.")

	switch t {
	case "smallint", "integer", "bigint", "int2", "int4", "int8":
		return domain.ColumnTypeInteger
	case "numeric", "decimal", "real", "double precision", "float4", "float8", "money":
		return domain.ColumnTypeNumeric
	case "boolean", "bool":
		return domain.ColumnTypeBoolean
	case "date":
		return domain.ColumnTypeDate
	case "timestamp", "timestamptz", "timestamp without time zone",
		"timestamp with time zone":
		return domain.ColumnTypeTimestamp
	default:
		return domain.ColumnTypeText
	}
}
