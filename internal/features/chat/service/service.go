package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/chart"
	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/llm"
	"github.com/heinousness/aibi/internal/core/sqlguard"
)

const resultRowsForModel = 30

type Catalog interface {
	List(ctx context.Context) ([]domain.Dataset, error)
	Relations(ctx context.Context) ([]domain.Relation, error)

	Scopes(ctx context.Context) ([]domain.Scope, error)
	Schema() string
}

type Models interface {
	Current(ctx context.Context) domain.LLM
}

type Executor interface {
	Query(ctx context.Context, target domain.QueryTarget, sql string, maxRows int, args ...any) (*domain.ResultSet, error)
}

type Dashboards interface {
	CreateFromWidgets(ctx context.Context, sessionID *uuid.UUID, title, description string, widgets []domain.Widget) (*domain.Dashboard, error)
}

type Service struct {
	catalog    Catalog
	exec       Executor
	sessions   SessionRepository
	dashboards Dashboards
	models     Models
	cfg        config.Query
	agent      config.Agent
	log        *slog.Logger

	// noTools помнит модели, которые отказали в function calling, чтобы
	// не тратить на них лишний запрос перед каждым вопросом.
	noTools sync.Map
}

func New(
	catalog Catalog,
	exec Executor,
	sessions SessionRepository,
	dashboards Dashboards,
	models Models,
	cfg config.Query,
	agent config.Agent,
	log *slog.Logger,
) *Service {
	return &Service{
		catalog: catalog, exec: exec, sessions: sessions,
		dashboards: dashboards, models: models, cfg: cfg, agent: agent, log: log,
	}
}

func (s *Service) Ask(ctx context.Context, sessionID uuid.UUID, question string) (*domain.Answer, error) {
	started := time.Now()

	question = strings.TrimSpace(question)
	if question == "" {
		return nil, apperr.NewValidationError("question", "вопрос не может быть пустым")
	}
	if len([]rune(question)) > 2000 {
		return nil, apperr.NewValidationError("question", "вопрос слишком длинный")
	}
	session, err := s.sessions.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	s.ensureTitle(ctx, session, question)

	if err := s.sessions.AddMessage(ctx, &domain.Message{
		ID: uuid.New(), SessionID: sessionID, Role: domain.RoleUser, Content: question,
	}); err != nil {
		return nil, err
	}

	answer := s.answer(ctx, sessionID, question)
	answer.Elapsed = time.Since(started).Milliseconds()

	if err := s.sessions.AddMessage(ctx, &domain.Message{
		ID: uuid.New(), SessionID: sessionID, Role: domain.RoleAssistant,
		Content: answer.Text, Answer: answer,
	}); err != nil {
		s.log.ErrorContext(ctx, "ответ не сохранён в истории", "session", sessionID, "error", err)
	}

	s.log.InfoContext(ctx, "вопрос обработан",
		"session", sessionID, "intent", answer.Intent,
		"elapsed_ms", answer.Elapsed, "rows", resultRows(answer.Result))
	return answer, nil
}

