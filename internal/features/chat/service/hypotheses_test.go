package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
)

// toolSummary разбирает summary из вывода первого инструмента.
func toolSummary(t *testing.T, model *toolLLM) map[string]any {
	t.Helper()
	var payload struct {
		Summary map[string]any `json:"summary"`
	}
	msgs := model.toolMessages()
	if len(msgs) == 0 {
		t.Fatal("инструмент не вызывался")
	}
	if err := json.Unmarshal([]byte(msgs[0].Content), &payload); err != nil {
		t.Fatalf("вывод инструмента: %v\n%s", err, msgs[0].Content)
	}
	if payload.Summary == nil {
		t.Fatalf("в выводе нет summary: %s", msgs[0].Content)
	}
	return payload.Summary
}

func confounderCall(args string) domain.Reply {
	return calls(call("c1", toolConfounder, args))
}

const bigSmall = `"group_a":[{"column":"amount","op":"gte","value":1000}],"group_b":[{"column":"amount","op":"lt","value":1000}],` +
	`"label_a":"Крупные","label_b":"Мелкие"`

func TestCheckConfounderExplainedByComposition(t *testing.T) {
	model := newToolLLM(
		confounderCall(`{"dataset":"orders","metric":{"fn":"avg","expr":"closed_at - created_at","label":"Ожидание, ч"},`+
			bigSmall+`,"by":["Статус"]}`),
		calls(call("c2", toolAnswer, `{"text":"Нет: разница объясняется статусом.","result_id":"r1"}`)),
	)
	cols := []string{"Статус", "na", "va", "sa", "nb", "vb", "sb"}
	row := func(v string, na, va, nb, vb float64) []any { return []any{v, na, va, 10.0, nb, vb, 10.0} }
	exec := &fakeExecutor{results: []*domain.ResultSet{
		resultSet(cols[1:], []any{1000.0, 50.0, 10.0, 3000.0, 40.0, 10.0}),
		resultSet(cols, row("paid", 800, 52, 600, 52), row("new", 150, 41, 1200, 41), row("cancelled", 50, 40, 1200, 40)),
	}}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})
	withClosedAt(svc)

	if _, err := svc.Ask(context.Background(), sessionID, "Правда ли, что крупные заказы дольше закрывают, или это из-за статуса?"); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(exec.queries) != 2 {
		t.Fatalf("ожидалось два запроса — в целом и по признаку: %d", len(exec.queries))
	}
	got := toolSummary(t, model)["conclusion"].(string)
	if !strings.HasPrefix(got, "Нет: разница между группами объясняется составом") || !strings.Contains(got, "«Статус»") {
		t.Fatalf("разница внутри статусов исчезает — вывод «объясняется»: %q", got)
	}
}

func TestCheckConfounderPartlyWhenGapIsUneven(t *testing.T) {
	model := newToolLLM(
		confounderCall(`{"dataset":"orders","metric":{"fn":"avg","expr":"closed_at - created_at","label":"Срок, ч"},`+
			bigSmall+`,"by":["Статус"]}`),
		calls(call("c2", toolAnswer, `{"text":"Частично.","result_id":"r1"}`)),
	)
	cols := []string{"Статус", "na", "va", "sa", "nb", "vb", "sb"}
	row := func(v string, na, va, nb, vb float64) []any { return []any{v, na, va, 20.0, nb, vb, 20.0} }
	exec := &fakeExecutor{results: []*domain.ResultSet{
		resultSet(cols[1:], []any{1000.0, 140.0, 20.0, 3000.0, 107.0, 20.0}),
		resultSet(cols, row("paid", 500, 150, 1000, 110), row("new", 300, 152, 1000, 114), row("cancelled", 200, 104, 1000, 104)),
	}}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})
	withClosedAt(svc)

	if _, err := svc.Ask(context.Background(), sessionID, "Правда ли, что крупные заказы дольше закрывают?"); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	summary := toolSummary(t, model)
	got := summary["conclusion"].(string)
	if !strings.HasPrefix(got, "Частично") {
		t.Fatalf("разница есть не во всех статусах — вывод «частично»: %q", got)
	}
	checked := summary["checked"].([]any)[0].(map[string]any)
	if fmt.Sprint(checked["no_gap_in"]) != "[cancelled]" {
		t.Fatalf("статус без разницы должен быть назван: %v", checked)
	}
}

