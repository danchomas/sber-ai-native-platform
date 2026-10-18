package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heinousness/aibi/internal/core/domain"
)

const maxDistinctScan = 100_000

type RawDataRepository struct {
	registry     *Registry
	schema       string
	queryTimeout time.Duration
}

func NewRawDataRepository(registry *Registry, schema string, queryTimeout time.Duration) *RawDataRepository {
	return &RawDataRepository{registry: registry, schema: schema, queryTimeout: queryTimeout}
}

var _ domain.RawDataRepository = (*RawDataRepository)(nil)

func (r *RawDataRepository) Schema() string { return r.schema }

func (r *RawDataRepository) local() *pgxpool.Pool { return r.registry.Local() }

func (r *RawDataRepository) qualified(table string) string {
	return quoteIdent(r.schema) + "." + quoteIdent(table)
}

func (r *RawDataRepository) target(ctx context.Context, ref domain.TableRef) (*pgxpool.Pool, string, error) {
	pool, err := r.registry.For(ctx, ref.SourceID)
	if err != nil {
		return nil, "", err
	}
	schema := ref.Schema
	if schema == "" {
		schema = r.schema
	}
	return pool, quoteIdent(schema) + "." + quoteIdent(ref.Table), nil
}

func (r *RawDataRepository) CreateTable(ctx context.Context, table string, cols []domain.Column) error {
	if len(cols) == 0 {
		return domain.NewValidationError("columns", "нет колонок для создания таблицы")
	}

	defs := make([]string, 0, len(cols))
	for _, c := range cols {
		defs = append(defs, fmt.Sprintf("%s %s", quoteIdent(c.Name), string(c.Type)))
	}
	ddl := fmt.Sprintf("CREATE TABLE %s (\n\t%s\n)", r.qualified(table), strings.Join(defs, ",\n\t"))

	return withTx(ctx, r.local(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("создание таблицы %s: %w", table, mapError(err))
		}
		for _, c := range cols {
			if c.SourceName == "" || c.SourceName == c.Name {
				continue
			}
			stmt := fmt.Sprintf("COMMENT ON COLUMN %s.%s IS %s",
				r.qualified(table), quoteIdent(c.Name), quoteLiteral(c.SourceName))
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("комментарий к колонке %s: %w", c.Name, mapError(err))
			}
		}
		return nil
	})
}

func (r *RawDataRepository) DropTable(ctx context.Context, table string) error {
	_, err := r.local().Exec(ctx, "DROP TABLE IF EXISTS "+r.qualified(table))
	if err != nil {
		return fmt.Errorf("удаление таблицы %s: %w", table, mapError(err))
	}
	return nil
}

func (r *RawDataRepository) CopyRows(ctx context.Context, table string, cols []domain.Column, rows [][]any) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	n, err := r.local().CopyFrom(ctx,
		pgx.Identifier{r.schema, table},
		names,
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return n, fmt.Errorf("COPY в %s: %w", table, mapError(err))
	}
	return n, nil
}

func (r *RawDataRepository) Analyze(ctx context.Context, table string) error {
	if _, err := r.local().Exec(ctx, "ANALYZE "+r.qualified(table)); err != nil {
		return fmt.Errorf("ANALYZE %s: %w", table, mapError(err))
	}
	return nil
}

func (r *RawDataRepository) RowCount(ctx context.Context, ref domain.TableRef) (int64, error) {
	pool, rel, err := r.target(ctx, ref)
	if err != nil {
		return 0, err
	}
	var n int64
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+rel).Scan(&n); err != nil {
		return 0, fmt.Errorf("подсчёт строк %s: %w", ref.Table, mapError(err))
	}
	return n, nil
}

func (r *RawDataRepository) TableSizeBytes(ctx context.Context, ref domain.TableRef) (int64, error) {
	pool, rel, err := r.target(ctx, ref)
	if err != nil {
		return 0, err
	}
	var n int64
	if err := pool.QueryRow(ctx, "SELECT pg_total_relation_size($1::regclass)", rel).Scan(&n); err != nil {
		return 0, fmt.Errorf("размер таблицы %s: %w", ref.Table, mapError(err))
	}
	return n, nil
}