func (s *Service) answer(ctx context.Context, sessionID uuid.UUID, question string) *domain.Answer {
	datasets, err := s.catalog.List(ctx)
	if err != nil {
		return failure("Не удалось прочитать каталог данных. Попробуйте повторить вопрос.", err)
	}
	if !hasReady(datasets) {
		return &domain.Answer{
			Intent: domain.IntentReject,
			Text: "Пока не загружено ни одного набора данных. " +
				"Загрузите CSV-файл на вкладке «Данные» — после этого я смогу отвечать на вопросы по нему.",
		}
	}

	relations, err := s.catalog.Relations(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "связи каталога недоступны", "error", err)
	}
	turns, err := s.sessions.LastTurns(ctx, sessionID, s.cfg.HistoryDepth)
	if err != nil {
		s.log.WarnContext(ctx, "история диалога недоступна", "error", err)
	}

	model := s.models.Current(ctx)
	if caller, ok := model.(ToolCaller); ok {
		if _, unsupported := s.noTools.Load(model.Model()); !unsupported {
			answer, err := s.runAgent(ctx, caller, sessionID, question, datasets, relations, turns)
			if err == nil {
				return answer
			}
			if !errors.Is(err, llm.ErrToolsUnsupported) {
				s.log.WarnContext(ctx, "цикл агента не запущен", "error", err)
				answer := s.overviewAnswer(ctx, datasets, relations, question)
				answer.Warnings = append(answer.Warnings,
					"Языковая модель недоступна, поэтому показано описание данных по профилю каталога.")
				return answer
			}
			s.noTools.Store(model.Model(), struct{}{})
			s.log.WarnContext(ctx, "модель не поддерживает инструменты, агент отвечает по плану в JSON",
				"model", model.Model())
		}
	}

	schemaContext := s.planContext(datasets, relations)
	plan, err := s.plan(ctx, schemaContext, turns, question)
	if err != nil {

		s.log.WarnContext(ctx, "планирование не выполнено", "error", err)
		answer := s.overviewAnswer(ctx, datasets, relations, question)
		answer.Warnings = append(answer.Warnings,
			"Языковая модель недоступна, поэтому показано описание данных по профилю каталога.")
		return answer
	}

	switch plan.intent() {
	case domain.IntentOverview:
		return s.overviewAnswer(ctx, datasets, relations, question)

	case domain.IntentQuery:
		return s.queryAnswer(ctx, schemaContext, datasets, relations, question, plan)

	case domain.IntentDashboard:
		return s.dashboardAnswer(ctx, sessionID, schemaContext, datasets, relations, question, plan)

	case domain.IntentReject:
		text := plan.Message
		if text == "" {
			text = "На этот вопрос нельзя ответить по загруженным данным. " +
				"Уточните, какой показатель и за какой период вас интересует."
		}
		return &domain.Answer{Intent: domain.IntentReject, Text: text, Followups: plan.Followups}

	case domain.IntentSmalltalk:
		text := plan.Message
		if text == "" {
			text = "Я отвечаю на вопросы по загруженным данным: описываю их состав, считаю показатели, " +
				"строю графики и собираю дашборды. Спросите, например, «что есть в этих данных»."
		}
		return &domain.Answer{Intent: domain.IntentSmalltalk, Text: text, Followups: plan.Followups}

	default:
		text := plan.Message
		if text == "" {
			text = "Уточните вопрос: какой показатель нужно посчитать, за какой период и в каком разрезе?"
		}
		return &domain.Answer{Intent: domain.IntentClarify, Text: text, Followups: plan.Followups}
	}
}

func (s *Service) plan(ctx context.Context, schemaContext string, turns []domain.Turn, question string) (*plan, error) {
	messages := formatHistory(turns)
	messages = append(messages, domain.ChatMessage{Role: domain.RoleUser, Content: question})

	raw, err := s.models.Current(ctx).Complete(ctx, domain.Completion{
		System:   buildPlannerPrompt(schemaContext, s.agent.AllowRawSQL),
		Messages: messages,
		JSONOnly: true,
	})
	if err != nil {
		return nil, err
	}

	p, err := parsePlan(raw)
	if err == nil {
		return p, nil
	}

	s.log.DebugContext(ctx, "план не разобран, повторная попытка", "error", err)
	messages = append(messages, domain.ChatMessage{
		Role:    domain.RoleUser,
		Content: "Верни ответ строго одним JSON-объектом по описанному формату, без текста вокруг.",
	})
	raw, err = s.models.Current(ctx).Complete(ctx, domain.Completion{
		System:   buildPlannerPrompt(schemaContext, s.agent.AllowRawSQL),
		Messages: messages,
		JSONOnly: true,
	})
	if err != nil {
		return nil, err
	}
	return parsePlan(raw)
}

func (s *Service) overviewAnswer(
	ctx context.Context,
	datasets []domain.Dataset,
	relations []domain.Relation,
	question string,
) *domain.Answer {
	answer := &domain.Answer{
		Intent: domain.IntentOverview,
		Result: overviewResultSet(datasets),
		Chart:  &domain.ChartSpec{Type: domain.ChartTypeTable, Title: "Загруженные данные"},
		Followups: []string{
			"Какие значения встречаются чаще всего?",
			"Покажи динамику по месяцам",
			"Есть ли в данных дубликаты?",
		},
	}
	for _, d := range datasets {
		if d.Ready() {
			answer.Datasets = append(answer.Datasets, d.Name)
		}
	}

	facts := buildOverviewFacts(s.catalog.Schema(), datasets, relations)
	text, err := s.models.Current(ctx).Complete(ctx, domain.Completion{
		System: overviewSystemPrompt,
		Messages: []domain.ChatMessage{{
			Role:    domain.RoleUser,
			Content: "ВОПРОС: " + question + "\n\nФАКТЫ О ДАННЫХ:\n" + facts,
		}},
	})
	if err != nil || strings.TrimSpace(text) == "" {
		answer.Text = fallbackOverviewText(datasets, relations)
		return answer
	}
	answer.Text = strings.TrimSpace(text)
	return answer
}

