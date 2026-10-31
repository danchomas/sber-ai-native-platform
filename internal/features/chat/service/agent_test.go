package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/llm"
)

func TestAgentAnswersThroughTools(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolAggregate,
			`{"dataset":"orders","group_by":["Статус"],"filters":[{"column":"amount","op":"gt","value":100}],"title":"Заказы по статусам"}`)),
		calls(call("c2", toolAnswer,
			`{"text":"Больше всего оплаченных заказов — 700.","result_id":"r1","chart":{"type":"bar"},"followups":["А по месяцам?"]}`)),
	)
	exec := &fakeExecutor{result: sampleResult()}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Сколько крупных заказов в каждом статусе?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	if answer.Intent != domain.IntentQuery {
		t.Fatalf("сценарий %s: %+v", answer.Intent, answer)
	}
	if answer.Text != "Больше всего оплаченных заказов — 700." {
		t.Fatalf("текст ответа: %q", answer.Text)
	}
	if answer.Chart == nil || answer.Chart.Type != domain.ChartTypeBar {
		t.Fatalf("визуализация: %+v", answer.Chart)
	}
	if len(answer.Followups) != 1 || len(answer.Datasets) != 1 || answer.Datasets[0] != "orders" {
		t.Fatalf("метаданные ответа: %+v", answer)
	}
	if len(answer.Query) == 0 || !strings.Contains(string(answer.Query), `"dataset":"orders"`) {
		t.Fatalf("в ответе нет параметров запроса для продолжения диалога: %s", answer.Query)
	}
	if len(answer.Steps) != 1 || answer.Steps[0].Tool != toolAggregate || !answer.Steps[0].OK {
		t.Fatalf("ход работы: %+v", answer.Steps)
	}

	if len(exec.queries) != 1 {
		t.Fatalf("ожидался один запрос к базе, выполнено %d", len(exec.queries))
	}
	if !strings.Contains(exec.queries[0], `t0."amount" > $1`) {
		t.Fatalf("фильтр должен уйти параметром:\n%s", exec.queries[0])
	}
	if len(exec.args[0]) != 1 || exec.args[0][0] != 100.0 {
		t.Fatalf("параметры запроса: %#v", exec.args[0])
	}
}

func TestAgentSendsCatalogAndToolsToModel(t *testing.T) {
	model := newToolLLM(
		domain.Reply{Content: "Здравствуйте! Спросите о данных."},
		domain.Reply{Content: "Здравствуйте! Спросите о данных."},
	)
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Привет")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Intent != domain.IntentSmalltalk || answer.Text != "Здравствуйте! Спросите о данных." {
		t.Fatalf("ответ без инструментов: %+v", answer)
	}

	req := model.requests[0]
	if !strings.Contains(req.System, "ДАТАСЕТ «orders»") || strings.Contains(req.System, "raw.ds_orders") {
		t.Fatal("в промпт агента должен попасть каталог по именам датасетов, без схем и таблиц")
	}
	names := map[string]bool{}
	for _, tool := range req.Tools {
		names[tool.Name] = true
		if !json.Valid(tool.Parameters) {
			t.Errorf("схема инструмента %s — невалидный JSON", tool.Name)
		}
	}
	for _, want := range []string{toolDescribe, toolAggregate, toolRows, toolDuplicates, toolDashboards, toolAnswer, toolClarify, toolDecline} {
		if !names[want] {
			t.Errorf("модели не передан инструмент %s", want)
		}
	}
	if names[toolSQL] {
		t.Fatal("run_sql не должен быть доступен модели по умолчанию")
	}
	for _, denied := range []string{toolDashboard, toolAddWidgets, toolRemove} {
		if names[denied] {
			t.Errorf("на приветствие модель не должна получать изменяющий инструмент %s", denied)
		}
	}
	if !strings.Contains(req.System, "Служебная метка: aibi-") {
		t.Fatal("в системный промпт должна попасть метка-канарейка")
	}
}