func (r *RawDataRepository) ProfileColumn(ctx context.Context, ref domain.TableRef, col domain.Column, topN int) (*domain.ColumnProfile, error) {
	pool, rel, err := r.target(ctx, ref)
	if err != nil {
		return nil, err
	}
	ident := quoteIdent(col.Name)

	aggregates := "NULL::double precision, NULL::double precision"
	if col.Type.IsNumeric() {
		aggregates = fmt.Sprintf("avg(%[1]s)::double precision, stddev_samp(%[1]s)::double precision", ident)
	}

	q := fmt.Sprintf(`
		SELECT count(*),
		       count(*) FILTER (WHERE %[1]s IS NULL),
		       count(DISTINCT %[1]s),
		       min(%[1]s)::text,
		       max(%[1]s)::text,
		       %[3]s
		  FROM %[2]s`, ident, rel, aggregates)

	p := &domain.ColumnProfile{ProfiledAt: time.Now().UTC()}
	err = pool.QueryRow(ctx, q).Scan(
		&p.RowCount, &p.NullCount, &p.DistinctCount, &p.MinValue, &p.MaxValue, &p.Mean, &p.StdDev)
	if err != nil {
		return nil, fmt.Errorf("профилирование %s.%s: %w", ref.Table, col.Name, mapError(err))
	}

	if topN > 0 && p.DistinctCount > 0 {
		top, err := r.topValues(ctx, pool, rel, ident, topN, p.RowCount-p.NullCount)
		if err != nil {
			return nil, err
		}
		p.TopValues = top
	}
	return p, nil
}

func (r *RawDataRepository) topValues(ctx context.Context, pool *pgxpool.Pool, rel, ident string, topN int, notNull int64) ([]domain.ValueFrequency, error) {
	q := fmt.Sprintf(`
		SELECT %[1]s::text AS value, count(*) AS cnt
		  FROM %[2]s
		 WHERE %[1]s IS NOT NULL
		 GROUP BY 1
		 ORDER BY cnt DESC, 1
		 LIMIT %[3]d`, ident, rel, topN)

	rows, err := pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("топ значений: %w", mapError(err))
	}
	defer rows.Close()

	var out []domain.ValueFrequency
	for rows.Next() {
		var vf domain.ValueFrequency
		if err := rows.Scan(&vf.Value, &vf.Count); err != nil {
			return nil, mapError(err)
		}
		if notNull > 0 {
			vf.Share = float64(vf.Count) / float64(notNull)
		}
		out = append(out, vf)
	}
	return out, mapError(rows.Err())
}

func (r *RawDataRepository) TimeSpan(ctx context.Context, ref domain.TableRef, col domain.Column) (*domain.TimeSpan, error) {
	if !col.Type.IsTemporal() {
		return nil, domain.NewValidationError("column", "колонка не является временной")
	}
	pool, rel, err := r.target(ctx, ref)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf("SELECT min(%[1]s)::timestamptz, max(%[1]s)::timestamptz FROM %[2]s",
		quoteIdent(col.Name), rel)

	span := &domain.TimeSpan{Column: col.Name}
	if err := pool.QueryRow(ctx, q).Scan(&span.From, &span.To); err != nil {
		return nil, fmt.Errorf("временной охват %s.%s: %w", ref.Table, col.Name, mapError(err))
	}
	if span.From != nil && span.To != nil {
		span.Days = int(span.To.Sub(*span.From).Hours() / 24)
	}
	return span, nil
}

func (r *RawDataRepository) DuplicateRows(ctx context.Context, ref domain.TableRef, cols []domain.Column) (int64, error) {
	if len(cols) == 0 {
		return 0, nil
	}
	pool, rel, err := r.target(ctx, ref)
	if err != nil {
		return 0, err
	}
	idents := make([]string, len(cols))
	for i, c := range cols {
		idents[i] = quoteIdent(c.Name)
	}
	q := fmt.Sprintf(`
		SELECT coalesce(sum(cnt - 1), 0)::bigint
		  FROM (SELECT count(*) AS cnt FROM %s GROUP BY %s HAVING count(*) > 1) d`,
		rel, strings.Join(idents, ", "))

	var n int64
	if err := pool.QueryRow(ctx, q).Scan(&n); err != nil {
		return 0, fmt.Errorf("поиск дубликатов %s: %w", ref.Table, mapError(err))
	}
	return n, nil
}

func (r *RawDataRepository) Sample(ctx context.Context, ref domain.TableRef, limit int) (*domain.ResultSet, error) {
	if limit <= 0 {
		limit = 20
	}
	_, rel, err := r.target(ctx, ref)
	if err != nil {
		return nil, err
	}
	schema := ref.Schema
	if schema == "" {
		schema = r.schema
	}
	q := fmt.Sprintf("SELECT * FROM %s LIMIT %d", rel, limit)
	return r.Query(ctx, domain.QueryTarget{SourceID: ref.SourceID, Schemas: []string{schema}}, q, limit)
}

