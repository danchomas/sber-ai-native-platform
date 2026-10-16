package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Migration struct {
	Version string
	Name    string
	SQL     string
}

func Migrate(ctx context.Context, pool *pgxpool.Pool, rawSchema string) ([]string, error) {
	migrations, err := loadMigrations()
	if err != nil {
		return nil, err
	}

	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text PRIMARY KEY,
			name       text NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return nil, fmt.Errorf("таблица schema_migrations: %w", err)
	}

	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("чтение применённых миграций: %w", err)
	}
	applied := map[string]struct{}{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return nil, err
		}
		applied[v] = struct{}{}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var done []string
	for _, m := range migrations {
		if _, ok := applied[m.Version]; ok {
			continue
		}
		stmt := strings.ReplaceAll(m.SQL, "{{RAW_SCHEMA}}", quoteIdent(rawSchema))
		err := withTx(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return err
			}
			_, err := tx.Exec(ctx,
				`INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, m.Version, m.Name)
			return err
		})
		if err != nil {
			return done, fmt.Errorf("миграция %s_%s: %w", m.Version, m.Name, err)
		}
		done = append(done, m.Version+"_"+m.Name)
	}
	return done, nil
}

func loadMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("чтение встроенных миграций: %w", err)
	}

	out := make([]Migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		base := strings.TrimSuffix(e.Name(), ".sql")
		version, name, _ := strings.Cut(base, "_")
		out = append(out, Migration{Version: version, Name: name, SQL: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}
