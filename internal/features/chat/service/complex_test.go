package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/domain"
)

// withClosedAt добавляет заказам вторую дату: от неё считаются длительности.
func withClosedAt(svc *Service) {
	catalog := svc.catalog.(*fakeCatalog)
	catalog.datasets[0].Columns = append(catalog.datasets[0].Columns, domain.Column{
		Name: "closed_at", SourceName: "Закрыт", Type: domain.ColumnTypeTimestamp, Role: domain.RoleTemporal,
		Profile: &domain.ColumnProfile{RowCount: 1000, NullCount: 80, DistinctCount: 900},
	})
}

func resultSet(columns []string, rows ...[]any) *domain.ResultSet {
	rs := &domain.ResultSet{Rows: rows, RowCount: len(rows)}
	for _, c := range columns {
		rs.Columns = append(rs.Columns, domain.ResultColumn{Name: c, Type: domain.ColumnTypeNumeric})
	}
	return rs
}

func TestAggregateWarnsWhenRecentDurationsAreIncomplete(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolAggregate, `{"dataset":"orders","time":{"column":"created_at","grain":"week"},`+
			`"metrics":[{"fn":"avg","expr":"\"Закрыт\" - \"Дата заказа\"","label":"Длительность, ч"}]}`)),
		calls(call("c2", toolAnswer, `{"text":"Однозначно утверждать нельзя: последние недели ещё не закрыты.","result_id":"r1"}`)),
	)
	main := resultSet([]string{"Неделя", "Длительность, ч"},
		[]any{"2024-06-03", 45.0}, []any{"2024-06-10", 44.0}, []any{"2024-06-17", 43.0}, []any{"2024-06-24", 19.0})
	coverage := resultSet([]string{"Неделя", analytics.CoverageTotal, analytics.CoverageKnown},
		[]any{"2024-06-03", int64(100), int64(95)}, []any{"2024-06-10", int64(100), int64(94)},
		[]any{"2024-06-17", int64(100), int64(93)}, []any{"2024-06-24", int64(100), int64(23)})
	exec := &fakeExecutor{results: []*domain.ResultSet{main, coverage}}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})
	withClosedAt(svc)

	answer, err := svc.Ask(context.Background(), sessionID, "Стали ли заказы закрываться быстрее в конце июня?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(exec.queries) != 2 || !strings.Contains(exec.queries[1], `count((date_part('epoch'`) {
		t.Fatalf("после расчёта длительности ожидалась проверка полноты: %v", exec.queries)
	}
	warning := strings.Join(answer.Warnings, "\n")
	for _, want := range []string{"Длительность известна не у всех записей", "«неделя с 24.06.2024» — у 23%", "обычно у 94%"} {
		if !strings.Contains(warning, want) {
			t.Errorf("в предупреждениях нет %q: %v", want, answer.Warnings)
		}
	}
	var toModel strings.Builder
	for _, m := range model.toolMessages() {
		toModel.WriteString(m.Content)
	}
	if !strings.Contains(toModel.String(), "Длительность известна не у всех записей") || !strings.Contains(toModel.String(), "долю записей, завершённых") {
		t.Fatalf("модель должна увидеть предупреждение и указание, как пересчитать: %s", toModel.String())
	}
	if strings.Contains(warning, "исключи") || strings.Contains(warning, "filters") {
		t.Fatalf("указания для модели не должны попадать к пользователю: %v", answer.Warnings)
	}
}

func TestAggregateWithoutDurationsSkipsCoverage(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolAggregate, `{"dataset":"orders","group_by":["status"],"metrics":[{"fn":"sum","column":"amount"}]}`)),
		calls(call("c2", toolAnswer, `{"text":"Больше всего — paid.","result_id":"r1"}`)),
	)
	exec := &fakeExecutor{result: sampleResult()}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})
	if _, err := svc.Ask(context.Background(), sessionID, "Сумма по статусам"); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(exec.queries) != 1 {
		t.Fatalf("без длительностей лишних запросов быть не должно: %v", exec.queries)
	}
}