func (s *Service) queryAnswer(
	ctx context.Context,
	schemaContext string,
	datasets []domain.Dataset,
	relations []domain.Relation,
	question string,
	p *plan,
) *domain.Answer {
	scopes, err := s.catalog.Scopes(ctx)
	if err != nil {
		return failure("Не удалось подготовить проверку запроса. Повторите вопрос.", err)
	}

	execution, err := s.runQuery(ctx, schemaContext, question,
		candidate{sql: p.SQL, query: p.Query}, scopes, s.analyticsCatalog(datasets, relations))
	if err != nil {
		if errors.Is(err, apperr.ErrQueryTimeout) {
			return &domain.Answer{
				Intent: domain.IntentClarify,
				Text: "Запрос оказался слишком тяжёлым и был остановлен по времени. " +
					"Сузьте вопрос: укажите период, конкретный разрез или ограничьте число категорий.",
			}
		}
		return &domain.Answer{
			Intent: domain.IntentClarify,
			Text: "Не получилось корректно посчитать ответ на этот вопрос по загруженным данным. " +
				"Попробуйте переформулировать: назовите показатель, период и разрез. " +
				"Причина: " + humanReason(err),
		}
	}

	title := strings.TrimSpace(p.Title)
	if title == "" {
		title = question
	}
	spec := chart.Normalize(p.Chart.chartSpec(), execution.result, title)

	answer := &domain.Answer{
		Intent:    domain.IntentQuery,
		SQL:       execution.sql,
		Query:     execution.queryJSON(),
		Result:    execution.result,
		Chart:     &spec,
		Datasets:  datasetNames(datasets, execution.tables),
		Warnings:  execution.warnings,
		Followups: p.Followups,
	}
	answer.Text = s.narrate(ctx, question, title, execution.result, execution.warnings)
	return answer
}

func (s *Service) dashboardAnswer(
	ctx context.Context,
	sessionID uuid.UUID,
	schemaContext string,
	datasets []domain.Dataset,
	relations []domain.Relation,
	question string,
	p *plan,
) *domain.Answer {
	scopes, err := s.catalog.Scopes(ctx)
	if err != nil {
		return failure("Не удалось подготовить проверку запросов дашборда.", err)
	}
	catalog := s.analyticsCatalog(datasets, relations)

	var (
		widgets  []domain.Widget
		warnings []string
		used     []string
	)

	for i, w := range p.Widgets {
		if strings.TrimSpace(w.SQL) == "" && w.Query == nil {
			continue
		}
		execution, err := s.runQuery(ctx, schemaContext, orDefault(w.Question, question),
			candidate{sql: w.SQL, query: w.Query}, scopes, catalog)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Виджет «%s» не построен: %s",
				orDefault(w.Title, fmt.Sprintf("№%d", i+1)), humanReason(err)))
			continue
		}

		title := orDefault(w.Title, orDefault(w.Question, fmt.Sprintf("Показатель %d", i+1)))
		spec := chart.Normalize(w.Chart.chartSpec(), execution.result, title)
		width := w.Width
		if width <= 0 || width > 12 {
			width = defaultWidth(spec.Type)
		}

		widgets = append(widgets, domain.Widget{
			Title:    title,
			Question: w.Question,
			SQL:      execution.sql,
			Query:    execution.queryJSON(),
			Chart:    spec,
			Width:    width,
			Result:   execution.result,
		})
		used = append(used, execution.tables...)
	}

	if len(widgets) == 0 {
		return &domain.Answer{
			Intent: domain.IntentClarify,
			Text: "Не удалось собрать дашборд по этому запросу. " +
				"Уточните, какие показатели и разрезы должны быть на панели.",
			Warnings: warnings,
		}
	}

	title := orDefault(p.Title, "Дашборд по запросу")
	dashboard, err := s.dashboards.CreateFromWidgets(ctx, &sessionID, title, question, widgets)
	if err != nil {
		return failure("Дашборд построен, но не сохранён. Повторите запрос.", err)
	}

	answer := &domain.Answer{
		Intent:      domain.IntentDashboard,
		DashboardID: &dashboard.ID,
		Datasets:    datasetNames(datasets, used),
		Warnings:    warnings,
		Followups:   p.Followups,
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Собрал дашборд «%s» из %s. Он сохранён и доступен на вкладке «Дашборды».\n",
		title, pluralWidgets(len(widgets)))
	for _, w := range widgets {
		fmt.Fprintf(&sb, "• %s\n", w.Title)
	}
	answer.Text = strings.TrimSpace(sb.String())
	return answer
}

