package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/promptguard"
)

const comparisonArgs = `{"dataset":"orders","time_column":"started_at","metric":{"duration":{"start":"started_at","end":"finished_at","unit":"hour"}},"baseline":{"from":"2026-09-01","to":"2026-09-11"},"current":{"from":"2026-09-21","to":"2026-10-01"},"group_by":["status"],"direction":"lower","observed_until":"2026-10-10","followup_hours":168}`

func addTaskDates(svc *Service) {
	cat := svc.catalog.(*fakeCatalog)
	cat.datasets[0].Columns = append(cat.datasets[0].Columns,
		domain.Column{Name: "started_at", SourceName: "Начало работы", Type: domain.ColumnTypeTimestamp},
		domain.Column{Name: "finished_at", SourceName: "Завершено", Type: domain.ColumnTypeTimestamp})
}

func comparisonStatsResult() *domain.ResultSet {
	rs := &domain.ResultSet{Columns: []domain.ResultColumn{{Name: "period_id", Type: domain.ColumnTypeInteger}, {Name: "Статус", Type: domain.ColumnTypeText}}}
	for _, name := range []string{"total", "measured", "mean", "variance", "median", "p90", "invalid", "unobserved", "immature"} {
		rs.Columns = append(rs.Columns, domain.ResultColumn{Name: name, Type: domain.ColumnTypeNumeric})
	}
	rs.Rows = [][]any{
		{int64(0), "paid", int64(100), int64(100), 10.0, 4.0, 9.0, 13.0, int64(0), int64(0), int64(0)},
		{int64(1), "paid", int64(100), int64(100), 8.0, 4.0, 7.0, 11.0, int64(0), int64(0), int64(0)},
	}
	rs.RowCount = len(rs.Rows)
	return rs
}

func columnValue(t *testing.T, rs *domain.ResultSet, row int, name string) any {
	t.Helper()
	for i, col := range rs.Columns {
		if col.Name == name {
			return rs.Rows[row][i]
		}
	}
	t.Fatalf("нет колонки %s", name)
	return nil
}

func TestAgentAnalyzesWaitingTimeWithoutRawSQL(t *testing.T) {
	model := newToolLLM(
		calls(call("a1", toolAggregate, `{"dataset":"orders","group_by":["status"],"metrics":[{"fn":"median","duration":{"start":"created_at","end":"started_at","unit":"hour"},"label":"Ожидание, ч"},{"fn":"p90","duration":{"start":"created_at","end":"started_at","unit":"hour"},"label":"P90 ожидания, ч"},{"fn":"count"},{"fn":"count","duration":{"start":"created_at","end":"started_at","unit":"hour"}}],"filters":[{"column":"amount","op":"gte","value":16}]}`)),
		calls(call("a2", toolAnswer, `{"text":"Результат ожидания показан по группам; причинная связь не установлена.","result_id":"r1"}`)),
	)
	exec := &fakeExecutor{result: sampleResult()}
	svc, id, _ := newTestAgent(t, model, exec, &fakeDashboards{})
	addTaskDates(svc)
	answer, err := svc.Ask(context.Background(), id, "Для задач от 16 часов при каких статусах дольше ожидание начала работы?")
	if err != nil {
		t.Fatal(err)
	}
	if answer.Intent != domain.IntentQuery || len(answer.Query) == 0 || len(exec.queries) != 1 {
		t.Fatalf("ответ: %+v", answer)
	}
	for _, want := range []string{"percentile_cont(0.5)", "percentile_cont(0.9)", "date_part('epoch'", `t0."amount" >= $1`} {
		if !strings.Contains(exec.queries[0], want) {
			t.Errorf("нет %s в %s", want, exec.queries[0])
		}
	}
}

