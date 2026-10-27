package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/chart"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/llm"
	"github.com/heinousness/aibi/internal/core/promptguard"
	"github.com/heinousness/aibi/internal/core/sqlguard"
)

// ToolCaller — модель, умеющая разговор с инструментами. Объявлен здесь,
// а не в domain.LLM: резервная модель и модели без function calling его
// не реализуют, и агент тогда отвечает через план в JSON-режиме.
type ToolCaller interface {
	Converse(ctx context.Context, req domain.Completion) (domain.Reply, error)
}

const (
	maxCallsPerStep   = 8
	maxResultsPerRun  = 16
	maxWidgets        = 8
	maxCellRunes      = 80
	maxToolOutputRows = resultRowsForModel
	// maxArgsBytes — предел аргументов одного вызова. Честному вызову
	// хватает пары килобайт; больше — признак мусора или атаки.
	maxArgsBytes = 16 << 10
	// maxRepeats — сколько одинаковых вызовов терпит цикл, прежде чем
	// остановиться: малые модели склонны зацикливаться.
	maxRepeats = 3
	// maxFailStreak — сколько ошибок подряд терпит цикл.
	maxFailStreak = 4
)

type agentResult struct {
	id       string
	title    string
	query    *analytics.Query
	sql      string
	result   *domain.ResultSet
	tables   []string
	warnings []string
	analysis string
	chart    *domain.ChartSpec
}

type agentFinal struct {
	intent    domain.AnswerIntent
	text      string
	resultID  string
	chart     *chartJSON
	followups []string
}

type agentRun struct {
	s         *Service
	sessionID uuid.UUID
	question  string
	datasets  []domain.Dataset
	relations []domain.Relation
	catalog   analytics.Catalog
	scopes    []domain.Scope
	known     map[string]bool
	filter    *dataFilter
	progress  func(domain.AgentStep)

	// seen — результаты уже выполненных вызовов по ключу «имя+аргументы».
	seen       map[string]string
	repeats    int
	failStreak int

	results   []*agentResult
	steps     []domain.AgentStep
	warnings  []string
	described bool
	dashboard *domain.Dashboard
	edited    bool
	final     *agentFinal
}