type execution struct {
	sql      string
	query    *analytics.Query
	result   *domain.ResultSet
	tables   []string
	scope    domain.Scope
	warnings []string
}

func (e *execution) queryJSON() json.RawMessage {
	if e.query == nil {
		return nil
	}
	out, err := json.Marshal(e.query)
	if err != nil {
		return nil
	}
	return out
}

// candidate — то, что предложила модель: параметры запроса или, если
// разрешён свободный SQL, текст запроса.
type candidate struct {
	sql   string
	query *analytics.Query
}

func (c candidate) String() string {
	if c.query != nil {
		out, _ := json.Marshal(c.query)
		return string(out)
	}
	return c.sql
}

func (s *Service) planContext(datasets []domain.Dataset, relations []domain.Relation) string {
	if s.agent.AllowRawSQL {
		return buildSchemaContext(s.catalog.Schema(), datasets, relations)
	}
	return buildToolContext(datasets, relations)
}

func (s *Service) analyticsCatalog(datasets []domain.Dataset, relations []domain.Relation) analytics.Catalog {
	return analytics.Catalog{Datasets: datasets, Relations: relations, DefaultSchema: s.catalog.Schema()}
}

// compile превращает предложение модели в SQL с параметрами. Свободный
// SQL принимается, только если он явно разрешён.
func (s *Service) compile(c candidate, catalog analytics.Catalog) (string, []any, []string, error) {
	if c.query != nil {
		compiled, err := analytics.Compile(*c.query, catalog, s.cfg.MaxRows)
		if err != nil {
			return "", nil, nil, err
		}
		return compiled.SQL, compiled.Args, compiled.Warnings, nil
	}
	if strings.TrimSpace(c.sql) == "" {
		return "", nil, nil, errors.New("в плане нет параметров запроса query")
	}
	if !s.agent.AllowRawSQL {
		return "", nil, nil, fmt.Errorf("%w: SQL от модели не принимается, опиши запрос параметрами query",
			apperr.ErrUnsafeQuery)
	}
	return c.sql, nil, nil, nil
}

func (s *Service) runQuery(
	ctx context.Context,
	schemaContext, question string,
	c candidate,
	scopes []domain.Scope,
	catalog analytics.Catalog,
) (*execution, error) {
	var lastErr error

	for attempt := 0; attempt <= s.cfg.MaxRepairs; attempt++ {
		if attempt > 0 {
			repaired, err := s.repair(ctx, schemaContext, question, c, lastErr)
			if err != nil {
				return nil, lastErr
			}
			c = repaired
			s.log.DebugContext(ctx, "запрос переписан после ошибки", "attempt", attempt)
		}

		sql, args, compileWarnings, err := s.compile(c, catalog)
		if err != nil {
			lastErr = err
			continue
		}

		scope, checked, err := sqlguard.ResolveScope(sql, scopes, s.catalog.Schema(), s.cfg.MaxRows)
		if err != nil {
			lastErr = err
			continue
		}

		result, err := s.exec.Query(ctx, scope.Target, checked.SQL, s.cfg.MaxRows, args...)
		if err != nil {
			if errors.Is(err, apperr.ErrQueryTimeout) || ctx.Err() != nil {
				return nil, err
			}
			lastErr = err
			continue
		}

		warnings := append(compileWarnings, checked.Warnings...)
		if result.Truncated {
			warnings = append(warnings,
				fmt.Sprintf("Показаны первые %d строк результата.", len(result.Rows)))
		}
		return &execution{
			sql: sql, query: c.query, result: result, tables: checked.Tables,
			scope: scope, warnings: warnings,
		}, nil
	}
	return nil, lastErr
}

