package sqlguard

import (
	"errors"
	"strings"
	"testing"

	"github.com/heinousness/aibi/internal/core/apperr"
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
			if !errors.Is(err, apperr.ErrUnsafeQuery) {
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
	// На строку больше предела: по ней исполнитель узнаёт об обрезании.
	if !strings.Contains(res.SQL, "LIMIT 1001") {
		t.Fatalf("LIMIT не добавлен: %s", res.SQL)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("обёртка не должна предупреждать сама: %v", res.Warnings)
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

func TestValidateAllowsFromInsideFunctions(t *testing.T) {
	cases := map[string]string{
		"extract":           `SELECT EXTRACT(MONTH FROM created_at) AS m, count(*) FROM raw.ds_orders GROUP BY 1`,
		"extract в cte":     `WITH m AS (SELECT EXTRACT(YEAR FROM created_at) AS y FROM raw.ds_orders) SELECT y FROM m`,
		"substring":         `SELECT SUBSTRING(status FROM 1 FOR 3) FROM raw.ds_orders`,
		"trim":              `SELECT TRIM(BOTH ' ' FROM status) FROM raw.ds_orders`,
		"is distinct from":  `SELECT count(*) FROM raw.ds_orders WHERE status IS DISTINCT FROM 'paid'`,
		"is not distinct":   `SELECT count(*) FROM raw.ds_orders WHERE status IS NOT DISTINCT FROM 'paid'`,
		"вложенный extract": `SELECT date_part('year', created_at), EXTRACT(DOW FROM date_trunc('day', created_at)) FROM raw.ds_orders`,
	}
	for name, sql := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := Validate(sql, testPolicy())
			if err != nil {
				t.Fatalf("колонка внутри функции принята за таблицу: %v", err)
			}
			if len(res.Tables) != 1 || res.Tables[0] != "raw.ds_orders" {
				t.Fatalf("таблицы: %v", res.Tables)
			}
		})
	}
}

func TestValidateChecksSubqueryInsideExtract(t *testing.T) {
	sql := `SELECT EXTRACT(MONTH FROM (SELECT max(created_at) FROM pg_catalog.pg_class)) FROM raw.ds_orders`
	if _, err := Validate(sql, testPolicy()); !errors.Is(err, apperr.ErrUnsafeQuery) {
		t.Fatalf("подзапрос внутри EXTRACT должен проверяться, получено: %v", err)
	}
}

func TestValidateCastsRoundArguments(t *testing.T) {
	cases := map[string]string{
		`SELECT round(avg(amount), 2) FROM raw.ds_orders`:                    `round((avg(amount))::numeric, 2)`,
		`SELECT ROUND(sum(a) * 100.0 / count(*), 1) AS p FROM raw.ds_orders`: `ROUND((sum(a) * 100.0 / count(*))::numeric, 1)`,
		`SELECT round(round(x, 3), 1) FROM raw.ds_orders`:                    `round((round((x)::numeric, 3))::numeric, 1)`,
	}
	for sql, want := range cases {
		res, err := Validate(sql, testPolicy())
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		if !strings.Contains(res.SQL, want) {
			t.Fatalf("round не приведён к numeric:\n%s\nожидалось: %s", res.SQL, want)
		}
	}

	res, err := Validate(`SELECT round(avg(amount)) FROM raw.ds_orders`, testPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.SQL, "numeric") {
		t.Fatalf("round с одним аргументом трогать не нужно: %s", res.SQL)
	}
}