func TestCheckConfounderRateIsWithinGroup(t *testing.T) {
	model := newToolLLM(
		confounderCall(`{"dataset":"orders","metric":{"fn":"share","filters":[{"column":"status","op":"eq","value":"cancelled"}],`+
			`"label":"Отмены, %"},`+bigSmall+`,"by":["Статус"]}`),
		calls(call("c2", toolAnswer, `{"text":"Да.","result_id":"r1"}`)),
	)
	exec := &fakeExecutor{results: []*domain.ResultSet{
		resultSet([]string{"na", "ka", "nb", "kb"}, []any{1000.0, 100.0, 3000.0, 150.0}),
		resultSet([]string{"Статус", "na", "ka", "nb", "kb"},
			[]any{"paid", 500.0, 50.0, 1500.0, 75.0}, []any{"new", 500.0, 50.0, 1500.0, 75.0}),
	}}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	if _, err := svc.Ask(context.Background(), sessionID, "Правда ли, что крупные заказы чаще отменяют?"); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if strings.Contains(exec.queries[0], "100.0 *") {
		t.Fatalf("частота внутри группы считается из двух количеств, а не share по всем строкам: %s", exec.queries[0])
	}
	overall := toolSummary(t, model)["overall"].(map[string]any)
	if overall["Крупные"] != 10.0 || overall["Мелкие"] != 5.0 || overall["gap_pct"] != 100.0 {
		t.Fatalf("частоты групп — 10%% и 5%%: %v", overall)
	}
}

// withNodes добавляет в каталог узел выполнения (4 значения) и окружение
// (2 значения): узел мелче и по оценке не хуже окружения. При равной оценке
// выше в списке тот, чья колонка раньше.
func withNodes(svc *Service, envFirst bool) {
	node := domain.Column{Name: "node", SourceName: "Узел", Type: domain.ColumnTypeText, Role: domain.RoleDimension,
		Profile: &domain.ColumnProfile{RowCount: 1000, DistinctCount: 4}}
	env := domain.Column{Name: "env", SourceName: "Окружение", Type: domain.ColumnTypeText, Role: domain.RoleDimension,
		Profile: &domain.ColumnProfile{RowCount: 1000, DistinctCount: 2}}
	cols := []domain.Column{node, env}
	if envFirst {
		cols = []domain.Column{env, node}
	}
	catalog := svc.catalog.(*fakeCatalog)
	catalog.datasets[0].Columns = append(catalog.datasets[0].Columns, cols...)
}

