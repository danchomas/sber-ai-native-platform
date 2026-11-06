package analytics

import (
	"errors"
	"strings"
	"testing"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
)

func TestCompileCalc(t *testing.T) {
	cases := []struct {
		name   string
		metric Metric
		want   string
	}{
		{"отношение сумм",
			Metric{Func: FuncCalc, Expr: `(sum("Сумма") - sum(order_id)) / sum(order_id) * 100`, Label: "ROMI, %"},
			`round(((((sum(t0."amount"::numeric) - sum(t0."order_id"::numeric)) / NULLIF(sum(t0."order_id"::numeric), 0)) * 100))::numeric, 2) AS "ROMI, %"`},
		{"на уникальный идентификатор",
			Metric{Func: FuncCalc, Expr: "sum(amount) / count_distinct(customer_id)", Label: "На клиента"},
			`(sum(t0."amount"::numeric) / NULLIF(count(DISTINCT t0."customer_id"::numeric), 0))`},
		{"количество строк",
			Metric{Func: FuncCalc, Expr: "count(*) / count()", Label: "Единица"},
			`(count(*) / NULLIF(count(*), 0))`},
		{"среднее длительности",
			Metric{Func: FuncCalc, Expr: "avg(shipped_at - created_at) / 24", Label: "Дней"},
			`avg((date_part('epoch', t0."shipped_at" - t0."created_at") / 3600)::numeric)`},
		{"условие метрики навешивается на каждый агрегат",
			Metric{Func: FuncCalc, Expr: "sum(amount) / count()", Label: "Средний оплаченный",
				Filters: []Filter{{Column: "is_paid", Op: OpEq, Value: true}}},
			`((sum(t0."amount"::numeric) FILTER (WHERE t0."is_paid" = $1)) / NULLIF((count(*) FILTER (WHERE t0."is_paid" = $1)), 0))`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat := lifecycleCatalog()
			got, err := Compile(Query{Dataset: "orders", GroupBy: []string{"status"}, Metrics: []Metric{tc.metric}}, cat, 1000)
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if !strings.Contains(got.SQL, tc.want) {
				t.Fatalf("в запросе нет %q:\n%s", tc.want, got.SQL)
			}
			mustPassGuard(t, got, cat)
		})
	}
}

func TestCompileCalcRejects(t *testing.T) {
	cases := []struct {
		name   string
		metric Metric
		wantIn string
	}{
		{"колонка вне функции", Metric{Func: FuncCalc, Expr: "sum(amount) / order_id"}, "вне функции"},
		{"без функций", Metric{Func: FuncCalc, Expr: "1 + 2"}, "нет ни одной функции"},
		{"вложенные функции", Metric{Func: FuncCalc, Expr: "sum(avg(amount))"}, "нельзя вкладывать"},
		{"функция в обычной формуле", Metric{Func: FuncSum, Expr: "sum(amount)"}, "только в метрике с fn calc"},
		{"незнакомая функция", Metric{Func: FuncCalc, Expr: "pg_sleep(1)"}, "вне функции"},
		{"сумма дат", Metric{Func: FuncCalc, Expr: "sum(created_at)"}, "нельзя складывать"},
		{"без формулы", Metric{Func: FuncCalc}, "нужна формула"},
		{"незакрытая функция", Metric{Func: FuncCalc, Expr: "sum(amount"}, "не закрыта скобка"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(Query{Dataset: "orders", GroupBy: []string{"status"}, Metrics: []Metric{tc.metric}}, lifecycleCatalog(), 1000)
			if !errors.Is(err, apperr.ErrValidation) {
				t.Fatalf("ожидалась ошибка проверки, получено %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("ошибка %q не содержит %q", err, tc.wantIn)
			}
		})
	}
}

func TestNumericBins(t *testing.T) {
	col := func(minV, maxV string, distinct int64) domain.Column {
		return domain.Column{Name: "x", Type: domain.ColumnTypeNumeric,
			Profile: &domain.ColumnProfile{DistinctCount: distinct, MinValue: &minV, MaxValue: &maxV}}
	}
	cases := []struct {
		name string
		col  domain.Column
		want []float64
	}{
		{"вес от 0,1 до 45 кг", col("0.1", "45", 30000), []float64{0.2, 0.5, 1, 2, 5, 10, 20}},
		{"деньги — по порядкам", col("10", "500000", 90000), []float64{100, 1000, 10000, 100000}},
		{"узкий диапазон — равные шаги", col("3.1", "3.9", 500), []float64{3.2, 3.4, 3.6, 3.8}},
		{"мало значений — без диапазонов", col("1", "64", 8), nil},
		{"текст не делится", domain.Column{Type: domain.ColumnTypeText}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := numericBins(tc.col)
			if len(got) != len(tc.want) {
				t.Fatalf("границы %v, ожидались %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("границы %v, ожидались %v", got, tc.want)
				}
			}
		})
	}
}

func TestGroupByContinuousColumnUsesRanges(t *testing.T) {
	cat := lifecycleCatalog()
	minV, maxV := "0.1", "45"
	cat.Datasets[0].Columns[4].Profile = &domain.ColumnProfile{DistinctCount: 30000, MinValue: &minV, MaxValue: &maxV}
	got, err := Compile(Query{Dataset: "orders", GroupBy: []string{"Сумма"}, Metrics: []Metric{{Func: FuncCount}}}, cat, 1000)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	for _, want := range []string{
		`CASE WHEN t0."amount" IS NULL THEN NULL WHEN t0."amount" < 0.2 THEN 'до 0,2' WHEN t0."amount" < 0.5 THEN '0,2–0,5'`,
		`ELSE 'от 20' END AS "Сумма"`,
		`ORDER BY min(t0."amount")`,
	} {
		if !strings.Contains(got.SQL, want) {
			t.Errorf("в запросе нет %q:\n%s", want, got.SQL)
		}
	}
	mustPassGuard(t, got, cat)

	byRange, err := Compile(Query{Dataset: "orders", GroupBy: []string{"Сумма"}, Metrics: []Metric{{Func: FuncCount}},
		Sort: &Sort{By: "Сумма", Desc: true}}, cat, 1000)
	if err != nil || !strings.Contains(byRange.SQL, `ORDER BY min(t0."amount") DESC`) {
		t.Fatalf("сортировка по диапазонам — по числу, а не по подписи: %v\n%v", err, byRange)
	}

	sorted, err := Compile(Query{Dataset: "orders", GroupBy: []string{"Сумма"}, Metrics: []Metric{{Func: FuncCount, Label: "N"}},
		Sort: &Sort{By: "N", Desc: true}}, cat, 1000)
	if err != nil || !strings.Contains(sorted.SQL, "ORDER BY 2 DESC") {
		t.Fatalf("явная сортировка важнее порядка диапазонов: %v\n%v", err, sorted)
	}
}