func TestAgentComparesPeriodsAndPreservesQuery(t *testing.T) {
	model := newToolLLM(
		calls(call("p1", toolDistributions, comparisonArgs)),
		calls(call("p2", toolAnswer, `{"text":"Средняя длительность снизилась с 10 до 8 часов. Наблюдаемая связь не устанавливает причину.","result_id":"r1"}`)),
	)
	exec := &fakeExecutor{result: comparisonStatsResult()}
	svc, id, _ := newTestAgent(t, model, exec, &fakeDashboards{})
	addTaskDates(svc)
	answer, err := svc.Ask(context.Background(), id, "Сравни время выполнения в указанные периоды по статусам")
	if err != nil {
		t.Fatal(err)
	}
	if answer.Intent != domain.IntentQuery || answer.Analysis != "period_comparison" || answer.Chart.Type != domain.ChartTypeTable || !strings.Contains(string(answer.Query), `"comparison"`) {
		t.Fatalf("ответ сравнения: %+v", answer)
	}
	if columnValue(t, answer.Result, 0, "Изменение, %") != -20.0 || len(answer.Warnings) == 0 {
		t.Fatalf("расчёт и оговорки: %+v", answer)
	}
	if len(exec.args[0]) != 6 {
		t.Fatalf("даты и горизонт должны идти параметрами: %#v", exec.args[0])
	}
	if !strings.Contains(model.toolMessages()[0].Content, "Уэлча") {
		t.Fatal("модель должна получить метод расчёта")
	}
}

func TestPeriodComparisonDoesNotOverclaimIncompleteData(t *testing.T) {
	var q analytics.PeriodComparisonQuery
	if err := json.Unmarshal([]byte(comparisonArgs), &q); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*domain.ResultSet, *analytics.PeriodComparisonQuery){
		"незавершённые": func(rs *domain.ResultSet, q *analytics.PeriodComparisonQuery) {
			rs.Rows[1][3], rs.Rows[1][9] = int64(80), int64(20)
		},
		"некорректные": func(rs *domain.ResultSet, q *analytics.PeriodComparisonQuery) {
			rs.Rows[1][3], rs.Rows[1][8] = int64(80), int64(20)
		},
		"свежие": func(rs *domain.ResultSet, q *analytics.PeriodComparisonQuery) {
			rs.Rows[1][3], rs.Rows[1][10] = int64(80), int64(20)
		},
		"нет даты среза":       func(rs *domain.ResultSet, q *analytics.PeriodComparisonQuery) { q.ObservedUntil = "" },
		"нет общего горизонта": func(rs *domain.ResultSet, q *analytics.PeriodComparisonQuery) { q.FollowupHours = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			rs := comparisonStatsResult()
			query := q
			change(rs, &query)
			out, warnings, err := periodComparisonResult(rs, query)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(columnValue(t, out, 0, "Вывод").(string), "описательное") || len(warnings) < 2 {
				t.Fatalf("нельзя уверенно объявлять улучшение: %+v %v", out.Rows, warnings)
			}
		})
	}
}

func TestPeriodComparisonKeepsUnmatchedAndSmallGroups(t *testing.T) {
	var q analytics.PeriodComparisonQuery
	_ = json.Unmarshal([]byte(comparisonArgs), &q)
	rs := comparisonStatsResult()
	rs.Rows = rs.Rows[:1]
	out, _, err := periodComparisonResult(rs, q)
	if err != nil {
		t.Fatal(err)
	}
	if columnValue(t, out, 0, "Среднее после") != nil || columnValue(t, out, 0, "Изменение, %") != nil {
		t.Fatal("отсутствующую группу нельзя подменять нулевым средним")
	}
	rs = comparisonStatsResult()
	rs.Rows[0][2], rs.Rows[0][3] = int64(2), int64(2)
	out, _, err = periodComparisonResult(rs, q)
	if err != nil {
		t.Fatal(err)
	}
	if columnValue(t, out, 0, "p-значение") != nil || !strings.Contains(columnValue(t, out, 0, "Вывод").(string), "Недостаточно") {
		t.Fatal("малая выборка не должна давать уверенное заключение")
	}
}