func TestCheckConfounderPrefersCoarseNestedDimension(t *testing.T) {
	tests := []struct {
		name     string
		nested   []any // число окружений у узла test-2
		envFirst bool
		want     string
	}{
		{"узел вложен в окружение", []any{"test-2", int64(1)}, false, "Окружение"},
		{"узел в двух окружениях", []any{"test-2", int64(2)}, false, "Узел"},
		{"окружение уже выше узла", []any{"test-2", int64(1)}, true, "Окружение"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newToolLLM(
				confounderCall(`{"dataset":"orders","metric":{"fn":"avg","expr":"closed_at - created_at","label":"Ожидание, ч"},`+bigSmall+`}`),
				calls(call("c2", toolAnswer, `{"text":"Нет.","result_id":"r1"}`)),
			)
			row := func(v string, na, va, nb, vb float64) []any { return []any{v, na, va, 10.0, nb, vb, 10.0} }
			cols := func(name string) []string { return []string{name, "na", "va", "sa", "nb", "vb", "sb"} }
			node := resultSet(cols("Узел"), row("prod-1", 500, 52, 500, 52), row("prod-2", 400, 52, 500, 52),
				row("test-1", 50, 32, 1000, 34), row("test-2", 50, 32, 1000, 34))
			env := resultSet(cols("Окружение"), row("prod", 900, 52, 1000, 52), row("test", 100, 32, 2000, 34))
			dims := []*domain.ResultSet{node, env}
			if tt.envFirst {
				dims = []*domain.ResultSet{env, node}
			}
			exec := &fakeExecutor{results: slices.Concat([]*domain.ResultSet{
				resultSet(cols("")[1:], []any{1000.0, 50.0, 10.0, 3000.0, 40.0, 10.0}),
				// Статус: состав групп одинаков — признак ничего не объясняет.
				resultSet(cols("Статус"), row("paid", 700, 50, 2100, 40), row("new", 300, 50, 900, 40)),
			}, dims, []*domain.ResultSet{
				resultSet([]string{"Узел", "Окружений"}, []any{"prod-1", int64(1)}, []any{"prod-2", int64(1)},
					[]any{"test-1", int64(1)}, tt.nested),
			})}
			svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})
			withClosedAt(svc)
			withNodes(svc, tt.envFirst)

			if _, err := svc.Ask(context.Background(), sessionID, "Правда ли, что крупные заказы дольше ждут?"); err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if len(exec.queries) != 5 || !strings.Contains(exec.queries[4], "count(DISTINCT") {
				t.Fatalf("ожидался запрос вложенности узла в окружение пятым: %q", exec.queries)
			}
			summary := toolSummary(t, model)
			first := summary["checked"].([]any)[0].(map[string]any)
			if first["dimension"] != tt.want {
				t.Fatalf("первым должен быть признак %q: %v", tt.want, first)
			}
			if _, has := first["includes"]; has != (tt.want == "Окружение") {
				t.Fatalf("вложенный узел называется только при замене: %v", first)
			}
			for _, item := range summary["checked"].([]any)[1:] {
				if dim := item.(map[string]any)["dimension"]; tt.want == "Окружение" && dim == "Узел" {
					t.Fatalf("узел, вложенный в окружение, не показывается отдельно: %v", summary["checked"])
				}
			}
			if got := summary["conclusion"].(string); !strings.Contains(got, "«"+tt.want+"»") {
				t.Fatalf("вывод должен называть %q: %q", tt.want, got)
			}
		})
	}
}

// withReasons добавляет причину отмены — следствие самой отмены: у неотменённых
// она пустая — и канал, настоящий признак.
func withReasons(svc *Service) {
	catalog := svc.catalog.(*fakeCatalog)
	catalog.datasets[0].Columns = append(catalog.datasets[0].Columns,
		domain.Column{Name: "reason", SourceName: "Причина отмены", Type: domain.ColumnTypeText, Role: domain.RoleDimension,
			Profile: &domain.ColumnProfile{RowCount: 1000, NullCount: 900, DistinctCount: 2}},
		domain.Column{Name: "channel", SourceName: "Канал", Type: domain.ColumnTypeText, Role: domain.RoleDimension,
			Profile: &domain.ColumnProfile{RowCount: 1000, DistinctCount: 2}},
	)
}

// toolPayload разбирает весь вывод первого инструмента.
func toolPayload(t *testing.T, model *toolLLM) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(model.toolMessages()[0].Content), &payload); err != nil {
		t.Fatalf("вывод инструмента: %v", err)
	}
	return payload
}