func TestAgentRepairsInvalidToolCall(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolAggregate, `{"dataset":"orders","group_by":["region"]}`)),
		calls(call("c2", toolAggregate, `{"dataset":"orders","group_by":"status"}`)),
		calls(call("c3", toolAnswer, `{"text":"Готово."}`)),
	)
	exec := &fakeExecutor{result: sampleResult()}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Заказы по регионам")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	if len(exec.queries) != 1 {
		t.Fatalf("ошибочный вызов не должен доходить до базы: выполнено %d запросов", len(exec.queries))
	}
	tools := model.toolMessages()
	if len(tools) != 2 || !strings.Contains(tools[0].Content, "region") || !strings.Contains(tools[0].Content, "нет в датасете") {
		t.Fatalf("модель должна увидеть причину ошибки: %+v", tools)
	}
	if len(answer.Steps) != 2 || answer.Steps[0].OK || !answer.Steps[1].OK {
		t.Fatalf("ход работы должен показать ошибку и исправление: %+v", answer.Steps)
	}
	if answer.Intent != domain.IntentQuery || answer.Result == nil {
		t.Fatalf("после исправления ожидался результат: %+v", answer)
	}
}

func TestAgentBuildsDashboard(t *testing.T) {
	model := newToolLLM(
		calls(
			call("c1", toolAggregate, `{"dataset":"orders","metrics":[{"fn":"count"}],"title":"Всего заказов"}`),
			call("c2", toolAggregate, `{"dataset":"orders","time":{"column":"created_at","grain":"quarter"},"metrics":[{"fn":"sum","column":"amount"}],"title":"Выручка по кварталам"}`),
		),
		calls(call("c3", toolDashboard,
			`{"title":"Квартальная статистика","widgets":[{"result_id":"r1","chart":{"type":"kpi"}},{"result_id":"r2","title":"Выручка","chart":{"type":"line"}},{"result_id":"r9"}]}`)),
		calls(call("c4", toolAnswer, `{"text":"Собрал панель с квартальной статистикой."}`)),
	)
	dashboards := &fakeDashboards{}
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, dashboards)

	answer, err := svc.Ask(context.Background(), sessionID, "Создай дашборд с ежеквартальной статистикой")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	if answer.Intent != domain.IntentDashboard || answer.DashboardID == nil {
		t.Fatalf("ожидался дашборд: %+v", answer)
	}
	if len(dashboards.created) != 1 {
		t.Fatalf("сохранено дашбордов: %d", len(dashboards.created))
	}
	widgets := dashboards.created[0].Widgets
	if len(widgets) != 2 {
		t.Fatalf("виджетов %d, ожидалось 2 (r9 не существует)", len(widgets))
	}
	if widgets[1].Title != "Выручка" || len(widgets[1].Query) == 0 {
		t.Fatalf("виджет должен хранить параметры запроса для обновления: %+v", widgets[1])
	}
	if tools := model.toolMessages(); len(tools) != 3 || !strings.Contains(tools[2].Content, "skipped") {
		t.Fatal("модель должна узнать о пропущенном виджете")
	}
}

func TestAgentClarifiesAndDeclines(t *testing.T) {
	cases := []struct {
		name   string
		reply  domain.Reply
		intent domain.AnswerIntent
		text   string
		follow int
	}{
		{
			name:   "уточнение",
			reply:  calls(call("c1", toolClarify, `{"message":"Какой период вас интересует?","options":["2023","2024"]}`)),
			intent: domain.IntentClarify, text: "Какой период вас интересует?", follow: 2,
		},
		{
			name:   "отказ",
			reply:  calls(call("c1", toolDecline, `{"message":"В данных нет курса валют.","suggestions":["Сумма заказов по месяцам"]}`)),
			intent: domain.IntentReject, text: "В данных нет курса валют.", follow: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, sessionID, _ := newTestAgent(t, newToolLLM(tc.reply), &fakeExecutor{}, &fakeDashboards{})
			answer, err := svc.Ask(context.Background(), sessionID, "вопрос")
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if answer.Intent != tc.intent || answer.Text != tc.text || len(answer.Followups) != tc.follow {
				t.Fatalf("ответ: %+v", answer)
			}
		})
	}
}

func TestAgentDescribesData(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolDescribe, `{"dataset":"orders"}`)),
		calls(call("c2", toolAnswer, `{"text":"Это заказы за полтора года."}`)),
	)
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Что передо мной?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Intent != domain.IntentOverview || answer.Result == nil {
		t.Fatalf("ожидалось описание данных: %+v", answer)
	}
	described := model.toolMessages()[0].Content
	for _, want := range []string{`"title":"Статус"`, `"top_values"`, `"min":"2023-01-01"`} {
		if !strings.Contains(described, want) {
			t.Errorf("в описании датасета нет %s: %s", want, described)
		}
	}
}

