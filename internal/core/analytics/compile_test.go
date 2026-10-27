package analytics

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/sqlguard"
)

func testCatalog() Catalog {
	orders := domain.Dataset{
		ID: uuid.New(), Name: "orders", TableName: "ds_orders", Schema: "raw",
		Status: domain.DatasetStatusReady,
		Columns: []domain.Column{
			{Name: "order_id", SourceName: "ID заказа", Type: domain.ColumnTypeInteger},
			{Name: "customer_id", SourceName: "Клиент", Type: domain.ColumnTypeInteger},
			{Name: "created_at", SourceName: "Дата заказа", Type: domain.ColumnTypeTimestamp},
			{Name: "status", SourceName: "Статус", Type: domain.ColumnTypeText},
			{Name: "amount", SourceName: "Сумма", Type: domain.ColumnTypeNumeric},
			{Name: "is_paid", SourceName: "Оплачен", Type: domain.ColumnTypeBoolean},
		},
	}
	customers := domain.Dataset{
		ID: uuid.New(), Name: "customers", TableName: "ds_customers", Schema: "raw",
		Status: domain.DatasetStatusReady,
		Columns: []domain.Column{
			{Name: "id", SourceName: "ID", Type: domain.ColumnTypeInteger},
			{Name: "region", SourceName: "Регион", Type: domain.ColumnTypeText},
			{Name: "status", SourceName: "Статус клиента", Type: domain.ColumnTypeText},
		},
	}
	failed := domain.Dataset{
		ID: uuid.New(), Name: "broken", TableName: "ds_broken", Schema: "raw",
		Status: domain.DatasetStatusFailed,
	}
	return Catalog{
		Datasets: []domain.Dataset{orders, customers, failed},
		Relations: []domain.Relation{{
			LeftDatasetID: orders.ID, LeftDataset: "orders", LeftColumn: "customer_id",
			RightDatasetID: customers.ID, RightDataset: "customers", RightColumn: "id",
		}},
		DefaultSchema: "raw",
	}
}

// mustPassGuard проверяет инвариант: всё, что собрал компилятор, проходит sqlguard.
func mustPassGuard(t *testing.T, c *Compiled, cat Catalog) {
	t.Helper()
	tables := make([]string, 0, len(cat.Datasets))
	for _, d := range cat.Datasets {
		tables = append(tables, d.TableName)
	}
	if _, err := sqlguard.Validate(c.SQL, sqlguard.NewPolicy("raw", tables, 1000)); err != nil {
		t.Fatalf("собранный запрос не прошёл sqlguard: %v\n%s", err, c.SQL)
	}
}