func TestCheckConfounderSkipsOutcomeProxy(t *testing.T) {
	model := newToolLLM(
		confounderCall(`{"dataset":"orders","metric":{"fn":"share","filters":[{"column":"status","op":"eq","value":"cancelled"}],`+
			`"label":"Отмены, %"},`+bigSmall+`}`),
		calls(call("c2", toolAnswer, `{"text":"Да.","result_id":"r1"}`)),
	)
	cols := func(name string) []string { return []string{name, "na", "ka", "nb", "kb"} }
	exec := &fakeExecutor{results: []*domain.ResultSet{
		resultSet(cols("")[1:], []any{1000.0, 100.0, 3000.0, 150.0}),
		// Причина отмены: отмены целиком в её значениях, остальные — в пустом.
		resultSet(cols("Причина отмены"), []any{nil, 900.0, 0.0, 2850.0, 0.0},
			[]any{"Передумал", 60.0, 60.0, 90.0, 90.0}, []any{"Дорого", 40.0, 40.0, 60.0, 60.0}),
		resultSet(cols("Канал"), []any{"web", 500.0, 50.0, 1500.0, 75.0}, []any{"app", 500.0, 50.0, 1500.0, 75.0}),
	}}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})
	withReasons(svc)

	if _, err := svc.Ask(context.Background(), sessionID, "Правда ли, что крупные заказы чаще отменяют?"); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(exec.queries) != 3 {
		t.Fatalf("статус задаёт событие и не перебирается: ожидалось 3 запроса, получено %d", len(exec.queries))
	}
	payload := toolPayload(t, model)
	summary := payload["summary"].(map[string]any)
	if got := summary["conclusion"].(string); !strings.HasPrefix(got, "Да, разница сохраняется") || !strings.Contains(got, "«Канал»") {
		t.Fatalf("причина отмены не должна «объяснять» разницу: %q", got)
	}
	if fmt.Sprint(payload["excluded"]) == "" || !strings.Contains(fmt.Sprint(payload["excluded"]), "Причина отмены") {
		t.Fatalf("модель должна знать, что причина отмены отброшена: %v", payload["excluded"])
	}
}

func TestCheckConfounderNeedsBothGroups(t *testing.T) {
	model := newToolLLM(
		confounderCall(`{"dataset":"orders","metric":{"fn":"avg","column":"amount"},"group_a":[{"column":"amount","op":"gte","value":1}]}`),
		calls(call("c2", toolAnswer, `{"text":"—"}`)),
	)
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{}, &fakeDashboards{})
	answer, err := svc.Ask(context.Background(), sessionID, "сравни")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(answer.Steps) != 1 || answer.Steps[0].OK || !strings.Contains(answer.Steps[0].Detail, "group_b") {
		t.Fatalf("без второй группы инструмент объясняет, чего не хватает: %+v", answer.Steps)
	}
}

// periodsExecutor отдаёт ответы compare_periods по порядку запросов:
// период, предыдущие дни, год назад, история по неделям, состав.
func periodsExecutor(target, previous, lastYear []any, weekly float64, from time.Time) *fakeExecutor {
	cols := []string{"n", "v", "s", "total"}
	var history [][]any
	start := time.Date(2023, 12, 25, 0, 0, 0, 0, time.UTC)
	for i := 0; !start.AddDate(0, 0, 7).After(from); i++ {
		history = append(history, []any{start.Format(dayLayout), 94.0, weekly + float64(i%3) - 1, 20.0, 100.0})
		start = start.AddDate(0, 0, 7)
	}
	return &fakeExecutor{
		results: []*domain.ResultSet{
			resultSet(cols, target), resultSet(cols, previous), resultSet(cols, lastYear),
			resultSet(append([]string{"Неделя"}, cols...), history...),
		},
		result: resultSet([]string{"Статус", "a", "b"}, []any{"paid", 60.0, 1500.0}, []any{"new", 40.0, 1000.0}),
	}
}

const periodsArgsJSON = `{"dataset":"orders","metric":{"fn":"avg","expr":"closed_at - created_at","label":"Длительность, ч"},` +
	`"date_column":"Дата заказа","from":"2024-06-21","to":"2024-06-30"}`

