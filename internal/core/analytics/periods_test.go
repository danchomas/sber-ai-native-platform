package analytics

import (
	"math"
	"strings"
	"testing"
)

func comparisonQuery() PeriodComparisonQuery {
	return PeriodComparisonQuery{
		Dataset: "orders", TimeColumn: "started_at", Metric: Metric{Duration: &Duration{Start: "started_at", End: "finished_at", Unit: "hour"}},
		Baseline: PeriodWindow{From: "2026-09-01", To: "2026-09-11"}, Current: PeriodWindow{From: "2026-09-21", To: "2026-10-01"},
		GroupBy: []string{"status", "amount"}, Direction: "lower", ObservedUntil: "2026-10-10", FollowupHours: 168,
	}
}

func TestCompilePeriodComparison(t *testing.T) {
	q := comparisonQuery()
	q.Filters = []Filter{{Column: "status", Op: OpEq, Value: "x' OR 1=1 --"}}
	compiled, err := CompilePeriodComparison(q, durationCatalog())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"WITH observations AS", `t0."started_at" >= $1::timestamp`, `t0."started_at" < $2::timestamp`,
		`CASE WHEN t0."finished_at" >= t0."started_at"`, "var_samp(value)", "percentile_cont(0.9)",
		"AS unobserved", "AS immature", "interval '1 hour'", "GROUP BY period_id, g0, g1"} {
		if !strings.Contains(compiled.SQL, want) {
			t.Errorf("нет %s в %s", want, compiled.SQL)
		}
	}
	if strings.Contains(compiled.SQL, "OR 1=1") || len(compiled.Args) != 7 {
		t.Fatalf("значения должны быть параметрами: %s, %v", compiled.SQL, compiled.Args)
	}
	mustPassGuard(t, compiled, durationCatalog())
}

func TestPeriodComparisonValidatesInputs(t *testing.T) {
	cases := map[string]func(*PeriodComparisonQuery){
		"пересечение":                  func(q *PeriodComparisonQuery) { q.Current.From = "2026-09-10" },
		"пустой период":                func(q *PeriodComparisonQuery) { q.Baseline.To = q.Baseline.From },
		"не дата":                      func(q *PeriodComparisonQuery) { q.Current.To = "2026-99-99" },
		"инъекция":                     func(q *PeriodComparisonQuery) { q.Current.To = "2026-10-01'); DROP TABLE x;--" },
		"единицы":                      func(q *PeriodComparisonQuery) { q.Metric.Duration.Unit = "week" },
		"неизвестный разрез":           func(q *PeriodComparisonQuery) { q.GroupBy = []string{"password"} },
		"нечисловой показатель":        func(q *PeriodComparisonQuery) { q.Metric = Metric{Column: "status"} },
		"неоднозначный показатель":     func(q *PeriodComparisonQuery) { q.Metric.Column = "amount" },
		"агрегат вместо распределения": func(q *PeriodComparisonQuery) { q.Metric.Func = FuncSum },
		"нет горизонта наблюдения":     func(q *PeriodComparisonQuery) { q.ObservedUntil = "" },
		"ещё нет данных":               func(q *PeriodComparisonQuery) { q.ObservedUntil = "2026-09-25" },
		"направление":                  func(q *PeriodComparisonQuery) { q.Direction = "guess" },
		"бесконечный горизонт":         func(q *PeriodComparisonQuery) { q.FollowupHours = math.Inf(1) },
		"период по тексту":             func(q *PeriodComparisonQuery) { q.TimeColumn = "status" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			q := comparisonQuery()
			change(&q)
			if _, err := CompilePeriodComparison(q, durationCatalog()); err == nil {
				t.Fatal("ожидался отказ")
			}
		})
	}
}

func TestPeriodComparisonWarnsAboutCensoringAndUnequalWindows(t *testing.T) {
	q := comparisonQuery()
	q.ObservedUntil, q.FollowupHours = "", 0
	q.Baseline.To = "2026-09-15"
	compiled, err := CompilePeriodComparison(q, durationCatalog())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Длины периодов", "Граница наблюдения", "Единый горизонт"} {
		if !strings.Contains(strings.Join(compiled.Warnings, " "), want) {
			t.Errorf("нет оговорки %q", want)
		}
	}
}

func TestPeriodComparisonNumericMeasuresAndCompletionCohorts(t *testing.T) {
	for _, metric := range []Metric{{Column: "amount"}, {Expr: "amount / 2"}} {
		q := comparisonQuery()
		q.Metric, q.ObservedUntil, q.FollowupHours = metric, "", 0
		compiled, err := CompilePeriodComparison(q, durationCatalog())
		if err != nil {
			t.Fatal(err)
		}
		mustPassGuard(t, compiled, durationCatalog())
	}
	q := comparisonQuery()
	q.TimeColumn = "finished_at"
	compiled, err := CompilePeriodComparison(q, durationCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(compiled.Warnings, " "), "Когорта выбрана по концу") {
		t.Fatal("нужна оговорка о смещении по завершённым")
	}
}