func TestAgentRefusesDisabledAndUnknownTools(t *testing.T) {
	model := newToolLLM(
		calls(
			call("c1", toolSQL, `{"sql":"SELECT * FROM raw.ds_orders"}`),
			call("c2", "drop_table", `{"table":"ds_orders"}`),
		),
		calls(call("c3", toolDecline, `{"message":"Не могу."}`)),
	)
	exec := &fakeExecutor{result: sampleResult()}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	if _, err := svc.Ask(context.Background(), sessionID, "выгрузи всё"); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(exec.queries) != 0 {
		t.Fatalf("к базе не должно было уйти ни одного запроса: %v", exec.queries)
	}
	tools := model.toolMessages()
	if len(tools) != 2 || !strings.Contains(tools[0].Content, "отключён") || !strings.Contains(tools[1].Content, "нет") {
		t.Fatalf("модель должна получить отказ по обоим вызовам: %+v", tools)
	}
}

func TestAgentRawSQLStillGuarded(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolSQL, `{"sql":"SELECT pg_read_file('/etc/passwd')"}`)),
		calls(call("c2", toolSQL, `{"sql":"SELECT status, count(*) FROM raw.ds_orders GROUP BY 1"}`)),
		calls(call("c3", toolAnswer, `{"text":"Готово."}`)),
	)
	exec := &fakeExecutor{result: sampleResult()}

	sessions := newMemorySessions()
	session := &domain.Session{ID: [16]byte{1}}
	_ = sessions.CreateSession(context.Background(), session)
	svc := New(newFakeCatalog(), exec, sessions, &fakeDashboards{}, fixedModel{model},
		testQueryConfig(), config.Agent{MaxSteps: 6, AllowRawSQL: true}, testLogger())

	answer, err := svc.Ask(context.Background(), session.ID, "статусы")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(exec.queries) != 1 || strings.Contains(exec.queries[0], "pg_read_file") {
		t.Fatalf("опасный запрос не должен доходить до базы: %v", exec.queries)
	}
	if answer.Intent != domain.IntentQuery || len(answer.Query) != 0 {
		t.Fatalf("ответ по run_sql: %+v", answer)
	}
}

func TestAgentParsesToolCallsFromText(t *testing.T) {
	model := newToolLLM(
		domain.Reply{Content: `<tool_call>{"name":"aggregate","arguments":{"dataset":"orders","group_by":["status"]}}</tool_call>`},
		domain.Reply{Content: `<tool_call>{"name":"answer","arguments":{"text":"Готово."}}</tool_call>`},
	)
	exec := &fakeExecutor{result: sampleResult()}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "статусы")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(exec.queries) != 1 || answer.Text != "Готово." || answer.Result == nil {
		t.Fatalf("вызовы текстом не распознаны: %+v", answer)
	}
}

func TestAgentStopsAfterMaxSteps(t *testing.T) {
	var replies []domain.Reply
	for i := range 10 {
		replies = append(replies, calls(call(fmt.Sprint("c", i), toolAggregate,
			fmt.Sprintf(`{"dataset":"orders","group_by":["status"],"limit":%d}`, i+1))))
	}
	model := newToolLLM(replies...)
	exec := &fakeExecutor{result: sampleResult()}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "статусы")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if model.conversations() != testAgentConfig().MaxSteps {
		t.Fatalf("обращений к модели %d, предел %d", model.conversations(), testAgentConfig().MaxSteps)
	}
	if answer.Result == nil || len(answer.Warnings) == 0 || answer.Text == "" {
		t.Fatalf("после исчерпания шагов ожидался последний результат с оговоркой: %+v", answer)
	}
}

func TestAgentFallsBackWhenToolsUnsupported(t *testing.T) {
	model := newToolLLM()
	model.err = fmt.Errorf("%w: %w", apperr.ErrLLMUnavailable, llm.ErrToolsUnsupported)
	model.planner = []string{
		`{"intent":"query","query":{"dataset":"orders","group_by":["status"]}}`,
		`{"intent":"query","query":{"dataset":"orders","metrics":[{"fn":"count"}]}}`,
	}
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, &fakeDashboards{})

	for range 2 {
		answer, err := svc.Ask(context.Background(), sessionID, "статусы")
		if err != nil {
			t.Fatalf("неожиданная ошибка: %v", err)
		}
		if answer.Intent != domain.IntentQuery {
			t.Fatalf("запасной путь через план не сработал: %+v", answer)
		}
	}
	if model.conversations() != 1 {
		t.Fatalf("после отказа в инструментах модель не должна спрашиваться повторно: %d обращений", model.conversations())
	}
}