func TestCoverageNote(t *testing.T) {
	cases := []struct {
		name string
		rs   *domain.ResultSet
		want string
	}{
		{"одна строка с пропусками",
			resultSet([]string{analytics.CoverageTotal, analytics.CoverageKnown}, []any{int64(200), int64(80)}),
			"известна только у 40% записей (80 из 200)"},
		{"одна полная строка", resultSet([]string{analytics.CoverageTotal, analytics.CoverageKnown}, []any{int64(200), int64(195)}), ""},
		{"группы без провалов",
			resultSet([]string{"Статус", analytics.CoverageTotal, analytics.CoverageKnown},
				[]any{"a", int64(100), int64(90)}, []any{"b", int64(100), int64(88)}, []any{"c", int64(100), int64(92)}), ""},
		{"пустое значение группы",
			resultSet([]string{"Статус", analytics.CoverageTotal, analytics.CoverageKnown},
				[]any{"a", int64(100), int64(90)}, []any{"b", int64(100), int64(91)}, []any{nil, int64(100), int64(10)}),
			"«(пусто)» — у 10%"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := coverageNote(&dataFilter{}, tc.rs, "")
			if tc.want == "" && got != "" {
				t.Fatalf("предупреждения быть не должно: %s", got)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("в %q нет %q", got, tc.want)
			}
		})
	}
}

func TestPartialPeriod(t *testing.T) {
	end := "2024-06-27 18:00:00+00"
	r := &agentRun{datasets: []domain.Dataset{{
		Name: "orders", Status: domain.DatasetStatusReady,
		Columns: []domain.Column{{Name: "created_at", SourceName: "Дата заказа", Type: domain.ColumnTypeTimestamp,
			Profile: &domain.ColumnProfile{MaxValue: &end}}},
	}}}
	weeks := resultSet([]string{"Неделя", "Заказов"}, []any{"2024-06-24", int64(10)}, []any{"2024-06-17", int64(70)})
	cases := []struct {
		name  string
		grain analytics.Grain
		rs    *domain.ResultSet
		want  string
	}{
		{"неделя обрывается", analytics.GrainWeek, weeks, "Последний период (с 24.06.2024) неполный: данные заканчиваются 27.06.2024, в нём 4 дн. из 7"},
		{"месяц обрывается", analytics.GrainMonth, resultSet([]string{"Месяц", "Заказов"}, []any{"2024-06-01", int64(10)}), "с 01.06.2024"},
		{"полный месяц", analytics.GrainMonth, resultSet([]string{"Месяц", "Заказов"}, []any{"2024-05-01", int64(10)}), ""},
		{"сезонность не проверяется", analytics.GrainMonthOfYear, resultSet([]string{"Месяц года", "Заказов"}, []any{int64(6), int64(10)}), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := analytics.Query{Dataset: "orders", Time: &analytics.TimeGroup{Column: "Дата заказа", Grain: tc.grain}}
			got := r.partialPeriod(q, tc.rs)
			if tc.want == "" && got != "" {
				t.Fatalf("предупреждения быть не должно: %s", got)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("в %q нет %q", got, tc.want)
			}
		})
	}
}

