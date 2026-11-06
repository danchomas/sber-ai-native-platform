package analytics

import (
	"strings"
	"testing"

	"github.com/heinousness/aibi/internal/core/domain"
)

func durationCatalog() Catalog {
	cat := testCatalog()
	cat.Datasets[0].Columns = append(cat.Datasets[0].Columns,
		domain.Column{Name: "started_at", SourceName: "Начало работы", Type: domain.ColumnTypeTimestamp},
		domain.Column{Name: "finished_at", SourceName: "Завершено", Type: domain.ColumnTypeTimestamp},
		domain.Column{Name: "due_date", Type: domain.ColumnTypeDate})
	return cat
}

func TestCompileDurationMetrics(t *testing.T) {
	cat := durationCatalog()
	for _, unit := range DurationUnits {
		for _, fn := range []Func{FuncAvg, FuncMedian, FuncP90, FuncMin, FuncMax, FuncSum, FuncCount} {
			t.Run(unit+"/"+string(fn), func(t *testing.T) {
				q := Query{Dataset: "orders", GroupBy: []string{"status"}, Metrics: []Metric{
					{Func: fn, Duration: &Duration{Start: "Дата заказа", End: "Начало работы", Unit: unit}},
					{Func: FuncCount},
				}}
				compiled, err := Compile(q, cat, 1000)
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{`CASE WHEN t0."started_at" >= t0."created_at"`, `date_part('epoch', (t0."started_at"::timestamp - t0."created_at"::timestamp))`, "count(*)"} {
					if !strings.Contains(compiled.SQL, want) {
						t.Errorf("нет %s в %s", want, compiled.SQL)
					}
				}
				if len(compiled.Warnings) == 0 {
					t.Fatal("нужна оговорка об исключённых интервалах")
				}
				mustPassGuard(t, compiled, cat)
			})
		}
	}
}

func TestDurationRejectsUnsafeAndAmbiguousMetrics(t *testing.T) {
	base := Duration{Start: "created_at", End: "started_at", Unit: "hour"}
	cases := []Metric{
		{Func: FuncAvg, Duration: &Duration{Start: "status", End: "started_at", Unit: "hour"}},
		{Func: FuncAvg, Duration: &Duration{Start: "created_at", End: "missing", Unit: "hour"}},
		{Func: FuncAvg, Duration: &Duration{Start: "created_at", End: "created_at", Unit: "hour"}},
		{Func: FuncAvg, Duration: &Duration{Start: "created_at", End: "started_at", Unit: "hour'); DROP TABLE x;--"}},
		{Func: FuncAvg, Duration: &base, Expr: "amount"},
		{Func: FuncAvg, Duration: &base, Column: "amount"},
		{Func: FuncSum, Duration: &base, Per: "order_id"},
		{Func: FuncCountDistinct, Duration: &base},
	}
	for i, metric := range cases {
		if _, err := Compile(Query{Dataset: "orders", Metrics: []Metric{metric}}, durationCatalog(), 100); err == nil {
			t.Errorf("пример %d должен отклоняться", i)
		}
	}
}

func TestP90WorksForNumericColumnAndExpression(t *testing.T) {
	for _, m := range []Metric{{Func: FuncP90, Column: "amount"}, {Func: FuncP90, Expr: "amount / 2"}} {
		compiled, err := Compile(Query{Dataset: "orders", Metrics: []Metric{m}}, testCatalog(), 1000)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(compiled.SQL, "percentile_cont(0.9)") {
			t.Fatal(compiled.SQL)
		}
		mustPassGuard(t, compiled, testCatalog())
	}
}