// runAgent ведёт цикл: модель вызывает инструменты, видит их результат и
// решает, что делать дальше, пока не вызовет answer, clarify или decline.
// Ошибку возвращает, только если модель не ответила на первом же шаге —
// тогда вызывающий выбирает запасной путь. Дальше любая проблема
// превращается в ответ с предупреждением, как и во всём агенте.
func (s *Service) runAgent(
	ctx context.Context,
	caller ToolCaller,
	in agentInput,
) (*domain.Answer, error) {
	scopes, err := s.catalog.Scopes(ctx)
	if err != nil {
		return s.failure(ctx, "Не удалось подготовить проверку запросов. Повторите вопрос.", err), nil
	}

	tools := agentTools(s.agent.AllowRawSQL, accessFor(in.question))
	r := s.newRun(in.sessionID, in.question, in.datasets, in.relations, scopes, tools)
	r.filter, r.progress = in.filter, in.progress

	messages := formatHistory(in.filter, in.turns)
	messages = append(messages, domain.ChatMessage{Role: domain.RoleUser, Content: in.question})
	system := buildAgentPrompt(buildToolContext(in.filter, in.datasets, in.relations), s.agent.AllowRawSQL, in.canary)

	// Обязательный вызов инструмента — на первом шаге и после напоминания:
	// без него модели, особенно на длинной истории, «отвечают» текстом
	// и придумывают числа. Если сервер параметр не понимает, модель
	// запоминается и дальше спрашивается без него.
	_, choiceOff := s.noToolChoice.Load(in.model)
	nudged, force := false, !choiceOff
	for step := 0; step < s.agent.MaxSteps; step++ {
		req := domain.Completion{System: system, Messages: messages, Tools: tools, RequireTool: force}
		force = false
		reply, err := caller.Converse(ctx, req)
		if err != nil && req.RequireTool && errors.Is(err, llm.ErrToolChoiceUnsupported) {
			s.noToolChoice.Store(in.model, struct{}{})
			choiceOff = true
			s.log.InfoContext(ctx, "сервер не принимает tool_choice, агент спрашивает без него", "model", in.model)
			req.RequireTool = false
			reply, err = caller.Converse(ctx, req)
		}
		if err != nil {
			if step == 0 {
				return nil, err
			}
			s.log.WarnContext(ctx, "модель перестала отвечать в цикле агента", "step", step, "error", err)
			r.warnings = append(r.warnings, "Языковая модель перестала отвечать, поэтому показан последний полученный результат.")
			return r.finish(""), nil
		}

		calls := reply.ToolCalls
		if len(calls) == 0 {
			calls = llm.ToolCallsFromText(reply.Content, func(name string) bool { return r.known[name] })
		}
		if len(calls) == 0 {
			// Ответ текстом, когда ничего не посчитано, — частая привычка
			// моделей: они «отвечают» по описанию каталога и придумывают
			// числа. Один раз просим опереться на инструменты.
			if !nudged && len(r.results) == 0 && !r.described && step+1 < s.agent.MaxSteps {
				nudged, force = true, !choiceOff
				messages = append(messages,
					domain.ChatMessage{Role: domain.RoleAssistant, Content: reply.Content},
					domain.ChatMessage{Role: domain.RoleUser, Content: groundingNudge},
				)
				continue
			}
			return r.finish(strings.TrimSpace(reply.Content)), nil
		}
		if len(calls) > maxCallsPerStep {
			calls = calls[:maxCallsPerStep]
		}

		messages = append(messages, domain.ChatMessage{
			Role: domain.RoleAssistant, Content: reply.Content, ToolCalls: calls,
		})
		for _, call := range calls {
			output := r.dispatch(ctx, call)
			messages = append(messages, domain.ChatMessage{
				Role: domain.RoleTool, ToolCallID: call.ID, Content: output,
			})
			if r.final != nil {
				return r.finish(""), nil
			}
		}

		switch {
		case r.repeats >= maxRepeats:
			s.log.InfoContext(ctx, "агент остановлен: модель повторяет одни и те же вызовы", "step", step)
			r.warnings = append(r.warnings,
				"Модель начала повторять одни и те же действия, поэтому показан последний полученный результат.")
			return r.finish(""), nil
		case r.failStreak >= maxFailStreak:
			s.log.InfoContext(ctx, "агент остановлен: ошибки инструментов подряд", "step", step)
			return r.finish(""), nil
		case step == s.agent.MaxSteps-2:
			// Последний шаг модель должна потратить на ответ, а не на новый расчёт.
			messages = append(messages, domain.ChatMessage{
				Role:    domain.RoleUser,
				Content: "Остался один шаг. Заверши работу вызовом answer по уже полученным результатам или clarify, если их нет.",
			})
		}
	}

	r.warnings = append(r.warnings,
		"Анализ не уложился в отведённое число шагов, поэтому показан последний полученный результат.")
	return r.finish(""), nil
}

const groundingNudge = "Ответ не принят: факты и числа о данных можно брать только из результатов инструментов. " +
	"Если вопрос о данных — вызови нужный инструмент (describe_dataset, aggregate и другие) и закончи вызовом answer. " +
	"Если это приветствие или вопрос о самой платформе — вызови answer с тем же текстом."

// agentInput — всё, что агенту нужно знать о вопросе.
type agentInput struct {
	sessionID uuid.UUID
	question  string
	datasets  []domain.Dataset
	relations []domain.Relation
	turns     []domain.Turn
	canary    promptguard.Canary
	filter    *dataFilter
	progress  func(domain.AgentStep)
	// model — имя модели, чтобы помнить её возможности между вопросами.
	model string
}

