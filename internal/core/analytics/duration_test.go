package analytics

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
)

// lifecycleCatalog — заказы с несколькими датами жизненного цикла:
// на них проверяются длительности и условия у метрик.
func lifecycleCatalog() Catalog {
	cat := testCatalog()
	cat.Datasets[0].Columns = append(cat.Datasets[0].Columns,
		domain.Column{Name: "shipped_at", SourceName: "Отгружен (МСК)", Type: domain.ColumnTypeTimestamp},
		domain.Column{Name: "due_date", SourceName: "Срок", Type: domain.ColumnTypeDate},
	)
	return cat
}

func TestCompileDurations(t *testing.T) {
	cases := []struct {
		name   string
		metric Metric
		want   string
	}{
		{"разность моментов — часы",
			Metric{Func: FuncAvg, Expr: "shipped_at - created_at", Label: "Ожидание, ч"},
			`round(avg((date_part('epoch', t0."shipped_at" - t0."created_at") / 3600)::numeric), 2) AS "Ожидание, ч"`},
		{"заголовки из файла и дни",
			Metric{Func: FuncMedian, Expr: `("Отгружен (МСК)" - «Дата заказа») / 24`, Label: "Дней"},
			`percentile_cont(0.5) WITHIN GROUP (ORDER BY ((date_part('epoch', t0."shipped_at" - t0."created_at") / 3600)::numeric / NULLIF(24, 0)))`},
		{"дата без времени приводится к моменту",
			Metric{Func: FuncMax, Expr: "shipped_at - due_date", Label: "Опоздание, ч"},
			`date_part('epoch', t0."shipped_at" - t0."due_date"::timestamp)`},
		{"число строк с известной длительностью",
			Metric{Func: FuncCount, Expr: "shipped_at - created_at", Label: "Отгружено"},
			`count((date_part('epoch', t0."shipped_at" - t0."created_at") / 3600)::numeric) AS "Отгружено"`},
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

func TestCompileMetricConditions(t *testing.T) {
	cases := []struct {
		name     string
		metric   Metric
		want     string
		wantArgs []any
	}{
		{"среднее по части строк",
			Metric{Func: FuncAvg, Expr: "shipped_at - created_at", Label: "Крупные",
				Filters: []Filter{{Column: "amount", Op: OpGte, Value: 1000.0}}},
			`round((avg((date_part('epoch', t0."shipped_at" - t0."created_at") / 3600)::numeric) FILTER (WHERE t0."amount" >= $1)), 2) AS "Крупные"`,
			[]any{1000.0}},
		{"доля строк с условием по формуле",
			Metric{Func: FuncShare, Label: "Просрочено, %",
				Filters: []Filter{{Expr: "shipped_at - due_date", Op: OpGt, Value: 0.0}}},
			`round(100.0 * (count(*) FILTER (WHERE ((date_part('epoch', t0."shipped_at" - t0."due_date"::timestamp) / 3600)::numeric) > $1)) / ` +
				`NULLIF((count(*) FILTER (WHERE ((date_part('epoch', t0."shipped_at" - t0."due_date"::timestamp) / 3600)::numeric) IS NOT NULL)), 0), 2) AS "Просрочено, %"`,
			[]any{0.0}},
		{"доля суммы с условием",
			Metric{Func: FuncShare, Column: "amount", Label: "Оплачено, %",
				Filters: []Filter{{Column: "is_paid", Op: OpEq, Value: true}}},
			`round(100.0 * (sum(t0."amount"::numeric) FILTER (WHERE t0."is_paid" = $1)) / NULLIF((sum(t0."amount"::numeric) FILTER (WHERE t0."is_paid" IS NOT NULL)), 0), 2)`,
			[]any{true}},
		{"количество с условием",
			Metric{Func: FuncCount, Label: "Отгружено", Filters: []Filter{{Column: "shipped_at", Op: OpNotNull}}},
			`(count(*) FILTER (WHERE t0."shipped_at" IS NOT NULL)) AS "Отгружено"`, nil},
		{"медиана колонки с условием",
			Metric{Func: FuncMedian, Column: "amount", Label: "М", Filters: []Filter{{Column: "status", Op: OpEq, Value: "paid"}}},
			`(percentile_cont(0.5) WITHIN GROUP (ORDER BY t0."amount") FILTER (WHERE t0."status" = $1))`, []any{"paid"}},
		{"знаменатель per с тем же условием",
			Metric{Func: FuncSum, Column: "amount", Per: "customer_id", Label: "На клиента",
				Filters: []Filter{{Column: "status", Op: OpEq, Value: "paid"}}},
			`NULLIF((count(DISTINCT t0."customer_id") FILTER (WHERE t0."status" = $1)), 0)`, []any{"paid"}},
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
			if tc.wantArgs != nil && !sameArgs(got.Args, tc.wantArgs) {
				t.Fatalf("параметры %v, ожидались %v", got.Args, tc.wantArgs)
			}
			mustPassGuard(t, got, cat)
		})
	}
}

