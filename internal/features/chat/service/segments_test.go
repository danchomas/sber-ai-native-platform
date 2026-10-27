package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/heinousness/aibi/internal/core/domain"
)

func TestAgentComparesSegments(t *testing.T) {
	result := &domain.ResultSet{
		Columns: []domain.ResultColumn{
			{Name: "Статус", Type: domain.ColumnTypeText},
			{Name: "Записей", Type: domain.ColumnTypeInteger},
			{Name: "Доля, %", Type: domain.ColumnTypeNumeric},
			{Name: "Среднее Сумма", Type: domain.ColumnTypeNumeric},
		},
		Rows:     [][]any{{"paid", int64(700), 70.0, 2000.0}, {"cancelled", int64(300), 30.0, 500.0}},
		RowCount: 2,
	}
	model := newToolLLM(
		calls(call("c1", toolCompare, `{"dataset":"orders","segments":"status","values":["paid","cancelled"]}`)),
		calls(call("c2", toolAnswer, `{"text":"Оплаченные заказы в 4 раза крупнее отменённых.","result_id":"r1"}`)),
	)
	exec := &fakeExecutor{result: result}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Что отличает оплаченные заказы от отменённых?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(exec.queries) != 1 || !strings.Contains(exec.queries[0], `"status" = ANY($1)`) || !strings.Contains(exec.queries[0], `avg(`) {
		t.Fatalf("сравнение должно собрать средние по сегментам с фильтром значений:\n%v", exec.queries)
	}
	if answer.Intent != domain.IntentQuery || len(answer.Query) == 0 {
		t.Fatalf("сравнение сегментов — обычный пересчитываемый результат: %+v", answer)
	}

	var out struct {
		Differences []struct {
			Metric  string         `json:"metric"`
			Highest map[string]any `json:"highest"`
			GapPct  float64        `json:"gap_pct"`
		} `json:"key_differences"`
	}
	if err := json.Unmarshal([]byte(model.toolMessages()[0].Content), &out); err != nil {
		t.Fatalf("ответ инструмента: %v", err)
	}
	if len(out.Differences) == 0 || out.Differences[0].Metric != "Среднее Сумма" || out.Differences[0].GapPct != 300 {
		t.Fatalf("первым должно идти самое сильное отличие: %+v", out.Differences)
	}
	if out.Differences[0].Highest["segment"] != "paid" {
		t.Fatalf("лидер по среднему чеку: %+v", out.Differences[0])
	}
}

func TestAgentWhatIf(t *testing.T) {
	result := &domain.ResultSet{Columns: []domain.ResultColumn{
		{Name: "Месяц", Type: domain.ColumnTypeDate},
		{Name: "Цена", Type: domain.ColumnTypeNumeric},
		{Name: "Продажи", Type: domain.ColumnTypeNumeric},
	}}
	for i := range 8 {
		result.Rows = append(result.Rows, []any{
			time.Date(2024, time.Month(i+1), 1, 0, 0, 0, 0, time.UTC), 100.0 + float64(i), 1000.0 - 5*float64(i),
		})
	}
	result.RowCount = len(result.Rows)

	model := newToolLLM(
		calls(call("c1", toolWhatIf, `{"dataset":"orders","time":{"column":"created_at","grain":"month"},`+
			`"driver":{"fn":"avg","column":"amount","label":"Цена"},"target":{"fn":"count","label":"Продажи"},"change_pct":10}`)),
		calls(call("c2", toolAnswer, `{"text":"При росте цены на 10% продажи снизятся примерно на 5,5%.","result_id":"r1"}`)),
	)
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: result}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Как изменятся продажи, если поднять цену на 10%?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Analysis != analysisWhatIf || len(answer.Warnings) == 0 {
		t.Fatalf("сценарий — расчёт поверх данных с оговоркой о причинности: %+v", answer)
	}
	scenario := answer.Result.Rows[1]
	if scenario[1] != 117.7 || scenario[2] != 911.5 {
		t.Fatalf("сценарий: цена 107→117,7, продажи 965→911,5; получено %v", scenario)
	}

	var out struct {
		Summary map[string]any `json:"summary"`
	}
	if err := json.Unmarshal([]byte(model.toolMessages()[0].Content), &out); err != nil {
		t.Fatalf("ответ инструмента: %v", err)
	}
	if out.Summary["r2"] != 1.0 || out.Summary["target_change_pct"] != -5.54 {
		t.Fatalf("сводка сценария: %+v", out.Summary)
	}
}
