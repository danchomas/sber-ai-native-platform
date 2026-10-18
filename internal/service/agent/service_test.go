package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testQueryConfig() config.Query {
	return config.Query{MaxRows: 1000, PreviewRows: 20, MaxRepairs: 2, HistoryDepth: 8}
}

func sampleResult() *domain.ResultSet {
	return &domain.ResultSet{
		Columns: []domain.ResultColumn{
			{Name: "status", Type: domain.ColumnTypeText},
			{Name: "Количество", Type: domain.ColumnTypeInteger},
		},
		Rows:     [][]any{{"paid", 700}, {"new", 200}, {"cancelled", 100}},
		RowCount: 3,
	}
}

func newTestAgent(t *testing.T, model domain.LLM, exec Executor, dashboards Dashboards) (*Service, uuid.UUID, *memorySessions) {
	t.Helper()

	sessions := newMemorySessions()
	session := &domain.Session{ID: uuid.New(), Title: "тест"}
	if err := sessions.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("создание сессии: %v", err)
	}

	svc := New(newFakeCatalog(), exec, sessions, dashboards, model, testQueryConfig(), testLogger())
	return svc, session.ID, sessions
}

func TestAskQueryHappyPath(t *testing.T) {
	model := newScriptedLLM(`{"intent":"query","sql":"SELECT status, count(*) AS \"Количество\" FROM raw.ds_orders GROUP BY status","chart":{"type":"bar","x":"status","y":["Количество"]},"title":"Заказы по статусам","followups":["Покажи динамику"]}`)
	model.narration = "Больше всего заказов в статусе paid — 700 из 1000."

	exec := &fakeExecutor{result: sampleResult()}
	svc, sessionID, sessions := newTestAgent(t, model, exec, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Сколько заказов в каждом статусе?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	if answer.Intent != domain.IntentQuery {
		t.Fatalf("сценарий: %s", answer.Intent)
	}
	if answer.Text != model.narration {
		t.Fatalf("текст ответа: %q", answer.Text)
	}
	if answer.Chart == nil || answer.Chart.Type != domain.ChartTypeBar {
		t.Fatalf("визуализация: %+v", answer.Chart)
	}
	if answer.Result == nil || answer.Result.RowCount != 3 {
		t.Fatalf("результат: %+v", answer.Result)
	}
	if len(answer.Datasets) != 1 || answer.Datasets[0] != "orders" {
		t.Fatalf("датасеты в ответе: %v", answer.Datasets)
	}
	if len(answer.Followups) != 1 {
		t.Fatalf("уточняющие вопросы: %v", answer.Followups)
	}

	msgs, _ := sessions.ListMessages(context.Background(), sessionID, 10)
	if len(msgs) != 2 || msgs[0].Role != domain.RoleUser || msgs[1].Role != domain.RoleAssistant {
		t.Fatalf("история диалога записана неверно: %d сообщений", len(msgs))
	}
}

func TestAskRepairsUnsafeSQL(t *testing.T) {
	model := newScriptedLLM(`{"intent":"query","sql":"DELETE FROM raw.ds_orders"}`)
	model.repair = []string{`{"sql":"SELECT status, count(*) AS \"Количество\" FROM raw.ds_orders GROUP BY status"}`}
	model.narration = "Готово."

	exec := &fakeExecutor{result: sampleResult()}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Разбивка по статусам")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Intent != domain.IntentQuery {
		t.Fatalf("сценарий: %s, предупреждения: %v", answer.Intent, answer.Warnings)
	}
	if model.callCount("repair") != 1 {
		t.Fatalf("ожидалась одна попытка исправления, было %d", model.callCount("repair"))
	}
	if strings.Contains(answer.SQL, "DELETE") {
		t.Fatalf("небезопасный SQL попал в ответ: %s", answer.SQL)
	}
}

func TestAskRepairsExecutionError(t *testing.T) {
	model := newScriptedLLM(`{"intent":"query","sql":"SELECT wrong_column FROM raw.ds_orders"}`)
	model.repair = []string{`{"sql":"SELECT status FROM raw.ds_orders"}`}
	model.narration = "Готово."

	exec := &fakeExecutor{result: sampleResult(), failOnce: errors.New(`ERROR: column "wrong_column" does not exist`)}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Статусы")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Intent != domain.IntentQuery {
		t.Fatalf("ошибка выполнения не исправлена: %s / %v", answer.Intent, answer.Warnings)
	}
}

func TestAskGivesUpAfterRepairs(t *testing.T) {
	model := newScriptedLLM(`{"intent":"query","sql":"DROP TABLE raw.ds_orders"}`)
	model.repair = []string{
		`{"sql":"DROP TABLE raw.ds_orders"}`,
		`{"sql":"TRUNCATE raw.ds_orders"}`,
	}

	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "удали всё")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Intent != domain.IntentClarify {
		t.Fatalf("ожидался переход к уточнению, получено %s", answer.Intent)
	}
	if answer.SQL != "" {
		t.Fatalf("SQL не должен попадать в ответ: %q", answer.SQL)
	}
}

func TestAskQueryTimeoutExplained(t *testing.T) {
	model := newScriptedLLM(`{"intent":"query","sql":"SELECT * FROM raw.ds_orders"}`)
	exec := &fakeExecutor{err: apperr.ErrQueryTimeout}
	svc, sessionID, _ := newTestAgent(t, model, exec, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "выгрузи всё")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Intent != domain.IntentClarify {
		t.Fatalf("сценарий: %s", answer.Intent)
	}
	if !strings.Contains(answer.Text, "по времени") {
		t.Fatalf("пользователю не объяснили таймаут: %q", answer.Text)
	}
}

