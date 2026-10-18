package sqlguard

import (
	"errors"
	"strings"
	"testing"

	"github.com/heinousness/aibi/internal/core/domain"
)

func testPolicy() Policy {
	return NewPolicy("raw", []string{"ds_orders", "ds_users"}, 1000)
}

func TestValidateAllowsReadOnlyQueries(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{"простой select", `SELECT * FROM raw.ds_orders LIMIT 10`},
		{"без схемы", `SELECT count(*) FROM ds_orders`},
		{"агрегация с группировкой", `SELECT status, count(*) FROM raw.ds_orders GROUP BY status ORDER BY 2 DESC`},
		{"cte", `WITH m AS (SELECT date_trunc('month', created_at) AS mon FROM raw.ds_orders) SELECT mon, count(*) FROM m GROUP BY mon`},
		{"join двух датасетов", `SELECT u.id FROM raw.ds_users u JOIN raw.ds_orders o ON o.user_id = u.id`},
		{"подзапрос", `SELECT * FROM (SELECT 1 AS x) s`},
		{"табличная функция", `SELECT * FROM generate_series(1, 10) AS g`},
		{"колонка с опасным именем в кавычках", `SELECT "update", "set" FROM raw.ds_orders`},
		{"комментарий в запросе", "SELECT 1 -- комментарий\nFROM raw.ds_orders"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Validate(tc.sql, testPolicy())
			if err != nil {
				t.Fatalf("ожидался разрешённый запрос, получено: %v", err)
			}
			if res.SQL == "" {
				t.Fatal("пустой SQL в результате")
			}
		})
	}
}

func TestValidateRejectsUnsafeQueries(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{"удаление", `DELETE FROM raw.ds_orders`},
		{"обновление", `UPDATE raw.ds_orders SET status = 'x'`},
		{"drop после select", `SELECT 1; DROP TABLE raw.ds_orders`},
		{"drop в комментарии не спасает", "SELECT 1 FROM raw.ds_orders; DROP TABLE raw.ds_users"},
		{"select into", `SELECT * INTO new_table FROM raw.ds_orders`},
		{"системный каталог", `SELECT * FROM pg_catalog.pg_tables`},
		{"information_schema", `SELECT * FROM information_schema.columns`},
		{"pg_ таблица", `SELECT * FROM pg_stat_activity`},
		{"чтение файла", `SELECT pg_read_file('/etc/passwd')`},
		{"задержка", `SELECT pg_sleep(100) FROM raw.ds_orders`},
		{"неизвестная таблица", `SELECT * FROM secrets`},
		{"чужая схема", `SELECT * FROM public.users`},
		{"изменение параметров", `SET statement_timeout = 0`},
		{"copy", `COPY raw.ds_orders TO '/tmp/x.csv'`},
		{"создание таблицы", `CREATE TABLE evil AS SELECT 1`},
		{"explain analyze", `EXPLAIN ANALYZE SELECT * FROM raw.ds_orders`},
		{"пустой", `   `},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Validate(tc.sql, testPolicy())
			if err == nil {
				t.Fatal("ожидалось отклонение запроса")
			}
			if !errors.Is(err, domain.ErrUnsafeQuery) {
				t.Fatalf("ожидалась ErrUnsafeQuery, получено: %v", err)
			}
		})
	}
}

func TestValidateAddsLimit(t *testing.T) {
	res, err := Validate(`SELECT * FROM raw.ds_orders`, testPolicy())
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if !res.Wrapped {
		t.Fatal("ожидалась обёртка с LIMIT")
	}
	if !strings.Contains(res.SQL, "LIMIT 1000") {
		t.Fatalf("LIMIT не добавлен: %s", res.SQL)
	}
}

func TestValidateKeepsExistingLimit(t *testing.T) {
	res, err := Validate(`SELECT * FROM raw.ds_orders LIMIT 5`, testPolicy())
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if res.Wrapped {
		t.Fatal("собственный LIMIT запроса должен сохраняться без обёртки")
	}
}

func TestValidateCollectsTables(t *testing.T) {
	res, err := Validate(
		`SELECT * FROM raw.ds_users u JOIN raw.ds_orders o ON o.user_id = u.id LIMIT 10`,
		testPolicy())
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(res.Tables) != 2 {
		t.Fatalf("ожидались две таблицы, получено: %v", res.Tables)
	}
	want := map[string]bool{"raw.ds_users": true, "raw.ds_orders": true}
	for _, got := range res.Tables {
		if !want[got] {
			t.Errorf("неожиданная таблица %q, ожидались полные имена вида raw.ds_users", got)
		}
	}
}

func TestSourcePolicyIsolatesSchemas(t *testing.T) {
	p := NewSourcePolicy("sales", []TableName{
		{Schema: "sales", Table: "orders"},
		{Schema: "crm", Table: "clients"},
	}, 1000)

	if _, err := Validate(`SELECT * FROM sales.orders LIMIT 5`, p); err != nil {
		t.Errorf("таблица своей схемы должна быть доступна: %v", err)
	}
	if _, err := Validate(`SELECT * FROM crm.clients LIMIT 5`, p); err != nil {
		t.Errorf("вторая схема источника должна быть доступна: %v", err)
	}
	if _, err := Validate(`SELECT * FROM orders LIMIT 5`, p); err != nil {
		t.Errorf("неполное имя должно разрешаться в схеме по умолчанию: %v", err)
	}
	if _, err := Validate(`SELECT * FROM raw.ds_users LIMIT 5`, p); err == nil {
		t.Error("схема чужого источника должна быть отклонена")
	}
	if _, err := Validate(`SELECT * FROM sales.secrets LIMIT 5`, p); err == nil {
		t.Error("таблица вне каталога должна быть отклонена")
	}
}

func TestValidateIgnoresKeywordsInsideLiterals(t *testing.T) {
	res, err := Validate(
		`SELECT * FROM raw.ds_orders WHERE status = 'drop table' LIMIT 1`, testPolicy())
	if err != nil {
		t.Fatalf("ключевое слово внутри литерала не должно отклонять запрос: %v", err)
	}
	if len(res.Tables) != 1 {
		t.Fatalf("ожидалась одна таблица, получено: %v", res.Tables)
	}
}

func TestValidateRejectsTooManyJoins(t *testing.T) {
	p := testPolicy()
	p.MaxJoins = 1
	_, err := Validate(
		`SELECT 1 FROM raw.ds_users a JOIN raw.ds_orders b ON true JOIN raw.ds_orders c ON true LIMIT 1`, p)
	if err == nil {
		t.Fatal("ожидалось отклонение по числу соединений")
	}
}