func TestAgentFallsBackWhenModelDown(t *testing.T) {
	model := newToolLLM()
	model.err = fmt.Errorf("%w: connection refused", apperr.ErrLLMUnavailable)
	model.scriptedLLM.err = errors.New("недоступна")
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "что в данных?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Intent != domain.IntentOverview || len(answer.Warnings) == 0 {
		t.Fatalf("без модели ожидалось описание по профилю: %+v", answer)
	}
	if model.callCount("planner") != 0 {
		t.Fatal("при недоступной модели не нужно ждать ещё и планировщика")
	}
}

func TestPlanWithoutToolsNeverRunsModelSQL(t *testing.T) {
	model := newScriptedLLM(`{"intent":"query","sql":"SELECT * FROM raw.ds_orders"}`)
	model.repair = []string{`{"query":{"dataset":"orders","group_by":["Статус"],"filters":[{"column":"status","op":"ne","value":"cancelled"}]}}`}
	exec := &fakeExecutor{result: sampleResult()}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "заказы по статусам")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if model.callCount("repair") != 1 {
		t.Fatalf("SQL модели должен быть отклонён и переписан в параметры: починок %d", model.callCount("repair"))
	}
	if len(exec.queries) != 1 || !strings.Contains(exec.queries[0], `t0."status" <> $1`) {
		t.Fatalf("выполняться должен только собранный платформой запрос: %v", exec.queries)
	}
	if answer.Intent != domain.IntentQuery || len(answer.Query) == 0 {
		t.Fatalf("ответ по плану с параметрами: %+v", answer)
	}
}

func TestPlanDashboardWithQueries(t *testing.T) {
	model := newScriptedLLM(`{"intent":"dashboard","title":"Панель","widgets":[
		{"title":"Всего","query":{"dataset":"orders","metrics":[{"fn":"count"}]},"chart":{"type":"kpi"}},
		{"title":"По месяцам","query":{"dataset":"orders","time":{"column":"created_at","grain":"month"}}},
		{"title":"SQL-виджет","sql":"SELECT 1 FROM raw.ds_orders"}
	]}`)
	dashboards := &fakeDashboards{}
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, dashboards)

	answer, err := svc.Ask(context.Background(), sessionID, "собери панель")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Intent != domain.IntentDashboard || len(dashboards.created) != 1 {
		t.Fatalf("ожидался дашборд: %+v", answer)
	}
	widgets := dashboards.created[0].Widgets
	if len(widgets) != 2 || len(widgets[0].Query) == 0 {
		t.Fatalf("SQL-виджет не должен попасть на панель, остальные хранят query: %+v", widgets)
	}
	if len(answer.Warnings) != 1 {
		t.Fatalf("о пропущенном виджете нужно предупредить: %v", answer.Warnings)
	}
}

func TestAgentEditsExistingDashboard(t *testing.T) {
	existing := domain.Dashboard{
		ID: uuid.New(), Title: "Продажи",
		Widgets: []domain.Widget{{ID: uuid.New(), Title: "Старый график"}},
	}
	dashboards := &fakeDashboards{created: []domain.Dashboard{existing}}

	model := newToolLLM(
		calls(call("c1", toolDashboards, `{}`)),
		calls(call("c2", toolAggregate, `{"dataset":"orders","group_by":["status"],"title":"Заказы по статусам"}`)),
		calls(
			call("c3", toolAddWidgets, fmt.Sprintf(`{"dashboard_id":%q,"widgets":[{"result_id":"r1","chart":{"type":"pie"}}]}`, existing.ID)),
			call("c4", toolRemove, fmt.Sprintf(`{"dashboard_id":%q,"widget_id":%q}`, existing.ID, existing.Widgets[0].ID)),
		),
		calls(call("c5", toolAnswer, `{"text":"Заменил старый график на структуру заказов по статусам."}`)),
	)
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, dashboards)

	answer, err := svc.Ask(context.Background(), sessionID, "Замени на дашборде продаж старый график на заказы по статусам")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Intent != domain.IntentDashboard || answer.DashboardID == nil || *answer.DashboardID != existing.ID {
		t.Fatalf("ответ должен вести на изменённый дашборд: %+v", answer)
	}
	if len(dashboards.added) != 1 || len(dashboards.added[0].Query) == 0 || dashboards.added[0].Chart.Type != domain.ChartTypePie {
		t.Fatalf("добавленный виджет: %+v", dashboards.added)
	}
	if len(dashboards.removed) != 1 || dashboards.removed[0] != existing.Widgets[0].ID {
		t.Fatalf("удалённый виджет: %v", dashboards.removed)
	}
	listed := model.requests[1].Messages
	if last := listed[len(listed)-1]; !strings.Contains(last.Content, existing.Widgets[0].ID.String()) {
		t.Fatalf("модель должна увидеть id виджетов: %s", last.Content)
	}
}