func TestAskOverviewUsesModelText(t *testing.T) {
	model := newScriptedLLM(`{"intent":"overview"}`)
	model.overview = "Это данные о заказах за 2023–2024 годы."

	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Что передо мной?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Intent != domain.IntentOverview {
		t.Fatalf("сценарий: %s", answer.Intent)
	}
	if answer.Text != model.overview {
		t.Fatalf("текст: %q", answer.Text)
	}
	if answer.Result == nil || answer.Result.RowCount != 1 {
		t.Fatalf("сводная таблица каталога не приложена: %+v", answer.Result)
	}
}

func TestAskFallsBackWhenModelUnavailable(t *testing.T) {
	model := newScriptedLLM()
	model.err = apperr.ErrLLMUnavailable

	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "Что это за данные?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Intent != domain.IntentOverview {
		t.Fatalf("без модели ожидалось описание данных, получено %s", answer.Intent)
	}
	if !strings.Contains(answer.Text, "orders") {
		t.Fatalf("описание не опирается на каталог: %q", answer.Text)
	}
	if len(answer.Warnings) == 0 {
		t.Fatal("пользователя не предупредили о недоступности модели")
	}
}

func TestAskBuildsDashboard(t *testing.T) {
	model := newScriptedLLM(`{"intent":"dashboard","title":"Квартальная статистика","widgets":[
		{"title":"Заказы по статусам","sql":"SELECT status, count(*) AS \"Количество\" FROM raw.ds_orders GROUP BY status","chart":{"type":"bar","x":"status","y":["Количество"]}},
		{"title":"Сломанный виджет","sql":"DROP TABLE raw.ds_orders"}
	]}`)

	dashboards := &fakeDashboards{}
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, dashboards)

	answer, err := svc.Ask(context.Background(), sessionID, "Собери дашборд с квартальной статистикой")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Intent != domain.IntentDashboard {
		t.Fatalf("сценарий: %s", answer.Intent)
	}
	if answer.DashboardID == nil {
		t.Fatal("дашборд не сохранён")
	}
	if len(dashboards.created) != 1 || len(dashboards.created[0].Widgets) != 1 {
		t.Fatalf("небезопасный виджет не отброшен: %+v", dashboards.created)
	}
	if len(answer.Warnings) == 0 {
		t.Fatal("пользователю не сообщили о неудавшемся виджете")
	}
}

func TestAskRejectsAndClarifies(t *testing.T) {
	cases := []struct {
		name   string
		plan   string
		intent domain.AnswerIntent
	}{
		{"отказ", `{"intent":"reject","message":"В данных нет информации о погоде."}`, domain.IntentReject},
		{"уточнение", `{"intent":"clarify","message":"Какой период вас интересует?"}`, domain.IntentClarify},
		{"о платформе", `{"intent":"smalltalk","message":"Я анализирую загруженные данные."}`, domain.IntentSmalltalk},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := newScriptedLLM(tc.plan)
			svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{}, &fakeDashboards{})

			answer, err := svc.Ask(context.Background(), sessionID, "вопрос")
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if answer.Intent != tc.intent {
				t.Fatalf("сценарий: %s, ожидался %s", answer.Intent, tc.intent)
			}
			if answer.Text == "" {
				t.Fatal("пустой текст ответа")
			}
		})
	}
}

func TestAskWithoutDatasets(t *testing.T) {
	catalog := newFakeCatalog()
	catalog.datasets = nil

	sessions := newMemorySessions()
	session := &domain.Session{ID: uuid.New(), Title: "тест"}
	_ = sessions.CreateSession(context.Background(), session)

	svc := New(catalog, &fakeExecutor{}, sessions, &fakeDashboards{},
		newScriptedLLM(), testQueryConfig(), testLogger())

	answer, err := svc.Ask(context.Background(), session.ID, "что в данных?")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Intent != domain.IntentReject || !strings.Contains(answer.Text, "CSV") {
		t.Fatalf("ожидалась подсказка о загрузке данных: %+v", answer)
	}
}

func TestAskValidatesQuestion(t *testing.T) {
	svc, sessionID, _ := newTestAgent(t, newScriptedLLM(), &fakeExecutor{}, &fakeDashboards{})

	if _, err := svc.Ask(context.Background(), sessionID, "   "); !errors.Is(err, apperr.ErrValidation) {
		t.Fatalf("пустой вопрос должен отклоняться: %v", err)
	}
	if _, err := svc.Ask(context.Background(), uuid.New(), "вопрос"); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("неизвестная сессия должна отклоняться: %v", err)
	}
}

func TestNarrationFallback(t *testing.T) {
	model := newScriptedLLM(`{"intent":"query","sql":"SELECT status, count(*) AS \"Количество\" FROM raw.ds_orders GROUP BY status"}`)
	svc, sessionID, _ := newTestAgent(t, model, &fakeExecutor{result: sampleResult()}, &fakeDashboards{})

	answer, err := svc.Ask(context.Background(), sessionID, "статусы")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if answer.Text == "" {
		t.Fatal("ответ без текста")
	}
	if !strings.Contains(answer.Text, "строк") {
		t.Fatalf("детерминированное изложение не сработало: %q", answer.Text)
	}
}