func TestCompile(t *testing.T) {
	cases := []struct {
		name     string
		query    Query
		wantSQL  []string
		wantArgs []any
	}{
		{
			name:  "количество по разрезу",
			query: Query{Dataset: "orders", GroupBy: []string{"status"}},
			wantSQL: []string{
				`SELECT t0."status" AS "Статус", count(*) AS "Количество" FROM "raw"."ds_orders" AS t0`,
				"GROUP BY 1 ORDER BY 2 DESC NULLS LAST LIMIT 100",
			},
		},
		{
			name: "динамика по месяцам с фильтром",
			query: Query{
				Dataset: "orders",
				Time:    &TimeGroup{Column: "created_at", Grain: GrainMonth},
				Metrics: []Metric{{Func: FuncSum, Column: "amount"}},
				Filters: []Filter{{Column: "created_at", Op: OpGte, Value: "2024-01-01"}},
			},
			wantSQL: []string{
				`date_trunc('month', t0."created_at")::date AS "Месяц"`,
				`sum(t0."amount") AS "Сумма «Сумма»"`,
				`WHERE t0."created_at" >= $1`,
				"GROUP BY 1 ORDER BY 1 LIMIT 100",
			},
			wantArgs: []any{time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
		{
			name: "сезонность по месяцу года",
			query: Query{
				Dataset: "orders",
				Time:    &TimeGroup{Column: "Дата заказа", Grain: GrainMonthOfYear},
				Metrics: []Metric{{Func: FuncAvg, Column: "amount", Label: "Средний чек"}},
			},
			wantSQL: []string{
				`date_part('month', t0."created_at")::int AS "Месяц года"`,
				`round(avg(t0."amount")::numeric, 2) AS "Средний чек"`,
			},
		},
		{
			name:    "одно число без разреза",
			query:   Query{Dataset: "orders", Metrics: []Metric{{Func: FuncCountDistinct, Column: "customer_id"}}},
			wantSQL: []string{`SELECT count(DISTINCT t0."customer_id") AS "Уникальных «Клиент»" FROM "raw"."ds_orders" AS t0`},
		},
		{
			name: "доля и медиана",
			query: Query{
				Dataset: "orders", GroupBy: []string{"status"},
				Metrics: []Metric{{Func: FuncShare}, {Func: FuncMedian, Column: "amount"}},
				Sort:    &Sort{By: "Доля, %", Desc: true}, Limit: 5,
			},
			wantSQL: []string{
				"round(100.0 * count(*) / NULLIF(sum(count(*)) OVER (), 0), 2)",
				`percentile_cont(0.5) WITHIN GROUP (ORDER BY t0."amount")`,
				"ORDER BY 2 DESC NULLS LAST LIMIT 5",
			},
		},
		{
			name: "список значений типизирован",
			query: Query{
				Dataset: "orders", GroupBy: []string{"status"},
				Filters: []Filter{{Column: "order_id", Op: OpIn, Values: []any{1.0, "2"}}},
			},
			wantSQL:  []string{`t0."order_id" = ANY($1)`},
			wantArgs: []any{[]int64{1, 2}},
		},
		{
			name: "подстрока экранирована",
			query: Query{
				Dataset: "orders", GroupBy: []string{"status"},
				Filters: []Filter{{Column: "status", Op: OpContains, Value: "100%_"}},
			},
			wantSQL:  []string{`t0."status" ILIKE $1 ESCAPE '!'`},
			wantArgs: []any{"%100!%!_%"},
		},
		{
			name: "соединение по выведенной связи",
			query: Query{
				Dataset: "orders", Join: []string{"customers"},
				GroupBy: []string{"region"},
				Metrics: []Metric{{Func: FuncSum, Column: "amount"}},
			},
			wantSQL: []string{
				`t1."region" AS "Регион"`,
				`LEFT JOIN "raw"."ds_customers" AS t1 ON t0."customer_id" = t1."id"`,
			},
		},
		{
			name: "одноимённая колонка берётся из основного датасета",
			query: Query{
				Dataset: "orders", Join: []string{"customers"},
				GroupBy: []string{"status", "customers.status"},
			},
			wantSQL: []string{`t0."status" AS "Статус", t1."status" AS "Статус клиента"`},
		},
		{
			name: "подпись с кавычкой экранируется",
			query: Query{
				Dataset: "orders",
				Metrics: []Metric{{Func: FuncCount, Label: `x" FROM pg_shadow --`}},
			},
			wantSQL: []string{`count(*) AS "x"" FROM pg_shadow --"`},
		},
	}

	cat := testCatalog()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Compile(tc.query, cat, 1000)
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			for _, part := range tc.wantSQL {
				if !strings.Contains(got.SQL, part) {
					t.Errorf("в запросе нет %q:\n%s", part, got.SQL)
				}
			}
			if tc.wantArgs != nil && !equalArgs(got.Args, tc.wantArgs) {
				t.Errorf("параметры %#v, ожидались %#v", got.Args, tc.wantArgs)
			}
			mustPassGuard(t, got, cat)
		})
	}
}