func monthlyResult(columns []string, rows int, value func(i int) []any) *domain.ResultSet {
	rs := &domain.ResultSet{}
	rs.Columns = append(rs.Columns, domain.ResultColumn{Name: columns[0], Type: domain.ColumnTypeDate})
	for _, c := range columns[1:] {
		rs.Columns = append(rs.Columns, domain.ResultColumn{Name: c, Type: domain.ColumnTypeNumeric})
	}
	for i := range rows {
		period := time.Date(2022, time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, i, 0).Format("2006-01-02")
		rs.Rows = append(rs.Rows, append([]any{period}, value(i)...))
	}
	rs.RowCount = len(rs.Rows)
	return rs
}

func TestAgentInsightTools(t *testing.T) {
	seasonal := monthlyResult([]string{"Месяц", "Сумма"}, 36, func(i int) []any {
		v := 100.0 + float64(i)
		if i%12 == 6 {
			v += 80
		}
		return []any{v}
	})
	leading := monthlyResult([]string{"Месяц", "Реклама", "Заказы"}, 24, func(i int) []any {
		ad := float64((i * 7) % 11)
		prev := float64(((i - 1) * 7) % 11)
		if i == 0 {
			prev = 0
		}
		return []any{ad, prev * 10}
	})

	cases := []struct {
		name     string
		result   *domain.ResultSet
		call     string
		args     string
		analysis string
		chart    domain.ChartType
		want     []string
	}{
		{
			name: "сезонность", result: seasonal, call: toolSeasonality,
			args:     `{"dataset":"orders","time":{"column":"created_at","grain":"month"},"metric":{"fn":"sum","column":"amount"}}`,
			analysis: "seasonality", chart: domain.ChartTypeBar,
			want: []string{`"peak":"Июль"`, "Отклонение от среднего"},
		},
		{
			name: "прогноз", result: seasonal, call: toolForecast,
			args:     `{"dataset":"orders","time":{"column":"created_at","grain":"month"},"metric":{"fn":"sum","column":"amount"},"horizon":6}`,
			analysis: "forecast", chart: domain.ChartTypeLine,
			want: []string{`"method":"тренд с сезонностью"`, `"horizon":6`},
		},
		{
			name: "корреляция с лагом", result: leading, call: toolCorrelate,
			args:     `{"dataset":"orders","time":{"column":"created_at","grain":"month"},"x":{"fn":"sum","column":"amount","label":"Реклама"},"y":{"fn":"count","label":"Заказы"},"max_lag":3}`,
			analysis: "correlation", chart: domain.ChartTypeBar,
			want: []string{`"best_lag":1`, `"strength":"сильная"`},
		},
		{
			name: "выбросы", result: seasonal, call: toolAnomalies,
			args:     `{"dataset":"orders","time":{"column":"created_at","grain":"month"}}`,
			analysis: "anomalies", chart: domain.ChartTypeTable,
			want: []string{`"periods_checked":36`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := newToolLLM(
				calls(call("c1", tc.call, tc.args)),
				calls(call("c2", toolAnswer, `{"text":"Готово."}`)),
			)
			exec := &fakeExecutor{result: tc.result}
			svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

			answer, err := svc.Ask(context.Background(), sessionID, "гипотеза")
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			out := model.toolMessages()[0].Content
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("в результате инструмента нет %s:\n%s", w, out)
				}
			}
			if answer.Analysis != tc.analysis || answer.Chart == nil || answer.Chart.Type != tc.chart {
				t.Fatalf("ответ: анализ %q, график %+v", answer.Analysis, answer.Chart)
			}
			if len(answer.Query) != 0 {
				t.Fatal("у производного расчёта нет параметров запроса для дашборда")
			}
			if len(exec.queries) != 1 || !strings.Contains(exec.queries[0], "date_trunc('month'") {
				t.Fatalf("ряд должен собираться компилятором: %v", exec.queries)
			}
		})
	}
}