func (s *Service) newRun(
	sessionID uuid.UUID,
	question string,
	datasets []domain.Dataset,
	relations []domain.Relation,
	scopes []domain.Scope,
	tools []domain.ToolSpec,
) *agentRun {
	r := &agentRun{
		s: s, sessionID: sessionID, question: question,
		datasets: datasets, relations: relations, scopes: scopes,
		catalog: analytics.Catalog{Datasets: datasets, Relations: relations, DefaultSchema: s.catalog.Schema()},
		known:   make(map[string]bool, len(tools)),
		seen:    map[string]string{},
	}
	for _, t := range tools {
		r.known[t.Name] = true
	}
	return r
}

func (r *agentRun) dispatch(ctx context.Context, call domain.ToolCall) string {
	started := time.Now()
	step := domain.AgentStep{Tool: call.Name, Title: stepTitle(call.Name)}
	final := call.Name == toolAnswer || call.Name == toolClarify || call.Name == toolDecline

	key := callKey(call)
	if previous, ok := r.seen[key]; ok && !final {
		r.repeats++
		if mutating(call.Name) {
			return encodeOutput(map[string]any{"error": "это действие уже выполнено, повторять его не нужно: заверши работу через answer"})
		}
		return `{"repeat":true,"note":"этот вызов уже выполнялся, его результат ниже: не повторяй его, используй результат или вызови answer","previous":` +
			previous + `}`
	}

	output, rows, err := r.invoke(ctx, call)

	step.Elapsed = time.Since(started).Milliseconds()
	step.Rows = rows
	if err != nil {
		r.failStreak++
		step.Detail = humanReason(err)
		r.s.log.DebugContext(ctx, "инструмент вернул ошибку", "tool", call.Name, "error", err)
		output = encodeOutput(map[string]any{
			"error": step.Detail,
			"hint":  "исправь параметры и вызови инструмент снова или выбери другой инструмент",
		})
	} else {
		r.failStreak = 0
		step.OK = true
		if !final {
			r.seen[key] = output
		}
	}
	if !final {
		r.steps = append(r.steps, step)
		if r.progress != nil {
			r.progress(step)
		}
	}
	return output
}

// callKey — ключ вызова для поиска повторов. Аргументы приводятся к
// каноническому JSON, чтобы порядок полей и пробелы не делали повтор новым.
func callKey(call domain.ToolCall) string {
	var v any
	if err := json.Unmarshal(call.Arguments, &v); err != nil {
		return call.Name + " " + string(call.Arguments)
	}
	canonical, err := json.Marshal(v)
	if err != nil {
		return call.Name + " " + string(call.Arguments)
	}
	return call.Name + " " + string(canonical)
}