func TestComparePeriodsSaysIncompleteDurationsAreNotAChange(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolPeriods, periodsArgsJSON)),
		calls(call("c2", toolAnswer, `{"text":"Нельзя утверждать.","result_id":"r1"}`)),
	)
	from := time.Date(2024, 6, 21, 0, 0, 0, 0, time.UTC)
	exec := periodsExecutor([]any{84.0, 35.7, 20.0, 100.0}, []any{94.0, 43.0, 20.0, 100.0}, []any{94.0, 43.8, 20.0, 100.0}, 43, from)
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})
	withClosedAt(svc)

	answer, err := svc.Ask(context.Background(), sessionID, "Можно ли утверждать, что в конце июня заказы стали закрывать быстрее?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	summary := toolSummary(t, model)
	got := summary["conclusion"].(string)
	if !strings.HasPrefix(got, "Нельзя утверждать: длительность известна у 84% записей периода против обычных 94%") {
		t.Fatalf("неполные длительности — главный вывод: %q", got)
	}
	if strings.Contains(got, "Перемена есть") || !strings.Contains(got, "год назад") {
		t.Fatalf("без поправки снижение — след незавершённых, а не перемена; год назад — для сравнения: %q", got)
	}
	if answer.Result == nil || answer.Result.Rows[0][0] != "Проверяемый период" || answer.Analysis != analysisPeriods {
		t.Fatalf("таблица периодов — производный расчёт: %+v", answer.Result)
	}
	if answer.Verdict != "нельзя утверждать" {
		t.Fatalf("вывод одним словом берётся из начала ответа: %q", answer.Verdict)
	}
}

func TestComparePeriodsWithinNoise(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolPeriods, periodsArgsJSON)),
		calls(call("c2", toolAnswer, `{"text":"Нет, перемены нет.","result_id":"r1"}`)),
	)
	from := time.Date(2024, 6, 21, 0, 0, 0, 0, time.UTC)
	exec := periodsExecutor([]any{94.0, 42.5, 20.0, 100.0}, []any{94.0, 43.0, 20.0, 100.0}, []any{94.0, 43.8, 20.0, 100.0}, 43, from)
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})
	withClosedAt(svc)

	if _, err := svc.Ask(context.Background(), sessionID, "Стали ли заказы закрывать быстрее в конце июня?"); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	got := toolSummary(t, model)["conclusion"].(string)
	if !strings.HasPrefix(got, "Перемены нет: значение периода 42,5 в пределах обычных колебаний") {
		t.Fatalf("значение внутри обычного разброса — не перемена: %q", got)
	}
}

func TestComparePeriodsClipsToDataEnd(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolPeriods, `{"dataset":"orders","metric":{"fn":"count"},"date_column":"created_at","from":"2024-06-01","to":"2024-12-31"}`)),
		calls(call("c2", toolAnswer, `{"text":"Готово.","result_id":"r1"}`)),
	)
	exec := &fakeExecutor{result: resultSet([]string{"n", "v"}, []any{300.0, 300.0})}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	if _, err := svc.Ask(context.Background(), sessionID, "Стало ли заказов больше в июне?"); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	summary := toolSummary(t, model)
	if summary["period"] != "01.06.2024–30.06.2024" {
		t.Fatalf("конец периода обрезается концом данных 30.06.2024: %v", summary["period"])
	}
	if !strings.Contains(fmt.Sprint(summary["notes"]), "обрезан концом данных") {
		t.Fatalf("об обрезке нужно сказать: %v", summary)
	}
	if summary["value"] != 10.0 {
		t.Fatalf("количество сравнивается в день: 300 за 30 дней — 10: %v", summary["value"])
	}
}

func TestReviewAnswerAsksForRateAndVerdict(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolAggregate, `{"dataset":"orders","group_by":["status"],"metrics":[{"fn":"count"}]}`)),
		calls(call("c2", toolAnswer, `{"text":"Крупных отмен больше всего.","result_id":"r1"}`)),
		calls(call("c3", toolAggregate, `{"dataset":"orders","group_by":["status"],"metrics":[{"fn":"share","filters":[{"column":"amount","op":"gte","value":1000}]}]}`)),
		calls(call("c4", toolAnswer, `{"text":"Нет, крупные заказы отменяют не чаще.","result_id":"r2"}`)),
	)
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Правда ли, что крупные заказы чаще отменяют?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Text != "Нет, крупные заказы отменяют не чаще." || answer.Verdict != "нет" {
		t.Fatalf("после возврата принимается исправленный ответ: %q, вывод %q", answer.Text, answer.Verdict)
	}
	rejected := model.requests[2].Messages
	last := rejected[len(rejected)-1].Content
	if !strings.Contains(last, "сравни частоту") || !strings.Contains(last, "начни текст ответа с вывода") {
		t.Fatalf("модель должна узнать обе причины возврата: %s", last)
	}
}