func TestAgentRecalculatesAtFixedVolume(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolRecalc,
			`{"dataset":"orders","target":{"fn":"sum","expr":"amount * 2","label":"Выручка"},"column":"amount","change_pct":10,"filters":[{"column":"status","op":"eq","value":"paid"}]}`)),
		calls(call("c2", toolAnswer, `{"text":"Выручка вырастет.","result_id":"r1"}`)),
	)
	exec := &fakeExecutor{results: []*domain.ResultSet{
		{Columns: []domain.ResultColumn{{Name: "Выручка", Type: domain.ColumnTypeNumeric}}, Rows: [][]any{{1000.0}}, RowCount: 1},
		{Columns: []domain.ResultColumn{{Name: "Выручка", Type: domain.ColumnTypeNumeric}, {Name: "Сценарий", Type: domain.ColumnTypeNumeric}},
			Rows: [][]any{{400.0, 440.0}}, RowCount: 1},
	}}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Как изменится выручка, если поднять цену на 10%?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Analysis != analysisWhatIf || answer.Result == nil || len(answer.Result.Rows) != 2 {
		t.Fatalf("ответ: %+v", answer)
	}
	if got := answer.Result.Rows[1][1]; got != 1040.0 {
		t.Fatalf("сценарий: %v, ожидалось 1040", got)
	}
	if strings.Contains(exec.queries[0], "WHERE") {
		t.Fatalf("итог должен считаться по всем строкам:\n%s", exec.queries[0])
	}
	if !strings.Contains(exec.queries[1], "* 1.1") || !strings.Contains(exec.queries[1], "WHERE") {
		t.Fatalf("сценарий должен считаться по затронутым строкам:\n%s", exec.queries[1])
	}
}

func TestAgentRetriesForeignScriptInAnswer(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolAggregate, `{"dataset":"orders","group_by":["Статус"]}`)),
		calls(call("c2", toolAnswer, `{"text":"Больше всего 紧随其后 оплаченных.","result_id":"r1"}`)),
		calls(call("c3", toolAnswer, `{"text":"Больше всего оплаченных.","result_id":"r1"}`)),
	)
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Сколько заказов в каждом статусе?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Text != "Больше всего оплаченных." {
		t.Fatalf("текст ответа: %q", answer.Text)
	}
}

func TestAgentFallsBackWhenForeignScriptPersists(t *testing.T) {
	model := newToolLLM(domain.Reply{Content: "Здесь 紧随其后 текст"}, domain.Reply{Content: "Снова 紧随其后 текст"})
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Привет")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if hasForeignScript(answer.Text) || answer.Text == "" {
		t.Fatalf("текст ответа: %q", answer.Text)
	}
}

func TestAgentHidesTechnicalTableName(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolAggregate, `{"dataset":"orders","group_by":["Статус"]}`)),
		calls(call("c2", toolAnswer, `{"text":"Посчитано по raw.ds_orders: всё хорошо.","result_id":"r1"}`)),
	)
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Сколько заказов в каждом статусе?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if strings.Contains(answer.Text, "ds_orders") {
		t.Fatalf("в ответе техническое имя: %q", answer.Text)
	}
}

func TestAgentShowsRawRecordsAsTable(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolRows, `{"dataset":"orders"}`)),
		calls(call("c2", toolAnswer, `{"text":"Вот записи.","result_id":"r1","chart":{"type":"line"}}`)),
	)
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Покажи записи")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Chart == nil || answer.Chart.Type != domain.ChartTypeTable || answer.Headline != nil || len(answer.ChartOptions) != 0 {
		t.Fatalf("сырые записи должны быть таблицей: %+v", answer)
	}
}

func TestAgentHidesEmptyRecords(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolDuplicates, `{"dataset":"orders"}`)),
		calls(call("c2", toolAnswer, `{"text":"Повторов нет.","result_id":"r1"}`)),
	)
	exec := &fakeExecutor{result: &domain.ResultSet{
		Columns: []domain.ResultColumn{{Name: "Код", Type: domain.ColumnTypeText}},
		Rows:    [][]any{},
	}}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Есть ли тут повторяющиеся данные?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Result != nil || answer.Chart != nil {
		t.Fatalf("пустую таблицу записей показывать не нужно: %+v", answer)
	}
}