func (s *Service) repair(ctx context.Context, schemaContext, question string, bad candidate, cause error) (candidate, error) {
	system := repairSystemPrompt
	if !s.agent.AllowRawSQL {
		system = repairQuerySystemPrompt
	}
	raw, err := s.models.Current(ctx).Complete(ctx, domain.Completion{
		System: system,
		Messages: []domain.ChatMessage{{
			Role:    domain.RoleUser,
			Content: buildRepairPrompt(schemaContext, question, bad.String(), humanReason(cause)),
		}},
		JSONOnly: true,
	})
	if err != nil {
		return candidate{}, err
	}

	p, err := parsePlan(raw)
	if err != nil {
		return candidate{}, err
	}
	if p.SQL == "" && p.Query == nil {
		return candidate{}, errors.New("модель не предложила исправленный запрос")
	}
	return candidate{sql: p.SQL, query: p.Query}, nil
}

func (s *Service) narrate(ctx context.Context, question, title string, rs *domain.ResultSet, warnings []string) string {
	digest := formatResultForModel(rs, resultRowsForModel)

	text, err := s.models.Current(ctx).Complete(ctx, domain.Completion{
		System: narratorSystemPrompt,
		Messages: []domain.ChatMessage{{
			Role:    domain.RoleUser,
			Content: buildNarrationPrompt(question, title, digest, warnings),
		}},
	})
	if err != nil || strings.TrimSpace(text) == "" {
		if err != nil {
			s.log.WarnContext(ctx, "изложение результата не выполнено", "error", err)
		}
		return fallbackResultText(rs)
	}
	return strings.TrimSpace(text)
}

func fallbackResultText(rs *domain.ResultSet) string {
	if rs == nil || len(rs.Rows) == 0 {
		return "По этому вопросу в данных ничего не нашлось. Попробуйте расширить период или снять фильтры."
	}
	if len(rs.Rows) == 1 && len(rs.Columns) == 1 {
		return fmt.Sprintf("%s: %s.", rs.Columns[0].Name, formatCell(rs.Rows[0][0]))
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Получено %d строк по %d показателям.", len(rs.Rows), len(rs.Columns))
	if len(rs.Rows) > 0 {
		cells := make([]string, 0, len(rs.Columns))
		for i, c := range rs.Columns {
			if i < len(rs.Rows[0]) {
				cells = append(cells, fmt.Sprintf("%s — %s", c.Name, formatCell(rs.Rows[0][i])))
			}
		}
		fmt.Fprintf(&sb, " Первая строка: %s.", strings.Join(cells, ", "))
	}
	sb.WriteString(" Результат показан в таблице и на графике.")
	return sb.String()
}

func humanReason(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if i := strings.Index(msg, "ERROR:"); i >= 0 {
		msg = strings.TrimSpace(msg[i+len("ERROR:"):])
	}
	msg = strings.TrimPrefix(msg, apperr.ErrUnsafeQuery.Error()+": ")
	msg = strings.TrimPrefix(msg, apperr.ErrValidation.Error()+": ")
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return msg
}

func failure(text string, err error) *domain.Answer {
	return &domain.Answer{
		Intent:   domain.IntentReject,
		Text:     text,
		Warnings: []string{humanReason(err)},
	}
}

func hasReady(datasets []domain.Dataset) bool {
	for _, d := range datasets {
		if d.Ready() {
			return true
		}
	}
	return false
}

func resultRows(rs *domain.ResultSet) int {
	if rs == nil {
		return 0
	}
	return len(rs.Rows)
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func defaultWidth(t domain.ChartType) int {
	switch t {
	case domain.ChartTypeKPI:
		return 3
	case domain.ChartTypeLine, domain.ChartTypeArea, domain.ChartTypeHeatmap, domain.ChartTypeTable:
		return 12
	default:
		return 6
	}
}

func pluralWidgets(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return fmt.Sprintf("%d блока", n)
	default:
		return fmt.Sprintf("%d блоков", n)
	}
}