func (r *agentRun) invoke(ctx context.Context, call domain.ToolCall) (string, int, error) {
	// Инструмент, которого нет в наборе этого вопроса, не выполняется, даже
	// если модель знает его имя: набор определяет платформа, а не модель.
	if !r.known[call.Name] {
		if call.Name == toolSQL {
			return "", 0, errors.New("инструмент run_sql отключён: используй aggregate")
		}
		if mutating(call.Name) {
			return "", 0, fmt.Errorf("инструмент %s недоступен: пользователь не просил менять дашборды в этом вопросе", call.Name)
		}
		return "", 0, fmt.Errorf("инструмента %q нет; доступны только перечисленные в описании", call.Name)
	}
	if len(call.Arguments) > maxArgsBytes {
		return "", 0, fmt.Errorf("аргументы слишком большие: %d байт, предел %d", len(call.Arguments), maxArgsBytes)
	}
	switch call.Name {
	case toolDescribe:
		var args struct {
			Dataset string `json:"dataset"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return "", 0, err
		}
		return r.describe(args.Dataset)

	case toolAggregate:
		var args struct {
			analytics.Query
			Title string `json:"title"`
		}
		if err := decodeArgs(normalizeArgs(call.Arguments, "metrics", "group_by", "filters", "join"), &args); err != nil {
			return "", 0, err
		}
		compiled, err := analytics.Compile(args.Query, r.catalog, r.s.cfg.MaxRows)
		if err != nil {
			return "", 0, err
		}
		q := args.Query
		return r.run(ctx, compiled, &q, orDefault(args.Title, r.question))

	case toolRows:
		var args analytics.RowsQuery
		if err := decodeArgs(normalizeArgs(call.Arguments, "columns", "filters"), &args); err != nil {
			return "", 0, err
		}
		compiled, err := analytics.CompileRows(args, r.catalog)
		if err != nil {
			return "", 0, err
		}
		return r.run(ctx, compiled, nil, "Записи «"+args.Dataset+"»")

	case toolDuplicates:
		var args struct {
			Dataset string   `json:"dataset"`
			Columns []string `json:"columns"`
		}
		if err := decodeArgs(normalizeArgs(call.Arguments, "columns"), &args); err != nil {
			return "", 0, err
		}
		return r.duplicates(ctx, args.Dataset, args.Columns)

	case toolSQL:
		var args struct {
			SQL   string `json:"sql"`
			Title string `json:"title"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return "", 0, err
		}
		return r.run(ctx, &analytics.Compiled{SQL: cleanSQL(args.SQL)}, nil, orDefault(args.Title, r.question))

	case toolDashboard:
		return r.createDashboard(ctx, call.Arguments)
	case toolSeasonality, toolCorrelate, toolForecast, toolAnomalies:
		return r.insight(ctx, call)
	case toolCompare:
		if len(r.results) >= maxResultsPerRun {
			return "", 0, errors.New("достигнут предел запросов на один вопрос: подведи итог через answer")
		}
		return r.compareSegments(ctx, call.Arguments)
	case toolWhatIf:
		if len(r.results) >= maxResultsPerRun {
			return "", 0, errors.New("достигнут предел запросов на один вопрос: подведи итог через answer")
		}
		return r.whatIf(ctx, call.Arguments)
	case toolDashboards:
		return r.listDashboards(ctx)
	case toolAddWidgets:
		return r.addWidgets(ctx, call.Arguments)
	case toolRemove:
		return r.removeWidget(ctx, call.Arguments)

	case toolAnswer:
		var args struct {
			Text      string     `json:"text"`
			ResultID  string     `json:"result_id"`
			Chart     *chartJSON `json:"chart"`
			Followups stringList `json:"followups"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return "", 0, err
		}
		if id := strings.TrimSpace(args.ResultID); id != "" && len(r.results) > 0 && r.result(id) == nil {
			return "", 0, fmt.Errorf("результата %q нет; доступны: %s", id, strings.Join(r.resultIDs(), ", "))
		}
		r.final = &agentFinal{
			intent: domain.IntentQuery, text: args.Text, resultID: args.ResultID,
			chart: args.Chart, followups: args.Followups,
		}
		return `{"ok":true}`, 0, nil

	case toolClarify, toolDecline:
		var args struct {
			Message     string     `json:"message"`
			Options     stringList `json:"options"`
			Suggestions stringList `json:"suggestions"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return "", 0, err
		}
		intent := domain.IntentClarify
		followups := args.Options
		if call.Name == toolDecline {
			intent, followups = domain.IntentReject, args.Suggestions
		}
		r.final = &agentFinal{intent: intent, text: args.Message, followups: followups}
		return `{"ok":true}`, 0, nil
	}
	return "", 0, fmt.Errorf("инструмента %q нет; доступны только перечисленные в описании", call.Name)
}

func (r *agentRun) resultIDs() []string {
	ids := make([]string, len(r.results))
	for i, res := range r.results {
		ids[i] = res.id
	}
	return ids
}