func sameArgs(got, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestRateWithoutGroupBy(t *testing.T) {
	cat := lifecycleCatalog()
	got, err := Compile(Query{Dataset: "orders", Metrics: []Metric{
		{Func: FuncShare, Label: "Просрочено, %", Filters: []Filter{{Expr: "shipped_at - due_date", Op: OpGt, Value: 0}}},
	}}, cat, 1000)
	if err != nil {
		t.Fatalf("доля с условием считается и без разреза: %v", err)
	}
	mustPassGuard(t, got, cat)
}

func TestFilterByExpression(t *testing.T) {
	cat := lifecycleCatalog()
	got, err := Compile(Query{
		Dataset: "orders", GroupBy: []string{"status"},
		Filters: []Filter{
			{Expr: "(shipped_at - created_at) / 24", Op: OpBetween, Values: []any{1.0, "3,5"}},
			{Expr: "shipped_at - created_at", Op: OpNotNull},
		},
	}, cat, 1000)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	for _, want := range []string{
		`WHERE (((date_part('epoch', t0."shipped_at" - t0."created_at") / 3600)::numeric / NULLIF(24, 0))) BETWEEN $1 AND $2`,
		`AND ((date_part('epoch', t0."shipped_at" - t0."created_at") / 3600)::numeric) IS NOT NULL`,
	} {
		if !strings.Contains(got.SQL, want) {
			t.Errorf("в запросе нет %q:\n%s", want, got.SQL)
		}
	}
	if !sameArgs(got.Args, []any{1.0, 3.5}) {
		t.Fatalf("значения должны приводиться к числу и идти параметрами: %v", got.Args)
	}
	mustPassGuard(t, got, cat)
}

func TestConditionRejects(t *testing.T) {
	cases := []struct {
		name   string
		query  Query
		wantIn string
	}{
		{"текст вместо числа у формулы",
			Query{Dataset: "orders", Filters: []Filter{{Expr: "shipped_at - created_at", Op: OpGt, Value: "1; DROP"}}, GroupBy: []string{"status"}},
			"формула"},
		{"подстрока у формулы",
			Query{Dataset: "orders", Filters: []Filter{{Expr: "amount", Op: OpContains, Value: "1"}}, GroupBy: []string{"status"}},
			"формулу можно сравнивать"},
		{"фильтр без колонки и формулы",
			Query{Dataset: "orders", Filters: []Filter{{Op: OpEq, Value: 1}}, GroupBy: []string{"status"}},
			"не указана колонка"},
		{"слишком много условий у метрики",
			Query{Dataset: "orders", GroupBy: []string{"status"}, Metrics: []Metric{{Func: FuncCount, Filters: make([]Filter, MaxMetricConds+1)}}},
			"условий у одной метрики"},
		{"неизвестная колонка в условии метрики",
			Query{Dataset: "orders", GroupBy: []string{"status"}, Metrics: []Metric{{Func: FuncCount, Filters: []Filter{{Column: "pg_authid", Op: OpNotNull}}}}},
			"нет в датасете"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.query, lifecycleCatalog(), 1000)
			if !errors.Is(err, apperr.ErrValidation) {
				t.Fatalf("ожидалась ошибка проверки, получено %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("ошибка %q не содержит %q", err, tc.wantIn)
			}
		})
	}
}