func (r *RawDataRepository) ValueOverlap(ctx context.Context, left, right domain.TableRef) (float64, int64, error) {
	if !left.SameSource(right) {
		return 0, 0, nil
	}
	pool, leftRel, err := r.target(ctx, left)
	if err != nil {
		return 0, 0, err
	}
	_, rightRel, err := r.target(ctx, right)
	if err != nil {
		return 0, 0, err
	}
	q := fmt.Sprintf(`
		WITH l AS (SELECT DISTINCT %[1]s::text AS v FROM %[2]s WHERE %[1]s IS NOT NULL LIMIT %[5]d),
		     r AS (SELECT DISTINCT %[3]s::text AS v FROM %[4]s WHERE %[3]s IS NOT NULL LIMIT %[5]d)
		SELECT (SELECT count(*) FROM l),
		       (SELECT count(*) FROM l JOIN r USING (v))`,
		quoteIdent(left.Column), leftRel,
		quoteIdent(right.Column), rightRel,
		maxDistinctScan)

	var total, matched int64
	if err := pool.QueryRow(ctx, q).Scan(&total, &matched); err != nil {
		return 0, 0, fmt.Errorf("пересечение значений: %w", mapError(err))
	}
	if total == 0 {
		return 0, 0, nil
	}
	return float64(matched) / float64(total), matched, nil
}

func (r *RawDataRepository) Query(ctx context.Context, target domain.QueryTarget, sql string, maxRows int) (*domain.ResultSet, error) {
	if maxRows <= 0 {
		maxRows = 1000
	}
	started := time.Now()

	pool, err := r.registry.For(ctx, target.SourceID)
	if err != nil {
		return nil, err
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		AccessMode: pgx.ReadOnly,
		IsoLevel:   pgx.ReadCommitted,
	})
	if err != nil {
		return nil, fmt.Errorf("read-only транзакция: %w", mapError(err))
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if r.queryTimeout > 0 {
		stmt := fmt.Sprintf("SET LOCAL statement_timeout = %d", r.queryTimeout.Milliseconds())
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return nil, fmt.Errorf("ограничение времени запроса: %w", mapError(err))
		}
	}

	if path := searchPath(target.Schemas, r.schema); path != "" {
		if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+path); err != nil {
			return nil, fmt.Errorf("установка search_path: %w", mapError(err))
		}
	}

	rows, err := tx.Query(ctx, sql)
	if err != nil {
		return nil, queryError(err)
	}
	defer rows.Close()

	rs := &domain.ResultSet{Columns: resultColumns(rows.FieldDescriptions()), SQL: sql}
	for rows.Next() {
		if len(rs.Rows) >= maxRows {
			rs.Truncated = true
			break
		}
		vals, err := rows.Values()
		if err != nil {
			return nil, queryError(err)
		}
		row := make([]any, len(vals))
		for i, v := range vals {
			row[i] = normalizeValue(v)
		}
		rs.Rows = append(rs.Rows, row)
	}
	if err := rows.Err(); err != nil && !rs.Truncated {
		return nil, queryError(err)
	}

	rs.RowCount = len(rs.Rows)
	rs.Duration = time.Since(started)
	return rs, nil
}

func (r *RawDataRepository) ExplainCost(ctx context.Context, target domain.QueryTarget, sql string) (float64, error) {
	pool, err := r.registry.For(ctx, target.SourceID)
	if err != nil {
		return 0, err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return 0, mapError(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var raw []byte
	if err := tx.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+sql).Scan(&raw); err != nil {
		return 0, queryError(err)
	}

	var plans []struct {
		Plan struct {
			TotalCost float64 `json:"Total Cost"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) == 0 {
		return 0, nil
	}
	return plans[0].Plan.TotalCost, nil
}

func searchPath(schemas []string, fallback string) string {
	if len(schemas) == 0 {
		if fallback == "" {
			return ""
		}
		schemas = []string{fallback}
	}
	quoted := make([]string, 0, len(schemas))
	seen := make(map[string]struct{}, len(schemas))
	for _, s := range schemas {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		quoted = append(quoted, quoteIdent(s))
	}
	return strings.Join(quoted, ", ")
}

func queryError(err error) error {
	mapped := mapError(err)
	if errors.Is(mapped, domain.ErrQueryTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return domain.ErrQueryTimeout
	}
	return mapped
}