// run выполняет собранный запрос. Даже SQL, собранный компилятором,
// проходит sqlguard: второй слой не доверяет первому.
func (r *agentRun) run(ctx context.Context, c *analytics.Compiled, q *analytics.Query, title string) (string, int, error) {
	if len(r.results) >= maxResultsPerRun {
		return "", 0, errors.New("достигнут предел запросов на один вопрос: подведи итог через answer")
	}

	scope, checked, err := sqlguard.ResolveScope(c.SQL, r.scopes, r.s.catalog.Schema(), r.s.cfg.MaxRows)
	if err != nil {
		return "", 0, err
	}
	rs, err := r.s.exec.Query(ctx, scope.Target, checked.SQL, r.s.cfg.MaxRows, c.Args...)
	if err != nil {
		return "", 0, err
	}

	warnings := append(append([]string{}, c.Warnings...), checked.Warnings...)
	if rs.Truncated {
		warnings = append(warnings, fmt.Sprintf("Показаны первые %d строк результата.", len(rs.Rows)))
	}
	res := &agentResult{
		id: "r" + strconv.Itoa(len(r.results)+1), title: title, query: q,
		sql: c.SQL, result: rs, tables: checked.Tables, warnings: warnings,
	}
	r.results = append(r.results, res)
	return encodeOutput(r.resultOutput(res)), len(rs.Rows), nil
}

func (r *agentRun) duplicates(ctx context.Context, dataset string, columns []string) (string, int, error) {
	summary, examples, err := analytics.CompileDuplicates(dataset, columns, r.catalog)
	if err != nil {
		return "", 0, err
	}
	scope, checked, err := sqlguard.ResolveScope(summary.SQL, r.scopes, r.s.catalog.Schema(), r.s.cfg.MaxRows)
	if err != nil {
		return "", 0, err
	}
	totals, err := r.s.exec.Query(ctx, scope.Target, checked.SQL, 1, summary.Args...)
	if err != nil {
		return "", 0, err
	}

	out, rows, err := r.run(ctx, examples, nil, "Повторяющиеся записи «"+dataset+"»")
	if err != nil {
		return "", 0, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		return out, rows, nil
	}
	payload["summary"] = r.tableRows(totals)
	return encodeOutput(payload), rows, nil
}

type widgetArgs struct {
	ResultID string     `json:"result_id"`
	Title    string     `json:"title"`
	Chart    *chartJSON `json:"chart"`
	Width    int        `json:"width"`
}

// buildWidgets превращает ссылки на результаты в виджеты. Виджет хранит
// параметры запроса, а не выгрузку: при открытии панели цифры пересчитаются.
func (r *agentRun) buildWidgets(args []widgetArgs) ([]domain.Widget, []string) {
	if len(args) > maxWidgets {
		args = args[:maxWidgets]
	}
	widgets := make([]domain.Widget, 0, len(args))
	var problems []string
	for _, w := range args {
		res := r.result(w.ResultID)
		if res == nil || w.ResultID == "" {
			problems = append(problems, fmt.Sprintf("результата %q нет", w.ResultID))
			continue
		}
		if res.analysis != "" {
			problems = append(problems, fmt.Sprintf(
				"%s — расчёт поверх данных, его не пересчитать на дашборде: закрепи исходный ряд через aggregate", w.ResultID))
			continue
		}
		title := orDefault(w.Title, res.title)
		spec := chart.Normalize(w.Chart.chartSpec(), res.result, title)
		width := w.Width
		if width <= 0 || width > 12 {
			width = defaultWidth(spec.Type)
		}
		widget := domain.Widget{
			Title: title, Question: res.title, SQL: res.sql,
			Chart: spec, Width: width, Result: res.result,
		}
		if res.query != nil {
			widget.Query, _ = json.Marshal(res.query)
		}
		widgets = append(widgets, widget)
	}
	return widgets, problems
}