func TestAgentSkipsDuplicateWidgets(t *testing.T) {
	dashboards := &fakeDashboards{}
	model := newToolLLM(
		calls(call("c1", toolAggregate, `{"dataset":"orders","metrics":[{"fn":"count"},{"fn":"sum","column":"amount"}]}`)),
		calls(call("c2", toolDashboard, `{"title":"Панель","widgets":[
			{"result_id":"r1","title":"Заказы","chart":{"type":"kpi"}},
			{"result_id":"r1","title":"Сумма","chart":{"type":"kpi"}},
			{"result_id":"r1","title":"Таблица","chart":{"type":"table"}}]}`)),
		calls(call("c3", toolAnswer, `{"text":"Готово."}`)),
	)
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, dashboards)

	answer, err := svc.Ask(context.Background(), sessionID, "Собери дашборд")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Intent != domain.IntentDashboard {
		t.Fatalf("сценарий %s: %+v", answer.Intent, answer)
	}
	tool := model.toolMessages()
	if !strings.Contains(tool[1].Content, `"widgets":2`) {
		t.Fatalf("одинаковый виджет должен быть пропущен: %s", tool[1].Content)
	}
}

func TestAgentShowsSummaryRowAsKPI(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolAggregate, `{"dataset":"orders","metrics":[{"fn":"count"}]}`)),
		calls(call("c2", toolAnswer, `{"text":"Всего 5.","result_id":"r1","chart":{"type":"table"}}`)),
	)
	exec := &fakeExecutor{result: &domain.ResultSet{
		Columns: []domain.ResultColumn{
			{Name: "Записей", Type: domain.ColumnTypeInteger},
			{Name: "Начало", Type: domain.ColumnTypeTimestamp},
		},
		Rows: [][]any{{int64(5), "2024-10-01T00:00:00Z"}}, RowCount: 1,
	}}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "За какой период они и сколько их?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Chart == nil || answer.Chart.Type != domain.ChartTypeKPI {
		t.Fatalf("итоговая строка должна быть плитками: %+v", answer.Chart)
	}
}

func TestAgentDropsRepeatedCountInSegments(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolCompare, `{"dataset":"orders","segments":["status"],"metrics":[{"fn":"count","label":"Записей"},{"fn":"sum","expr":"amount * 2","label":"Выручка"}]}`)),
		calls(call("c2", toolAnswer, `{"text":"Сравнил.","result_id":"r1","chart":{"type":"bar","y":["Записей","Выручка"]}}`)),
	)
	exec := &fakeExecutor{result: sampleResult()}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Что отличает статусы?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if strings.Contains(exec.queries[0], "Записей (2)") {
		t.Fatalf("количество записей посчитано дважды:\n%s", exec.queries[0])
	}
	if answer.Chart == nil || answer.Chart.Type != domain.ChartTypeTable {
		t.Fatalf("разные масштабы показываем таблицей: %+v", answer.Chart)
	}
}

func TestEchoIgnoresScenarioAnswers(t *testing.T) {
	for _, text := range []string{
		"Как изменится выручка, если поднять цену товаров категории «Электроника» на 10%? Выручка вырастет с 2,68 млрд до 2,74 млрд рублей.",
		"Какой рост выручки ожидается в ближайшие кварталы при текущих условиях: выручка снизится на 2%.",
		"Что отличает покупателей типа «Организация» от «Физическое лицо»? Средний чек организаций втрое выше.",
		"Есть ли сезонные пики зимой? Да, в декабре средняя выручка 156 млн рублей.",
		"Сумма цен не является выручкой, поэтому выручка посчитана как количество, умноженное на цену со скидкой.",
	} {
		if promptEcho.Repeats(text) {
			t.Errorf("обычный ответ принят за утечку промпта: %q", text)
		}
	}
}

func TestAgentRejectsAnswerWithoutData(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolAnswer, `{"text":"Лидер — 727 млн, это столбчатая диаграмма."}`)),
		calls(call("c2", toolAggregate, `{"dataset":"orders","group_by":["Статус"]}`)),
		calls(call("c3", toolAnswer, `{"text":"Лидер — оплаченные.","result_id":"r1","chart":{"type":"bar"}}`)),
	)
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "А теперь покажи то же самое в другом виде")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Intent != domain.IntentQuery || answer.Result == nil || answer.Chart == nil {
		t.Fatalf("ответ без расчёта должен вернуться модели: %+v", answer)
	}
}
