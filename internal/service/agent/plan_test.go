package agent

import (
	"testing"

	"github.com/heinousness/aibi/internal/domain"
)

func TestParsePlanQuery(t *testing.T) {
	raw := "```json\n" + `{
		"intent": "query",
		"sql": "SELECT status, count(*) FROM raw.ds_orders GROUP BY status;",
		"chart": {"type": "bar", "x": "status", "y": "count"},
		"title": "Заказы по статусам",
		"followups": ["Показать динамику по месяцам"]
	}` + "\n```"

	p, err := parsePlan(raw)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if p.intent() != domain.IntentQuery {
		t.Fatalf("сценарий определён как %s", p.intent())
	}
	if p.SQL != "SELECT status, count(*) FROM raw.ds_orders GROUP BY status" {
		t.Fatalf("SQL не очищен: %q", p.SQL)
	}
	spec := p.Chart.chartSpec()
	if spec.Type != domain.ChartTypeBar || len(spec.Y) != 1 || spec.Y[0] != "count" {
		t.Fatalf("визуализация разобрана неверно: %+v", spec)
	}
	if len(p.Followups) != 1 {
		t.Fatalf("уточняющие вопросы: %v", p.Followups)
	}
}

func TestParsePlanIntentFallbacks(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want domain.AnswerIntent
	}{
		{"без intent, но с sql", `{"sql":"SELECT 1 FROM raw.t"}`, domain.IntentQuery},
		{"intent query без sql", `{"intent":"query"}`, domain.IntentClarify},
		{"описание данных", `{"intent":"overview"}`, domain.IntentOverview},
		{"отказ", `{"intent":"reject","message":"нет таких данных"}`, domain.IntentReject},
		{"уточнение", `{"intent":"clarify","message":"что именно?"}`, domain.IntentClarify},
		{"дашборд", `{"intent":"dashboard","widgets":[{"sql":"SELECT 1 FROM raw.t"}]}`, domain.IntentDashboard},
		{"дашборд без виджетов но с sql", `{"intent":"dashboard","sql":"SELECT 1 FROM raw.t"}`, domain.IntentQuery},
		{"неизвестный intent", `{"intent":"что-то","sql":"SELECT 1 FROM raw.t"}`, domain.IntentQuery},
		{"пустой план", `{}`, domain.IntentClarify},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := parsePlan(tc.raw)
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if got := p.intent(); got != tc.want {
				t.Fatalf("получено %s, ожидалось %s", got, tc.want)
			}
		})
	}
}

func TestParsePlanWithReasoningAround(t *testing.T) {
	raw := `Думаю, нужен такой запрос.
{"intent":"query","sql":"SELECT 1 FROM raw.t"}
Готово.`
	p, err := parsePlan(raw)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if p.SQL != "SELECT 1 FROM raw.t" {
		t.Fatalf("SQL: %q", p.SQL)
	}
}

func TestParsePlanInvalid(t *testing.T) {
	if _, err := parsePlan("модель ничего не вернула"); err == nil {
		t.Fatal("ожидалась ошибка разбора")
	}
}

func TestStringListAcceptsNull(t *testing.T) {
	p, err := parsePlan(`{"intent":"query","sql":"SELECT 1 FROM raw.t","chart":{"type":"kpi","y":null},"followups":null}`)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(p.Chart.chartSpec().Y) != 0 || len(p.Followups) != 0 {
		t.Fatal("null должен давать пустой список")
	}
}

func TestCleanSQL(t *testing.T) {
	cases := map[string]string{
		"```sql\nSELECT 1\n```": "SELECT 1",
		"SELECT 1;":             "SELECT 1",
		"  SELECT 1  ":          "SELECT 1",
		"":                      "",
	}
	for in, want := range cases {
		if got := cleanSQL(in); got != want {
			t.Errorf("cleanSQL(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}