func TestCompileCoverage(t *testing.T) {
	cat := lifecycleCatalog()
	q := Query{
		Dataset: "orders", Time: &TimeGroup{Column: "created_at", Grain: GrainWeek},
		Filters: []Filter{{Column: "status", Op: OpNe, Value: "cancelled"}},
		Sort:    &Sort{By: "Длительность", Desc: true},
		Metrics: []Metric{
			{Func: FuncCount, Label: "Заказов"},
			{Func: FuncAvg, Expr: "shipped_at - created_at", Label: "Длительность",
				Filters: []Filter{{Column: "amount", Op: OpGt, Value: 10.0}}},
		},
	}
	got, ok, err := CompileCoverage(q, cat, 1000)
	if err != nil || !ok {
		t.Fatalf("ожидался запрос покрытия: ok=%v, %v", ok, err)
	}
	for _, want := range []string{
		`(count(*) FILTER (WHERE t0."amount" > $1)) AS "` + CoverageTotal + `"`,
		`(count((date_part('epoch', t0."shipped_at" - t0."created_at") / 3600)::numeric) FILTER (WHERE t0."amount" > $2)) AS "` + CoverageKnown + `"`,
		`WHERE t0."status" <> $3`,
		`date_trunc('week', t0."created_at")::date`,
	} {
		if !strings.Contains(got.SQL, want) {
			t.Errorf("в запросе нет %q:\n%s", want, got.SQL)
		}
	}
	mustPassGuard(t, got, cat)

	_, ok, err = CompileCoverage(Query{Dataset: "orders", GroupBy: []string{"status"},
		Metrics: []Metric{{Func: FuncSum, Expr: "amount * 2"}}}, cat, 1000)
	if err != nil || ok {
		t.Fatalf("без длительностей запрос покрытия не нужен: ok=%v, %v", ok, err)
	}
}

func TestDateBoundsCoverWholePeriod(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		name     string
		filter   Filter
		want     string
		wantArgs []any
	}{
		{"between по дням включает последний день",
			Filter{Column: "created_at", Op: OpBetween, Values: []any{"2026-09-01", "2026-09-30"}},
			`t0."created_at" >= $1 AND t0."created_at" < $2`, []any{day(2026, 9, 1), day(2026, 10, 1)}},
		{"between по месяцам",
			Filter{Column: "created_at", Op: OpBetween, Values: []any{"2026-07", "2026-09"}},
			`t0."created_at" >= $1 AND t0."created_at" < $2`, []any{day(2026, 7, 1), day(2026, 10, 1)}},
		{"по дату включительно", Filter{Column: "created_at", Op: OpLte, Value: "30.09.2026"},
			`t0."created_at" < $1`, []any{day(2026, 10, 1)}},
		{"после даты — со следующего дня", Filter{Column: "created_at", Op: OpGt, Value: "2026-09-30"},
			`t0."created_at" >= $1`, []any{day(2026, 10, 1)}},
		{"равно году", Filter{Column: "created_at", Op: OpEq, Value: 2025.0},
			`t0."created_at" >= $1 AND t0."created_at" < $2`, []any{day(2025, 1, 1), day(2026, 1, 1)}},
		{"не в этот день", Filter{Column: "created_at", Op: OpNe, Value: "2026-09-30"},
			`(t0."created_at" < $1 OR t0."created_at" >= $2)`, []any{day(2026, 9, 30), day(2026, 10, 1)}},
		{"время задано точно", Filter{Column: "created_at", Op: OpLte, Value: "2026-09-30 12:00:00"},
			`t0."created_at" <= $1`, []any{time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}},
		{"с даты — как раньше", Filter{Column: "created_at", Op: OpGte, Value: "2026-09-01"},
			`t0."created_at" >= $1`, []any{day(2026, 9, 1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat := lifecycleCatalog()
			got, err := Compile(Query{Dataset: "orders", GroupBy: []string{"status"}, Filters: []Filter{tc.filter}}, cat, 1000)
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if !strings.Contains(got.SQL, tc.want) {
				t.Fatalf("в запросе нет %q:\n%s", tc.want, got.SQL)
			}
			if !sameArgs(got.Args, tc.wantArgs) {
				t.Fatalf("параметры %v, ожидались %v", got.Args, tc.wantArgs)
			}
			mustPassGuard(t, got, cat)
		})
	}
}