func TestCompileRejects(t *testing.T) {
	cases := []struct {
		name   string
		query  Query
		wantIn string
	}{
		{"неизвестный датасет", Query{Dataset: "pg_shadow", GroupBy: []string{"status"}}, "нет в каталоге"},
		{"датасет не готов", Query{Dataset: "broken", Metrics: []Metric{{Func: FuncCount}}}, "нет в каталоге"},
		{"внедрение в имя колонки", Query{Dataset: "orders", GroupBy: []string{`status"; DROP TABLE x; --`}}, "нет в датасете"},
		{"неизвестная функция", Query{Dataset: "orders", Metrics: []Metric{{Func: "pg_read_file", Column: "status"}}}, "неизвестная функция"},
		{"сумма по тексту", Query{Dataset: "orders", Metrics: []Metric{{Func: FuncSum, Column: "status"}}}, "нельзя применить"},
		{"время по нечисловой колонке", Query{Dataset: "orders", Time: &TimeGroup{Column: "status", Grain: GrainMonth}}, "не содержит дат"},
		{"неизвестная гранулярность", Query{Dataset: "orders", Time: &TimeGroup{Column: "created_at", Grain: "century"}}, "гранулярность"},
		{"неизвестный оператор", Query{Dataset: "orders", GroupBy: []string{"status"}, Filters: []Filter{{Column: "status", Op: "; DELETE", Value: "x"}}}, "неизвестный оператор"},
		{"значение не того типа", Query{Dataset: "orders", GroupBy: []string{"status"}, Filters: []Filter{{Column: "amount", Op: OpGt, Value: "много"}}}, "не подходит"},
		{"пустой запрос", Query{Dataset: "orders"}, "хотя бы одну метрику"},
		{"доля без разреза", Query{Dataset: "orders", Metrics: []Metric{{Func: FuncShare}}}, "только в разрезе"},
		{"соединение без связи", Query{Dataset: "customers", Join: []string{"broken"}, GroupBy: []string{"region"}}, "нет в каталоге"},
		{"сортировка выражением", Query{Dataset: "orders", GroupBy: []string{"status"}, Sort: &Sort{By: "random()"}}, "только по колонкам результата"},
		{"слишком много разрезов", Query{Dataset: "orders", GroupBy: []string{"a", "b", "c", "d"}}, "разрезов не больше"},
	}

	cat := testCatalog()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.query, cat, 1000)
			if err == nil {
				t.Fatal("ожидался отказ")
			}
			if !errors.Is(err, apperr.ErrValidation) {
				t.Fatalf("отказ должен быть ошибкой валидации: %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("ошибка %q не содержит %q", err, tc.wantIn)
			}
		})
	}
}

func TestCompileKeepsValuesOutOfSQL(t *testing.T) {
	payload := "'; DROP TABLE raw.ds_orders; --"
	got, err := Compile(Query{
		Dataset: "orders", GroupBy: []string{"status"},
		Filters: []Filter{{Column: "status", Op: OpEq, Value: payload}},
	}, testCatalog(), 1000)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if strings.Contains(got.SQL, "DROP") {
		t.Fatalf("значение фильтра попало в текст запроса:\n%s", got.SQL)
	}
	if len(got.Args) != 1 || got.Args[0] != payload {
		t.Fatalf("значение должно уйти параметром: %#v", got.Args)
	}
}

func TestCompileClampsLimit(t *testing.T) {
	got, err := Compile(Query{Dataset: "orders", GroupBy: []string{"status"}, Limit: 1_000_000}, testCatalog(), 500)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if !strings.HasSuffix(got.SQL, "LIMIT 500") {
		t.Fatalf("предел строк не применён:\n%s", got.SQL)
	}
}

func TestCompileRows(t *testing.T) {
	cat := testCatalog()
	got, err := CompileRows(RowsQuery{
		Dataset: "orders", Columns: []string{"order_id", "Сумма"},
		Filters: []Filter{{Column: "is_paid", Op: OpEq, Value: "да"}},
		Sort:    &Sort{By: "amount", Desc: true}, Limit: 999,
	}, cat)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	want := `SELECT t0."order_id" AS "ID заказа", t0."amount" AS "Сумма" FROM "raw"."ds_orders" AS t0 ` +
		`WHERE t0."is_paid" = $1 ORDER BY t0."amount" DESC NULLS LAST LIMIT 50`
	if got.SQL != want {
		t.Fatalf("запрос:\n%s\nожидался:\n%s", got.SQL, want)
	}
	if got.Args[0] != true {
		t.Fatalf("логическое значение не приведено: %#v", got.Args)
	}
	mustPassGuard(t, got, cat)
}

func TestCompileDuplicates(t *testing.T) {
	cat := testCatalog()
	summary, examples, err := CompileDuplicates("orders", []string{"customer_id", "amount"}, cat)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	for _, c := range []*Compiled{summary, examples} {
		if !strings.Contains(c.SQL, `GROUP BY t0."customer_id", t0."amount" HAVING count(*) > 1`) {
			t.Errorf("нет группировки по ключу:\n%s", c.SQL)
		}
		mustPassGuard(t, c, cat)
	}
}

func equalArgs(got, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		switch w := want[i].(type) {
		case []int64:
			g, ok := got[i].([]int64)
			if !ok || len(g) != len(w) {
				return false
			}
			for j := range g {
				if g[j] != w[j] {
					return false
				}
			}
		case time.Time:
			g, ok := got[i].(time.Time)
			if !ok || !g.Equal(w) {
				return false
			}
		default:
			if got[i] != want[i] {
				return false
			}
		}
	}
	return true
}