func TestReviewAnswerReturnsOnlyOnce(t *testing.T) {
	model := newToolLLM(
		calls(call("c1", toolAggregate, `{"dataset":"orders","group_by":["status"],"metrics":[{"fn":"count"}]}`)),
		calls(call("c2", toolAnswer, `{"text":"Крупных отмен больше.","result_id":"r1"}`)),
		calls(call("c3", toolAnswer, `{"text":"Крупных отмен всё равно больше.","result_id":"r1"}`)),
	)
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Правда ли, что крупные заказы чаще отменяют?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Text != "Крупных отмен всё равно больше." {
		t.Fatalf("второй раз ответ не возвращается: %q", answer.Text)
	}
}

// failingLLM перестаёт отвечать после заданного числа шагов агента.
type failingLLM struct {
	*toolLLM
	after int
}

func (l *failingLLM) Converse(ctx context.Context, req domain.Completion) (domain.Reply, error) {
	if l.conversations() >= l.after {
		l.mu.Lock()
		l.requests = append(l.requests, req)
		l.mu.Unlock()
		return domain.Reply{}, fmt.Errorf("%w: unexpected EOF", apperr.ErrLLMUnavailable)
	}
	return l.toolLLM.Converse(ctx, req)
}

func TestAgentNarratesResultWhenModelDropsMidway(t *testing.T) {
	model := &failingLLM{toolLLM: newToolLLM(
		calls(call("c1", toolAggregate, `{"dataset":"orders","group_by":["status"],"metrics":[{"fn":"count"}]}`)),
	), after: 1}
	model.narration = "Больше всего оплаченных заказов — 700."
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Сколько заказов в каждом статусе?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Text != model.narration {
		t.Fatalf("при обрыве ответ излагается отдельным вызовом: %q", answer.Text)
	}
	if got := model.conversations(); got != 3 {
		t.Fatalf("шаг после обрыва повторяется один раз: обращений %d, ожидалось 3", got)
	}
	if len(answer.Warnings) == 0 {
		t.Fatal("об обрыве нужно предупредить")
	}
}

func TestFallbackResultTextIsReadable(t *testing.T) {
	rs := resultSet([]string{"Срок, ч", "Крупных заявок"}, []any{140.05, int64(22460)})
	if got := fallbackResultText(rs); got != "Результат расчёта: Срок, ч — 140,05; Крупных заявок — 22 460." {
		t.Fatalf("текст без модели — числа читаемой фразой: %q", got)
	}
}

func TestQuestionPlan(t *testing.T) {
	cases := []struct {
		question string
		want     []string
		claim    bool
	}{
		{"При каких условиях крупные задачи дольше ждут начала работы?", []string{"Вопрос об условиях"}, false},
		{"Можно ли утверждать, что в конце сентября 2026 года команды стали выполнять задачи быстрее?", []string{"перемене во времени"}, true},
		{"Правда ли, что крупные заявки на закупку дольше согласуют, или это объясняется категорией закупки?", []string{"Проверка утверждения"}, true},
		{"Правда ли, что фоновые задания с большим объёмом записей чаще падают с ошибкой?", []string{"Проверка утверждения"}, true},
		{"Какие рекламные площадки окупаются лучше всего, и не объясняется ли разница составом аудитории?", []string{"Проверка утверждения"}, false},
		{"Можно ли утверждать, что в последние месяцы поддержка стала быстрее давать первый ответ клиенту?", []string{"перемене во времени"}, true},
		{"Покажи выручку по месяцам", nil, false},
	}
	for _, tc := range cases {
		got := questionPlan(tc.question)
		if len(tc.want) == 0 && got != "" {
			t.Errorf("%q: плана быть не должно, получено %q", tc.question, got)
		}
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%q: в плане нет %q", tc.question, w)
			}
		}
		if isClaim(tc.question) != tc.claim {
			t.Errorf("%q: вопрос-проверка %v, ожидалось %v", tc.question, !tc.claim, tc.claim)
		}
	}
}