func TestFindDriversRanksSignificantCondition(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolDrivers, `{"dataset":"orders","metric":{"fn":"avg","expr":"closed_at - created_at","label":"Ожидание, ч"},`+
			`"group_a":{"column":"amount","op":"gte","value":1000},"group_b":[{"column":"amount","op":"lt","value":1000}],`+
			`"label_a":"Крупные","label_b":"Мелкие","dimensions":["Статус"]}`)),
		calls(call("c2", toolAnswer, `{"text":"Крупные заказы дольше ждут в четвёртом квартале.","result_id":"r1"}`)),
	)
	cols := func(first string) []string {
		return []string{first, "na", "va", "sa", "nb", "vb", "sb"}
	}
	row := func(v any, na, va, nb, vb float64) []any { return []any{v, na, va, 30.0, nb, vb, 30.0} }
	exec := &fakeExecutor{
		results: []*domain.ResultSet{
			resultSet(cols("")[1:], []any{1000.0, 40.0, 30.0, 3000.0, 36.0, 30.0}),
			resultSet(cols("Статус"), row("paid", 600, 41, 1800, 36), row("new", 300, 39, 900, 36), row("cancelled", 100, 38, 300, 36)),
			resultSet(cols("Квартал года"), row(int64(1), 250, 37, 750, 36), row(int64(2), 250, 37, 750, 36),
				row(int64(3), 250, 37, 750, 36), row(int64(4), 250, 50, 750, 36)),
		},
		result: resultSet(cols("x"), row(int64(1), 500, 40, 1500, 36), row(int64(2), 500, 40, 1500, 36)),
	}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})
	withClosedAt(svc)

	answer, err := svc.Ask(context.Background(), sessionID, "При каких условиях крупные заказы дольше ждут закрытия?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(answer.Steps) != 1 || !answer.Steps[0].OK {
		t.Fatalf("поиск условий должен пройти: %+v", answer.Steps)
	}
	if want := 1 + 1 + 4; len(exec.queries) != want {
		t.Fatalf("ожидалось %d запросов (все, статус, четыре календарных признака), получено %d", want, len(exec.queries))
	}
	for _, q := range exec.queries {
		if !strings.Contains(q, "FILTER (WHERE") || !strings.Contains(q, "stddev_samp") {
			t.Fatalf("группы и разброс считаются одним запросом с FILTER: %s", q)
		}
	}

	var payload struct {
		Summary struct {
			Ranking []struct {
				Dimension string `json:"dimension"`
				Highest   string `json:"highest"`
				Strength  float64
			} `json:"ranking"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(model.toolMessages()[0].Content), &payload); err != nil {
		t.Fatalf("вывод инструмента: %v", err)
	}
	top := payload.Summary.Ranking[0]
	if !strings.HasPrefix(top.Dimension, "Квартал года") || top.Highest != "4 квартал" {
		t.Fatalf("сильнейшим должен быть квартал с пиком в четвёртом: %+v", payload.Summary.Ranking)
	}
	for _, item := range payload.Summary.Ranking[1:] {
		if item.Strength != 0 {
			t.Errorf("различия в пределах шума не должны давать силу признака: %+v", item)
		}
	}
	if answer.Result == nil || answer.Result.Rows[0][0] != driverOverallName {
		t.Fatalf("первая строка таблицы — общий итог: %+v", answer.Result)
	}
	if answer.Analysis != analysisDrivers {
		t.Fatalf("результат поиска условий — производный расчёт: %q", answer.Analysis)
	}
}

func TestFindDriversSkipsEventColumns(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolDrivers, `{"dataset":"orders","metric":{"fn":"share",`+
			`"filters":[{"column":"status","op":"eq","value":"cancelled"}],"label":"Отмены, %"}}`)),
		calls(call("c2", toolAnswer, `{"text":"Чаще отменяют в вебе.","result_id":"r1"}`)),
	)
	cols := func(name string) []string { return []string{name, "n", "k"} }
	exec := &fakeExecutor{
		results: []*domain.ResultSet{
			resultSet(cols("")[1:], []any{1000.0, 100.0}),
			resultSet(cols("Причина отмены"), []any{nil, 900.0, 0.0}, []any{"Передумал", 60.0, 60.0}, []any{"Дорого", 40.0, 40.0}),
			resultSet(cols("Канал"), []any{"web", 500.0, 70.0}, []any{"app", 500.0, 30.0}),
		},
		result: resultSet(cols("x"), []any{int64(1), 500.0, 50.0}, []any{int64(2), 500.0, 50.0}),
	}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})
	withReasons(svc)

	if _, err := svc.Ask(context.Background(), sessionID, "При каких условиях чаще отменяют заказы?"); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if want := 1 + 2 + 4; len(exec.queries) != want {
		t.Fatalf("статус задаёт событие и не перебирается: ожидалось %d запросов, получено %d", want, len(exec.queries))
	}
	payload := toolPayload(t, model)
	ranking := payload["summary"].(map[string]any)["ranking"].([]any)
	for _, item := range ranking {
		if dim := item.(map[string]any)["dimension"]; dim == "Причина отмены" || dim == "Статус" {
			t.Fatalf("признак, повторяющий событие, не условие: %v", ranking)
		}
	}
	if top := ranking[0].(map[string]any)["dimension"]; top != "Канал" {
		t.Fatalf("сильнейшее условие — канал: %v", ranking)
	}
	if !strings.Contains(fmt.Sprint(payload["excluded"]), "Причина отмены") {
		t.Fatalf("модель должна знать, что причина отмены отброшена: %v", payload["excluded"])
	}
}

func TestOutcomeProxy(t *testing.T) {
	tests := []struct {
		name   string
		values []driverStats
		want   bool
	}{
		{"описание ошибки", []driverStats{{n: 900, a: 0}, {n: 60, a: 100}, {n: 40, a: 100}}, true},
		{"экран: нулевые есть, стопроцентных нет", []driverStats{{n: 880, a: 0}, {n: 80, a: 0.15}, {n: 40, a: 8.4}}, false},
		{"обычный признак", []driverStats{{n: 500, a: 14}, {n: 500, a: 6}}, false},
		{"две группы", []driverStats{{n: 900, a: 0, nB: 2850, b: 0}, {n: 100, a: 100, nB: 150, b: 100}}, true},
		{"нет событий", []driverStats{{n: 500, a: 0}, {n: 500, a: 0}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := outcomeProxy(tt.values); got != tt.want {
				t.Fatalf("outcomeProxy = %v, ожидалось %v", got, tt.want)
			}
		})
	}
}

func TestFindDriversRejectsBadInput(t *testing.T) {
	cases := []struct {
		name, args, want string
	}{
		{"без метрики", `{"dataset":"orders"}`, "укажи metric"},
		{"чужой датасет", `{"dataset":"pg_authid","metric":{"fn":"count"}}`, "нет в каталоге"},
		{"неизвестный признак", `{"dataset":"orders","metric":{"fn":"count"},"dimensions":["pg_authid"]}`, "нет в датасете"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _ := newTestAgent(t, newToolLLM(), &fakeExecutor{}, &fakeDashboards{})
			datasets, _ := svc.catalog.List(context.Background())
			scopes, _ := svc.catalog.Scopes(context.Background())
			r := svc.newRun(uuid.Nil, "", datasets, nil, scopes, svc.DataTools())
			r.filter = &dataFilter{}
			_, _, err := r.findDrivers(context.Background(), []byte(tc.args))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ожидалась ошибка с %q, получено %v", tc.want, err)
			}
		})
	}
}

func TestNormalizeArgsWrapsMetricConditions(t *testing.T) {
	raw := normalizeArgs(json.RawMessage(`{"metrics":[{"fn":"avg","filters":{"column":"a","op":"gt","value":1}}],`+
		`"metric":{"fn":"share","filters":{"expr":"x - y","op":"gt","value":0}}}`), "metrics")
	var got struct {
		Metrics []analytics.Metric `json:"metrics"`
		Metric  analytics.Metric   `json:"metric"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("условие-объект должно стать списком: %v\n%s", err, raw)
	}
	if len(got.Metrics[0].Filters) != 1 || got.Metric.Filters[0].Expr != "x - y" {
		t.Fatalf("условия потерялись: %s", raw)
	}
}

