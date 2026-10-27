//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/heinousness/aibi/internal/core/analytics"
	corepg "github.com/heinousness/aibi/internal/core/repository/postgres"
	pgpool "github.com/heinousness/aibi/internal/core/repository/postgres/pool/pgx"
	"github.com/heinousness/aibi/internal/core/sqlguard"
)

func TestCompiledQueriesRunOnPostgres(t *testing.T) {
	pool := setup(t)
	e := newEnv(t, pool)
	ctx := context.Background()

	upload(t, e, "orders", ordersCSV)
	upload(t, e, "clients", clientsCSV)
	if err := e.datasets.DiscoverRelations(ctx); err != nil {
		t.Fatalf("поиск связей: %v", err)
	}

	datasets, err := e.datasets.List(ctx)
	if err != nil {
		t.Fatalf("каталог: %v", err)
	}
	relations, err := e.datasets.Relations(ctx)
	if err != nil {
		t.Fatalf("связи: %v", err)
	}
	scopes, err := e.datasets.Scopes(ctx)
	if err != nil {
		t.Fatalf("области: %v", err)
	}
	cat := analytics.Catalog{Datasets: datasets, Relations: relations, DefaultSchema: testSchema}

	cases := []struct {
		name     string
		query    analytics.Query
		wantRows int
		check    func(t *testing.T, rows [][]any)
	}{
		{
			name: "сумма по месяцам с фильтром по дате",
			query: analytics.Query{
				Dataset: "orders",
				Time:    &analytics.TimeGroup{Column: "data_zakaza", Grain: analytics.GrainMonth},
				Metrics: []analytics.Metric{{Func: analytics.FuncSum, Column: "summa_rub"}},
				Filters: []analytics.Filter{{Column: "data_zakaza", Op: analytics.OpGte, Value: "2024-03-01"}},
			},
			wantRows: 3,
		},
		{
			name: "доля и медиана по статусу",
			query: analytics.Query{
				Dataset: "orders", GroupBy: []string{"Статус"},
				Metrics: []analytics.Metric{{Func: analytics.FuncShare}, {Func: analytics.FuncMedian, Column: "summa_rub"}},
			},
			wantRows: 3,
		},
		{
			name: "список значений и подстрока",
			query: analytics.Query{
				Dataset: "orders", Metrics: []analytics.Metric{{Func: analytics.FuncCount}},
				Filters: []analytics.Filter{
					{Column: "kod_klienta", Op: analytics.OpIn, Values: []any{"0042", "0043"}},
					{Column: "kanal", Op: analytics.OpContains, Value: "сай"},
				},
			},
			wantRows: 1,
			check: func(t *testing.T, rows [][]any) {
				if rows[0][0] != int64(4) {
					t.Fatalf("заказов с сайта у двух клиентов: %v, ожидалось 4", rows[0][0])
				}
			},
		},
		{
			name: "соединение по выведенной связи",
			query: analytics.Query{
				Dataset: "orders", Join: []string{"clients"},
				GroupBy: []string{"segment"},
				Metrics: []analytics.Metric{{Func: analytics.FuncCountDistinct, Column: "orders.kod_klienta"}},
			},
			wantRows: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := analytics.Compile(tc.query, cat, queryCfg.MaxRows)
			if err != nil {
				t.Fatalf("компиляция: %v", err)
			}
			scope, checked, err := sqlguard.ResolveScope(compiled.SQL, scopes, testSchema, queryCfg.MaxRows)
			if err != nil {
				t.Fatalf("sqlguard отклонил собранный запрос: %v\n%s", err, compiled.SQL)
			}
			result, err := e.raw.Query(ctx, scope.Target, checked.SQL, queryCfg.MaxRows, compiled.Args...)
			if err != nil {
				t.Fatalf("выполнение: %v\n%s", err, compiled.SQL)
			}
			if result.RowCount != tc.wantRows {
				t.Fatalf("строк %d, ожидалось %d: %v", result.RowCount, tc.wantRows, result.Rows)
			}
			if tc.check != nil {
				tc.check(t, result.Rows)
			}
		})
	}
}

func TestReaderRoleIsolation(t *testing.T) {
	pool := setup(t)
	e := newEnv(t, pool)
	ctx := context.Background()

	report := upload(t, e, "orders", ordersCSV)

	const role = "aibi_reader_itest"
	password, err := corepg.EnsureReaderRole(ctx, pool, role, testSchema, queryCfg.Timeout)
	if err != nil {
		t.Fatalf("роль чтения: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DROP OWNED BY `+role)
		_, _ = pool.Exec(ctx, `DROP ROLE IF EXISTS `+role)
	})
	if _, err := corepg.EnsureReaderRole(ctx, pool, role, testSchema, queryCfg.Timeout); err != nil {
		t.Fatalf("повторная настройка роли при перезапуске: %v", err)
	}

	reader, err := pgpool.NewPool(ctx, pgpool.PoolOptions{
		DSN: os.Getenv("TEST_DATABASE_URL"), User: role, Password: password, MaxConns: 2,
	})
	if err == nil {
		t.Fatal("старый пароль роли должен перестать работать после перезапуска")
	}

	password, err = corepg.EnsureReaderRole(ctx, pool, role, testSchema, queryCfg.Timeout)
	if err != nil {
		t.Fatalf("роль чтения: %v", err)
	}
	reader, err = pgpool.NewPool(ctx, pgpool.PoolOptions{
		DSN: os.Getenv("TEST_DATABASE_URL"), User: role, Password: password, MaxConns: 2,
	})
	if err != nil {
		t.Fatalf("подключение ролью чтения: %v", err)
	}
	defer reader.Close()
	e.raw.WithReader(reader)

	scopes, err := e.datasets.Scopes(ctx)
	if err != nil {
		t.Fatalf("области: %v", err)
	}
	target := scopes[0].Target
	table := testSchema + "." + report.Dataset.TableName

	if _, err := e.raw.Query(ctx, target, "SELECT count(*) FROM "+table, 1); err != nil {
		t.Fatalf("чтение данных ролью чтения: %v", err)
	}

	later := upload(t, e, "clients", clientsCSV)
	if _, err := e.raw.Query(ctx, target, "SELECT count(*) FROM "+testSchema+"."+later.Dataset.TableName, 1); err != nil {
		t.Fatalf("таблица, загруженная после настройки роли, должна читаться: %v", err)
	}

	denied := map[string]string{
		"метаданные платформы":  "SELECT count(*) FROM public.datasets",
		"ключи провайдеров":     "SELECT api_key FROM public.llm_providers",
		"чтение файлов сервера": "SELECT pg_read_file('/etc/passwd')",
		"изменение данных":      "WITH d AS (DELETE FROM " + table + " RETURNING 1) SELECT count(*) FROM d",
	}
	for name, sql := range denied {
		t.Run(name, func(t *testing.T) {
			_, err := e.raw.Query(ctx, target, sql, 1)
			if err == nil {
				t.Fatalf("запрос выполнен ролью чтения: %s", sql)
			}
			if !strings.Contains(err.Error(), "permission denied") && !strings.Contains(err.Error(), "read-only") {
				t.Fatalf("ожидался отказ в правах: %v", err)
			}
		})
	}
}