func TestTemplatePhrasesAreNotPromptLeaks(t *testing.T) {
	ok := []string{
		"Нет, утверждать нельзя. Последний период неполный, в сравнение его не беру; по полным неделям 43–47 ч.",
		"Сильнее всего — город. По остальным признакам разница в пределах случайного разброса.",
		"Да. Это связь в данных; возможно и обратное — большие объёмы получают скидку.",
	}
	for _, text := range ok {
		if promptEcho.Repeats(text) {
			t.Errorf("готовая формулировка из промпта — не утечка: %q", text)
		}
	}
	leak := "Вопрос о перемене во времени: compare_periods, from и to — проверяемый период из вопроса; " +
		"если перемена привязана к событию, а не к датам, сравни группы до и после через aggregate по признаку события"
	if !promptEcho.Repeats(leak) {
		t.Error("пересказ самого плана по-прежнему считается утечкой")
	}
}

func TestDriverRowGapKeepsSignWithNegativeBaseline(t *testing.T) {
	tests := []struct {
		name string
		a, b float64
		want float64
	}{
		{"обе положительные", 140, 107, 140.0/107 - 1},
		{"Б отрицательная, А выше", 74.11, -48, (74.11 + 48) / 48},
		{"обе отрицательные, А выше", -10, -20, 0.5},
		{"А ниже", 50, 100, -0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := driverRow([]any{"x", 100.0, tt.a, 200.0, tt.b}, 1, true, false)
			if !st.defined {
				t.Fatal("разница не посчитана")
			}
			if math.Abs(st.value-tt.want) > 1e-9 {
				t.Fatalf("разница %v, ожидалось %v", st.value, tt.want)
			}
			if (st.value > 0) != (tt.a > tt.b) {
				t.Fatalf("знак разницы %v не совпадает с тем, что А %v, а Б %v", st.value, tt.a, tt.b)
			}
		})
	}
}

func TestRelChange(t *testing.T) {
	tests := []struct{ a, base, want float64 }{
		{110, 100, 0.1},
		{90, 100, -0.1},
		{-24, -48, 0.5},
		{-72, -48, -0.5},
		{74.11, -48, (74.11 + 48) / 48},
	}
	for _, tt := range tests {
		if got := relChange(tt.a, tt.base); math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("relChange(%v, %v) = %v, ожидалось %v", tt.a, tt.base, got, tt.want)
		}
	}
}

func TestRateSpreadHint(t *testing.T) {
	// Срыв срока у перевозчиков: разница 0,2 п.п. при сотнях тысяч доставок.
	carriers := []rateGroup{
		{name: "Курьерская служба", n: 168070, k: 14370},
		{name: "Региональная служба", n: 84512, k: 7200},
		{name: "Почтовая доставка", n: 46162, k: 3859},
	}
	got := rateSpreadHint("Срыв срока, %", carriers)
	if !strings.Contains(got, "не различаются") || strings.Contains(got, "выше:") {
		t.Fatalf("перевозчики не различаются, получено: %s", got)
	}

	methods := []rateGroup{
		{name: "Курьером", n: 150000, k: 13230},
		{name: "Пункт выдачи", n: 150000, k: 12510},
		{name: "Постамат", n: 100000, k: 8070},
	}
	got = rateSpreadHint("Срыв срока, %", methods)
	if !strings.Contains(got, "выше: «Курьером»") || !strings.Contains(got, "ниже: «Постамат»") {
		t.Fatalf("курьер выше, постамат ниже, получено: %s", got)
	}
	if strings.Contains(got, "Пункт выдачи") {
		t.Fatalf("пункт выдачи в пределах разброса, но выделен: %s", got)
	}

	if got := rateSpreadHint("x", carriers[:1]); got != "" {
		t.Fatalf("одна группа — сравнивать не с чем, получено: %s", got)
	}
}