func TestSuggestionsFollowAnsweredDataset(t *testing.T) {
	tasks := domain.Dataset{Name: "Задачи", Status: domain.DatasetStatusReady, Columns: []domain.Column{
		{Name: "prioritet", SourceName: "Приоритет", Role: domain.RoleDimension},
	}}
	sales := domain.Dataset{Name: "Продажи", Status: domain.DatasetStatusReady, Columns: []domain.Column{
		{Name: "kanal", SourceName: "Канал", Role: domain.RoleDimension},
	}}
	got := suggestQuestions([]domain.Dataset{sales, tasks}, "Задачи")
	if len(got) == 0 || !strings.Contains(got[0], "Приоритет") {
		t.Fatalf("подсказки должны быть по датасету ответа: %v", got)
	}
	if got := suggestQuestions([]domain.Dataset{sales, tasks}); !strings.Contains(got[0], "Канал") {
		t.Fatalf("без предпочтений — первый датасет: %v", got)
	}
}

func TestPeriodInShareWarning(t *testing.T) {
	svc, _, _ := newTestAgent(t, newToolLLM(), &fakeExecutor{}, &fakeDashboards{})
	datasets, _ := svc.catalog.List(context.Background())
	r := svc.newRun(uuid.Nil, "", datasets, nil, nil, nil)
	r.filter = &dataFilter{}
	late := analytics.Filter{Column: "status", Op: analytics.OpEq, Value: "late"}
	inYear := analytics.Filter{Column: "Дата заказа", Op: analytics.OpBetween, Values: []any{"2025-01-01", "2025-12-31"}}

	q := analytics.Query{Dataset: "orders", GroupBy: []string{"status"}, Metrics: []analytics.Metric{
		{Func: analytics.FuncShare, Label: "Просрочено в 2025, %", Filters: []analytics.Filter{late, inYear}},
		{Func: analytics.FuncShare, Label: "Просрочено, %", Filters: []analytics.Filter{late}},
	}}
	got := r.periodInShare(q)
	if !strings.Contains(got, "«Просрочено в 2025, %»") || strings.Contains(got, "«Просрочено, %»") {
		t.Fatalf("предупреждение должно называть только долю с периодом в условии: %q", got)
	}
	q.Metrics = q.Metrics[1:]
	if got := r.periodInShare(q); got != "" {
		t.Fatalf("без периода в условии предупреждения быть не должно: %q", got)
	}
}