func TestColumnNameWithoutPunctuation(t *testing.T) {
	cat := lifecycleCatalog()
	for _, ref := range []string{"отгружен мск", "ОТГРУЖЕН (МСК)", "Отгружен, МСК"} {
		got, err := Compile(Query{Dataset: "orders", Metrics: []Metric{{Func: FuncMax, Column: ref}}}, cat, 1000)
		if err != nil || !strings.Contains(got.SQL, `max(t0."shipped_at")`) {
			t.Fatalf("%q должна найти колонку «Отгружен (МСК)»: %v", ref, err)
		}
	}
}

func TestKnownFilters(t *testing.T) {
	got := KnownFilters([]Filter{
		{Expr: `"Доставлено" - "Срок"`, Op: OpGt, Value: 0},
		{Column: "status", Op: OpEq, Value: "paid"},
		{Column: "status", Op: OpNe, Value: "new"},
		{Column: "error", Op: OpNotNull},
	})
	want := []Filter{
		{Expr: `"Доставлено" - "Срок"`, Op: OpNotNull},
		{Column: "status", Op: OpNotNull},
	}
	if len(got) != len(want) {
		t.Fatalf("получено %+v, ожидалось %+v", got, want)
	}
	for i := range want {
		if got[i].Column != want[i].Column || got[i].Expr != want[i].Expr || got[i].Op != want[i].Op {
			t.Fatalf("условие %d: %+v, ожидалось %+v", i, got[i], want[i])
		}
	}
}

func TestCompileRateCounts(t *testing.T) {
	cat := testCatalog()
	q := Query{
		Dataset: "orders",
		GroupBy: []string{"status"},
		Metrics: []Metric{{Func: FuncShare, Label: "Оплачено, %", Filters: []Filter{{Column: "is_paid", Op: OpEq, Value: true}}}},
	}
	compiled, label, ok, err := CompileRateCounts(q, cat, 100)
	if err != nil || !ok {
		t.Fatalf("запрос не собран: ok=%v err=%v", ok, err)
	}
	if label != "Оплачено, %" {
		t.Fatalf("подпись %q", label)
	}
	for _, want := range []string{
		`(count(*) FILTER (WHERE t0."is_paid" IS NOT NULL)) AS "` + RateKnown + `"`,
		`(count(*) FILTER (WHERE t0."is_paid" = $1)) AS "` + RateEvents + `"`,
	} {
		if !strings.Contains(compiled.SQL, want) {
			t.Fatalf("в запросе нет %q:\n%s", want, compiled.SQL)
		}
	}
	mustPassGuard(t, compiled, cat)

	for name, other := range map[string]Query{
		"по времени":       {Dataset: "orders", Time: &TimeGroup{Column: "created_at", Grain: GrainMonth}, Metrics: q.Metrics},
		"без разреза":      {Dataset: "orders", Metrics: q.Metrics},
		"доля без условия": {Dataset: "orders", GroupBy: q.GroupBy, Metrics: []Metric{{Func: FuncShare}}},
	} {
		if _, _, ok, _ := CompileRateCounts(other, cat, 100); ok {
			t.Errorf("%s: проверка разброса не нужна, но запрос собран", name)
		}
	}
}