func (r *agentRun) createDashboard(ctx context.Context, raw json.RawMessage) (string, int, error) {
	if r.dashboard != nil {
		return "", 0, errors.New("дашборд по этому вопросу уже создан: заверши работу через answer")
	}
	var args struct {
		Title       string       `json:"title"`
		Description string       `json:"description"`
		Widgets     []widgetArgs `json:"widgets"`
	}
	if err := decodeArgs(normalizeArgs(raw, "widgets"), &args); err != nil {
		return "", 0, err
	}
	if len(args.Widgets) == 0 {
		return "", 0, errors.New("нужен хотя бы один виджет: сначала посчитай показатели через aggregate")
	}

	widgets, problems := r.buildWidgets(args.Widgets)
	if len(widgets) == 0 {
		return "", 0, fmt.Errorf("ни один виджет не собран: %s", strings.Join(problems, "; "))
	}

	title := orDefault(args.Title, "Дашборд по запросу")
	dashboard, err := r.s.dashboards.CreateFromWidgets(ctx, &r.sessionID, title,
		orDefault(args.Description, r.question), widgets)
	if err != nil {
		return "", 0, err
	}
	r.dashboard = dashboard

	out := map[string]any{"dashboard_id": dashboard.ID, "widgets": len(widgets)}
	if len(problems) > 0 {
		out["skipped"] = problems
	}
	return encodeOutput(out), len(widgets), nil
}

func (r *agentRun) listDashboards(ctx context.Context) (string, int, error) {
	dashboards, err := r.s.dashboards.List(ctx, 20)
	if err != nil {
		return "", 0, err
	}
	out := make([]map[string]any, 0, len(dashboards))
	for _, d := range dashboards {
		widgets := make([]map[string]any, 0, len(d.Widgets))
		for _, w := range d.Widgets {
			widgets = append(widgets, map[string]any{
				"widget_id": w.ID, "title": r.filter.clean(w.Title, maxNameRunes), "chart": w.Chart.Type,
			})
		}
		out = append(out, map[string]any{
			"dashboard_id": d.ID, "title": r.filter.clean(d.Title, maxNameRunes), "widgets": widgets,
		})
	}
	if len(out) == 0 {
		return encodeOutput(map[string]any{"dashboards": out, "note": "дашбордов пока нет, создай новый через create_dashboard"}), 0, nil
	}
	return encodeOutput(map[string]any{"dashboards": out}), len(out), nil
}

func (r *agentRun) dashboardByID(ctx context.Context, raw string) (*domain.Dashboard, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, errors.New("dashboard_id должен быть id из list_dashboards")
	}
	dashboards, err := r.s.dashboards.List(ctx, 50)
	if err != nil {
		return nil, err
	}
	for i := range dashboards {
		if dashboards[i].ID == id {
			return &dashboards[i], nil
		}
	}
	return nil, fmt.Errorf("дашборда %s нет: проверь list_dashboards", id)
}

func (r *agentRun) addWidgets(ctx context.Context, raw json.RawMessage) (string, int, error) {
	var args struct {
		DashboardID string       `json:"dashboard_id"`
		Widgets     []widgetArgs `json:"widgets"`
	}
	if err := decodeArgs(normalizeArgs(raw, "widgets"), &args); err != nil {
		return "", 0, err
	}
	dashboard, err := r.dashboardByID(ctx, args.DashboardID)
	if err != nil {
		return "", 0, err
	}
	widgets, problems := r.buildWidgets(args.Widgets)
	if len(widgets) == 0 {
		return "", 0, fmt.Errorf("нечего добавить: %s", strings.Join(append(problems, "посчитай показатель через aggregate"), "; "))
	}

	added := 0
	for _, w := range widgets {
		if _, err := r.s.dashboards.AddWidget(ctx, dashboard.ID, w); err != nil {
			problems = append(problems, fmt.Sprintf("«%s»: %s", w.Title, humanReason(err)))
			continue
		}
		added++
	}
	if added == 0 {
		return "", 0, fmt.Errorf("виджеты не добавлены: %s", strings.Join(problems, "; "))
	}
	r.dashboard, r.edited = dashboard, true

	out := map[string]any{"dashboard_id": dashboard.ID, "added": added}
	if len(problems) > 0 {
		out["skipped"] = problems
	}
	return encodeOutput(out), added, nil
}