func TestComparePeriodsAvailableThroughMCPAndPlanner(t *testing.T) {
	exec := &fakeExecutor{result: comparisonStatsResult()}
	model := newScriptedLLM(`{"intent":"query","comparison":` + comparisonArgs + `}`)
	svc, id, _ := newTestAgent(t, model, exec, &fakeDashboards{})
	addTaskDates(svc)
	answer, err := svc.Ask(context.Background(), id, "Сравни периоды")
	if err != nil || answer.Intent != domain.IntentQuery || answer.Analysis != "period_comparison" {
		t.Fatalf("JSON-план сравнения: %+v, %v", answer, err)
	}
	out, isError, err := svc.CallTool(context.Background(), toolDistributions, []byte(comparisonArgs))
	if err != nil || isError || !strings.Contains(out, "Изменение, %") {
		t.Fatalf("MCP: %s %v %v", out, isError, err)
	}
	for _, invalid := range []string{strings.Replace(comparisonArgs, "started_at", "password", 1), strings.Replace(comparisonArgs, "2026-09-21", "2026-09-05", 1)} {
		_, isError, err := svc.CallTool(context.Background(), toolDistributions, []byte(invalid))
		if err != nil || !isError {
			t.Fatalf("небезопасные параметры должны отклоняться: %v %v", isError, err)
		}
	}
	if len(exec.queries) != 2 {
		t.Fatalf("отклонённые параметры не должны попадать в базу: %d", len(exec.queries))
	}
	for _, tool := range svc.DataTools() {
		if tool.Name == toolDistributions {
			return
		}
	}
	t.Fatal("инструмента нет в MCP tools/list")
}

func TestComparePeriodsRejectsTruncatedResults(t *testing.T) {
	rs := comparisonStatsResult()
	rs.Truncated = true
	svc, _, _ := newTestAgent(t, newToolLLM(), &fakeExecutor{result: rs}, &fakeDashboards{})
	addTaskDates(svc)
	out, isError, err := svc.CallTool(context.Background(), toolDistributions, []byte(comparisonArgs))
	if err != nil || !isError || !strings.Contains(out, "усечённые") {
		t.Fatalf("усечённые группы нельзя сравнивать: %s %v %v", out, isError, err)
	}
}

func TestAnalyticalPolicyInAllPromptModes(t *testing.T) {
	canary := promptguard.NewCanary()
	for _, system := range []string{buildAgentPrompt("каталог", false, canary, "Стали ли задачи выполнять быстрее?"), buildPlannerPrompt("каталог", false, canary), buildPlannerPrompt("каталог", true, canary)} {
		if strings.Count(system, analyticalReasoningPolicy) != 1 || strings.Contains(system, "%!") {
			t.Fatal("политика анализа должна передаваться во всех режимах без ошибок формата")
		}
	}
}

func TestAnalyticalExplanationsAreNotPromptLeaks(t *testing.T) {
	for _, text := range []string{
		"Ожидание — время от создания до начала работы. Выполнение — время от начала работы до завершения.",
		"Фактические трудозатраты не равны календарной длительности задачи.",
		"Отсутствие статистически значимого различия не доказывает равенство групп.",
		"Интервал относится к разнице средних, а не медиан. Наблюдаемая связь не доказывает причинность.",
	} {
		if promptEcho.Repeats(text) {
			t.Errorf("обычное объяснение метрики не должно блокироваться: %s", text)
		}
	}
	if !promptEcho.Repeats(analyticalReasoningPolicy) {
		t.Fatal("дословная утечка аналитических инструкций должна обнаруживаться")
	}
}

func TestComparisonPlanRejectsMixedQuery(t *testing.T) {
	for _, extra := range []string{`,"query":{"dataset":"orders"}`, `,"sql":"SELECT 1"`} {
		if _, err := parsePlan(`{"intent":"query","comparison":` + comparisonArgs + extra + `}`); err == nil {
			t.Fatal("нельзя молча выбрать один из двух несовместимых планов")
		}
	}
}

func TestComparisonHistoryPreservesFullParameters(t *testing.T) {
	var q analytics.PeriodComparisonQuery
	_ = json.Unmarshal([]byte(comparisonArgs), &q)
	q.Filters = []analytics.Filter{{Column: "status", Op: analytics.OpIn, Values: []any{strings.Repeat("длинное значение ", 40)}}}
	raw, err := json.Marshal(map[string]any{"comparison": q})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 600 {
		t.Fatal("тест должен проверять длинный план")
	}
	history := formatHistory(&dataFilter{}, []domain.Turn{{Question: "Сравни периоды", Answer: "Готово", Query: raw}})
	if len(history) != 2 || !strings.Contains(history[1].Content, string(raw)) {
		t.Fatalf("параметры сравнения нельзя обрезать посередине JSON: %+v", history)
	}
}
