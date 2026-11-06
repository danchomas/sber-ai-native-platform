//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/sqlguard"
	chatsvc "github.com/heinousness/aibi/internal/features/chat/service"
)

const tasksCSV = `id;team;created;started;finished;planned
1;A;2026-09-01 00:00:00;2026-09-02 00:00:00;2026-09-03 00:00:00;16
2;A;2026-09-04 00:00:00;2026-09-05 00:00:00;2026-09-07 00:00:00;16
3;A;2026-09-21 00:00:00;2026-09-22 00:00:00;2026-09-22 12:00:00;16
4;A;2026-09-22 00:00:00;2026-09-23 00:00:00;;16
5;A;2026-09-24 00:00:00;2026-09-25 00:00:00;2026-09-24 00:00:00;16
6;A;2026-09-29 00:00:00;2026-09-30 00:00:00;2026-10-02 00:00:00;16
7;A;2026-09-27 00:00:00;;;16
8;A;2026-09-30 00:00:00;2026-10-01 00:00:00;2026-10-01 12:00:00;16
`

func TestTaskDurationsAndPeriodComparisonOnPostgres(t *testing.T) {
	pool := setup(t)
	e := newEnv(t, pool)
	ctx := context.Background()
	upload(t, e, "tasks", tasksCSV)
	datasets, err := e.datasets.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := e.datasets.Scopes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cat := analytics.Catalog{Datasets: datasets, DefaultSchema: testSchema}
	q := analytics.Query{Dataset: "tasks", GroupBy: []string{"team"}, Filters: []analytics.Filter{{Column: "planned", Op: analytics.OpGte, Value: 16.0}}, Metrics: []analytics.Metric{
		{Func: analytics.FuncCount, Label: "Всего"},
		{Func: analytics.FuncCount, Duration: &analytics.Duration{Start: "created", End: "started", Unit: "hour"}, Label: "С ожиданием"},
		{Func: analytics.FuncMedian, Duration: &analytics.Duration{Start: "created", End: "started", Unit: "hour"}, Label: "Ожидание"},
		{Func: analytics.FuncP90, Duration: &analytics.Duration{Start: "started", End: "finished", Unit: "hour"}, Label: "P90 выполнения"},
	}}
	compiled, err := analytics.Compile(q, cat, queryCfg.MaxRows)
	if err != nil {
		t.Fatal(err)
	}
	scope, checked, err := sqlguard.ResolveScope(compiled.SQL, scopes, testSchema, queryCfg.MaxRows)
	if err != nil {
		t.Fatalf("sqlguard: %v\n%s", err, compiled.SQL)
	}
	rs, err := e.raw.Query(ctx, scope.Target, checked.SQL, queryCfg.MaxRows, compiled.Args...)
	if err != nil {
		t.Fatalf("Postgres: %v\n%s", err, compiled.SQL)
	}
	if len(rs.Rows) != 1 || rs.Rows[0][1] != int64(8) || rs.Rows[0][2] != int64(7) || rs.Rows[0][3] != float64(24) || rs.Rows[0][4] != float64(48) {
		t.Fatalf("ожидание 24ч, P90=48ч, пропуск не равен нулю: %+v", rs)
	}

	comparison := analytics.PeriodComparisonQuery{
		Dataset: "tasks", TimeColumn: "started", Metric: analytics.Metric{Duration: &analytics.Duration{Start: "started", End: "finished", Unit: "hour"}},
		Baseline: analytics.PeriodWindow{From: "2026-09-01", To: "2026-09-11"}, Current: analytics.PeriodWindow{From: "2026-09-21", To: "2026-10-01"},
		GroupBy: []string{"team", "planned"}, Direction: "lower", ObservedUntil: "2026-10-01", FollowupHours: 48,
	}
	svc := chatsvc.New(e.datasets, e.raw, e.sessions, nil, nil, queryCfg, config.Agent{MaxSteps: 6}, testLogger())
	args, _ := json.Marshal(comparison)
	out, isError, err := svc.CallTool(ctx, "compare_periods", args)
	if err != nil || isError {
		t.Fatalf("compare_periods: %s %v %v", out, isError, err)
	}
	var payload struct {
		Columns  []string `json:"columns"`
		Rows     [][]any  `json:"rows"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Rows) != 1 {
		t.Fatalf("ожидалась одна сопоставимая группа: %s", out)
	}
	values := map[string]any{}
	for i, col := range payload.Columns {
		values[col] = payload.Rows[0][i]
	}
	for name, want := range map[string]float64{
		"Строк до": 2, "Строк после": 4, "Измерений до": 2, "Измерений после": 1,
		"Среднее до": 36, "Среднее после": 12, "Медиана до": 36, "P90 до": 45.6,
		"Без измерения после": 3, "Некорректных после": 1, "Незавершённых в окне после": 2, "Недонаблюдённых после": 1,
	} {
		got, ok := values[name].(float64)
		if !ok || math.Abs(got-want) > 1e-8 {
			t.Errorf("%s: %v, ожидалось %v", name, values[name], want)
		}
	}
	if values["Вывод"] != "Только описательное сравнение: неполные или несопоставимые наблюдения" || values["p-значение"] != nil || len(payload.Warnings) == 0 {
		t.Fatalf("снижение среднего не доказывает ускорение при незавершённых задачах: %s", out)
	}

	var body strings.Builder
	body.WriteString("id;team;started;finished\n")
	for period := range 2 {
		start := time.Date(2026, 9, 2+20*period, 0, 0, 0, 0, time.UTC)
		for i := range 40 {
			end := start.Add(time.Duration(24-12*period+i%5) * time.Hour)
			fmt.Fprintf(&body, "%d;A;%s;%s\n", period*40+i, start.Format("2006-01-02 15:04:05"), end.Format("2006-01-02 15:04:05"))
		}
	}
	upload(t, e, "complete_tasks", body.String())
	comparison.Dataset, comparison.GroupBy, comparison.ObservedUntil, comparison.FollowupHours = "complete_tasks", []string{"team"}, "2026-10-10", 168
	args, _ = json.Marshal(comparison)
	out, isError, err = svc.CallTool(ctx, "compare_periods", args)
	if err != nil || isError {
		t.Fatalf("полные когорты: %s %v %v", out, isError, err)
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	for i, col := range payload.Columns {
		values[col] = payload.Rows[0][i]
	}
	if values["Среднее до"] != float64(26) || values["Среднее после"] != float64(14) || values["Вывод"] != "Среднее изменилось в лучшую сторону" || values["p-значение"] == nil {
		t.Fatalf("полные сопоставимые когорты должны показать различие: %s", out)
	}
}