func TestPartialRowIsHiddenFromModel(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolAggregate, `{"dataset":"orders","time":{"column":"created_at","grain":"week"},"metrics":[{"fn":"count"}]}`)),
		calls(call("c2", toolAnswer, `{"text":"Готово.","result_id":"r1","chart":{"type":"line"}}`)),
	)
	rs := resultSet([]string{"Неделя", "Количество"},
		[]any{"2024-06-10", int64(60)}, []any{"2024-06-17", int64(70)}, []any{"2024-06-24", int64(10)})
	rs.Columns[0].Type = domain.ColumnTypeDate
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: rs}, &fakeDashboards{})
	end := "2024-06-26"
	svc.catalog.(*fakeCatalog).datasets[0].Columns[1].Profile.MaxValue = &end

	answer, err := svc.Ask(context.Background(), sessionID, "Заказы по неделям")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	out := model.toolMessages()[0].Content
	if strings.Contains(out, `["2024-06-24"`) || !strings.Contains(out, `["2024-06-17"`) {
		t.Fatalf("строки неполной недели модель видеть не должна: %s", out)
	}
	if !strings.Contains(out, "partial_period_removed") {
		t.Fatalf("модель должна знать, что период убран: %s", out)
	}
	if len(answer.Result.Rows) != 3 || answer.Result.Rows[2][0] != "2024-06-24" {
		t.Fatalf("таблица пользователя не меняется: %v", answer.Result.Rows)
	}
	h := answer.Headline
	if h == nil || h.Value != 70 || !strings.Contains(h.Label, "Последний полный период") {
		t.Fatalf("главное число — по последней полной неделе: %+v", h)
	}
	if answer.Chart == nil || !answer.Chart.PartialLast {
		t.Fatalf("график должен знать о неполной последней точке: %+v", answer.Chart)
	}
}

func TestCoverageNoteNamesPeriods(t *testing.T) {
	rs := resultSet([]string{"Неделя", analytics.CoverageTotal, analytics.CoverageKnown},
		[]any{"2026-09-07", int64(100), int64(95)}, []any{"2026-09-14", int64(100), int64(94)},
		[]any{"2026-09-21", int64(100), int64(84)}, []any{"2026-09-28", int64(100), int64(23)})
	got := coverageNote(&dataFilter{}, rs, analytics.GrainWeek)
	if !strings.Contains(got, "«неделя с 28.09.2026» — у 23%") || strings.Contains(got, "2026-09-28") {
		t.Fatalf("периоды в предупреждении — по-человечески: %q", got)
	}
}

func TestCalendarNamesAndNoShareForAverages(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolAggregate, `{"dataset":"orders","time":{"column":"created_at","grain":"month_of_year"},`+
			`"metrics":[{"fn":"avg","column":"amount","label":"Средняя сумма"}]}`)),
		calls(call("c2", toolAnswer, `{"text":"Пик — в декабре.","result_id":"r1","chart":{"type":"bar","x":"Месяц года","y":["Средняя сумма"]}}`)),
	)
	rs := &domain.ResultSet{
		Columns: []domain.ResultColumn{{Name: "Месяц года", Type: domain.ColumnTypeInteger}, {Name: "Средняя сумма", Type: domain.ColumnTypeNumeric}},
		Rows:    [][]any{{int64(1), 100.0}, {int64(6), 90.0}, {int64(12), 150.0}}, RowCount: 3,
	}
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: rs}, &fakeDashboards{})
	answer, err := svc.Ask(context.Background(), sessionID, "Средняя сумма по месяцам года")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Result.Rows[2][0] != "Декабрь" || answer.Result.Columns[0].Type != domain.ColumnTypeText {
		t.Fatalf("месяцы должны идти названиями: %v", answer.Result.Rows)
	}
	if !strings.Contains(model.toolMessages()[0].Content, "Декабрь") {
		t.Fatal("модель тоже должна видеть названия месяцев")
	}
	if h := answer.Headline; h != nil && (h.SharePct != nil || strings.Contains(h.Text, "от суммы")) {
		t.Fatalf("у среднего нет «доли от суммы»: %+v", h)
	}
}

func TestAdditiveMetrics(t *testing.T) {
	cases := []struct {
		metrics []analytics.Metric
		want    bool
	}{
		{nil, true},
		{[]analytics.Metric{{Func: analytics.FuncSum, Column: "amount"}, {Func: analytics.FuncCount}}, true},
		{[]analytics.Metric{{Func: analytics.FuncAvg, Column: "amount"}}, false},
		{[]analytics.Metric{{Func: analytics.FuncSum, Column: "amount", Per: "order_id"}}, false},
		{[]analytics.Metric{{Func: analytics.FuncShare}}, false},
		{[]analytics.Metric{{Func: analytics.FuncCalc, Expr: "sum(a)/sum(b)"}}, false},
	}
	for i, tc := range cases {
		if got := additiveMetrics(tc.metrics); got != tc.want {
			t.Errorf("случай %d: %v, ожидалось %v", i, got, tc.want)
		}
	}
}