func (r *agentRun) removeWidget(ctx context.Context, raw json.RawMessage) (string, int, error) {
	var args struct {
		DashboardID string `json:"dashboard_id"`
		WidgetID    string `json:"widget_id"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", 0, err
	}
	dashboard, err := r.dashboardByID(ctx, args.DashboardID)
	if err != nil {
		return "", 0, err
	}
	widgetID, err := uuid.Parse(strings.TrimSpace(args.WidgetID))
	if err != nil {
		return "", 0, errors.New("widget_id должен быть id из list_dashboards")
	}
	if err := r.s.dashboards.RemoveWidget(ctx, dashboard.ID, widgetID); err != nil {
		return "", 0, err
	}
	r.dashboard, r.edited = dashboard, true
	return `{"ok":true}`, 1, nil
}

func (r *agentRun) result(id string) *agentResult {
	id = strings.TrimSpace(id)
	for _, res := range r.results {
		if res.id == id {
			return res
		}
	}
	if id == "" && len(r.results) > 0 {
		return r.results[len(r.results)-1]
	}
	return nil
}

func (r *agentRun) finish(content string) *domain.Answer {
	f := r.final
	if f == nil {
		f = &agentFinal{intent: domain.IntentQuery, text: content}
	}

	answer := &domain.Answer{Steps: r.steps, Warnings: r.warnings, Followups: f.followups}
	switch {
	case f.intent == domain.IntentClarify || f.intent == domain.IntentReject:
		answer.Intent = f.intent
		answer.Text = orDefault(f.text, "Уточните вопрос: какой показатель нужно посчитать, за какой период и в каком разрезе?")
		return answer

	case r.dashboard != nil:
		answer.Intent = domain.IntentDashboard
		answer.DashboardID = &r.dashboard.ID
		answer.Datasets = r.usedDatasets(r.results)
		summary := dashboardSummary(r.dashboard)
		if r.edited {
			summary = "Обновил дашборд «" + r.dashboard.Title + "». Изменения уже на вкладке «Дашборды»."
		}
		answer.Text = orDefault(f.text, summary)
		return answer
	}

	if res := r.result(f.resultID); res != nil {
		title := orDefault(res.title, r.question)
		proposed := f.chart.chartSpec()
		if proposed == nil {
			proposed = res.chart
		}
		spec := chart.Normalize(proposed, res.result, title)
		answer.Intent = domain.IntentQuery
		answer.SQL = res.sql
		answer.Result = res.result
		answer.Chart = &spec
		answer.ChartOptions = chart.Options(spec, res.result)
		if res.analysis == "" {
			answer.Headline = chart.Headline(spec, res.result)
		}
		answer.Datasets = r.usedDatasets([]*agentResult{res})
		answer.Warnings = append(answer.Warnings, res.warnings...)
		answer.Analysis = res.analysis
		if res.query != nil {
			answer.Query, _ = json.Marshal(res.query)
		}
		answer.Text = orDefault(f.text, fallbackResultText(res.result))
		return answer
	}

	if r.described {
		answer.Intent = domain.IntentOverview
		answer.Result = overviewResultSet(r.datasets)
		answer.Chart = &domain.ChartSpec{Type: domain.ChartTypeTable, Title: "Загруженные данные"}
		for _, d := range r.datasets {
			answer.Datasets = append(answer.Datasets, d.Name)
		}
		answer.Text = orDefault(f.text, fallbackOverviewText(r.datasets, r.relations))
		return answer
	}

	if f.text == "" && len(r.steps) > 0 {
		// Модель работала, но до результата не дошла: честнее попросить
		// уточнить вопрос, чем рассказывать о возможностях платформы.
		answer.Intent = domain.IntentClarify
		answer.Text = "Не получилось посчитать ответ по данным. Уточните, какой показатель нужен, " +
			"за какой период и в каком разрезе, — или выберите один из вопросов ниже."
		if len(answer.Followups) == 0 {
			answer.Followups = suggestQuestions(r.datasets)
		}
		return answer
	}

	answer.Intent = domain.IntentSmalltalk
	answer.Text = orDefault(f.text,
		"Я отвечаю на вопросы по загруженным данным: описываю их состав, считаю показатели, "+
			"строю графики и собираю дашборды. Спросите, например, «что есть в этих данных».")
	if hasDigits.MatchString(f.text) {
		// Числа без единого расчёта взяты не из данных.
		answer.Warnings = append(answer.Warnings,
			"Этот ответ дан без расчёта по данным, числа в нём не проверены. "+
				"Уточните вопрос — например, назовите показатель и датасет, — чтобы получить расчёт.")
	}
	return answer
}

var hasDigits = regexp.MustCompile(`\d`)

func (r *agentRun) usedDatasets(results []*agentResult) []string {
	var tables []string
	for _, res := range results {
		tables = append(tables, res.tables...)
	}
	names := datasetNames(r.datasets, tables)
	out := names[:0]
	for i, n := range names {
		if i == 0 || n != names[i-1] {
			out = append(out, n)
		}
	}
	return out
}

func dashboardSummary(d *domain.Dashboard) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Собрал дашборд «%s» из %s. Он сохранён и доступен на вкладке «Дашборды».\n",
		d.Title, pluralWidgets(len(d.Widgets)))
	for _, w := range d.Widgets {
		fmt.Fprintf(&sb, "• %s\n", w.Title)
	}
	return strings.TrimSpace(sb.String())
}

func stepTitle(tool string) string {
	switch tool {
	case toolDescribe:
		return "Изучил структуру данных"
	case toolAggregate:
		return "Посчитал показатели"
	case toolRows:
		return "Посмотрел записи"
	case toolDuplicates:
		return "Проверил повторы"
	case toolSQL:
		return "Выполнил SQL-запрос"
	case toolDashboard:
		return "Собрал дашборд"
	case toolSeasonality:
		return "Оценил сезонность"
	case toolCorrelate:
		return "Проверил связь показателей"
	case toolForecast:
		return "Построил прогноз"
	case toolAnomalies:
		return "Поискал выбросы"
	case toolDashboards:
		return "Посмотрел сохранённые дашборды"
	case toolAddWidgets:
		return "Добавил виджеты на дашборд"
	case toolRemove:
		return "Убрал виджет с дашборда"
	case toolCompare:
		return "Сравнил сегменты"
	case toolWhatIf:
		return "Оценил сценарий «что если»"
	}
	return "Неизвестный инструмент «" + tool + "»"
}

func decodeArgs(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("аргументы не соответствуют схеме инструмента: %w", err)
	}
	return nil
}

func (r *agentRun) resultOutput(res *agentResult) map[string]any {
	out := map[string]any{
		"result_id": res.id,
		"row_count": res.result.RowCount,
	}
	for k, v := range r.tableRows(res.result) {
		out[k] = v
	}
	if res.result.Truncated {
		out["truncated"] = true
	}
	if len(res.warnings) > 0 {
		out["warnings"] = res.warnings
	}
	if res.result.Empty() {
		out["note"] = "результат пуст: проверь фильтры и значения по описанию данных"
	}
	return out
}

// tableRows — результат в виде, удобном модели. Строки из данных проходят
// фильтр: значение, похожее на инструкцию, модель не увидит.
func (r *agentRun) tableRows(rs *domain.ResultSet) map[string]any {
	if rs == nil {
		return map[string]any{}
	}
	cols := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		cols[i] = r.filter.clean(c.Name, maxNameRunes)
	}
	rows := rs.Rows
	if len(rows) > maxToolOutputRows {
		rows = rows[:maxToolOutputRows]
	}
	cells := make([][]any, len(rows))
	for i, row := range rows {
		cells[i] = make([]any, len(row))
		for j, v := range row {
			cells[i][j] = r.cell(v)
		}
	}
	out := map[string]any{"columns": cols, "rows": cells}
	if len(rs.Rows) > len(rows) {
		out["rows_not_shown"] = len(rs.Rows) - len(rows)
	}
	return out
}

func (r *agentRun) cell(v any) any {
	switch x := v.(type) {
	case string:
		return r.filter.clean(x, maxCellRunes)
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 {
			return x.Format("2006-01-02")
		}
		return x.Format(time.RFC3339)
	}
	return v
}

func encodeOutput(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"не удалось сериализовать результат"}`
	}
	return string(b)
}
