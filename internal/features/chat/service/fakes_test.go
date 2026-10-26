package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
)

type scriptedLLM struct {
	mu        sync.Mutex
	planner   []string
	repair    []string
	narration string
	overview  string
	err       error
	calls     map[string]int
}

func newScriptedLLM(planner ...string) *scriptedLLM {
	return &scriptedLLM{planner: planner, calls: map[string]int{}}
}

func (l *scriptedLLM) Complete(_ context.Context, req domain.Completion) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.err != nil {
		return "", l.err
	}

	switch {
	case strings.Contains(req.System, "аналитический агент BI-платформы"):
		l.calls["planner"]++
		return pop(&l.planner)
	case strings.Contains(req.System, "Ты исправляешь"):
		l.calls["repair"]++
		return pop(&l.repair)
	case strings.Contains(req.System, "объясняет результат запроса"):
		l.calls["narration"]++
		if l.narration == "" {
			return "", errors.New("нет ответа для изложения")
		}
		return l.narration, nil
	case strings.Contains(req.System, "описываешь незнакомый набор данных"):
		l.calls["overview"]++
		if l.overview == "" {
			return "", errors.New("нет ответа для описания")
		}
		return l.overview, nil
	}
	return "", errors.New("неожиданный запрос к модели")
}

func (l *scriptedLLM) Model() string              { return "scripted" }
func (l *scriptedLLM) Ping(context.Context) error { return l.err }
func (l *scriptedLLM) callCount(step string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls[step]
}

func pop(queue *[]string) (string, error) {
	if len(*queue) == 0 {
		return "", errors.New("сценарий исчерпан")
	}
	head := (*queue)[0]
	*queue = (*queue)[1:]
	return head, nil
}

type fakeCatalog struct {
	datasets  []domain.Dataset
	relations []domain.Relation
	err       error
}

func newFakeCatalog() *fakeCatalog {
	id := uuid.New()
	mean := 1500.0
	minV, maxV := "2023-01-01", "2024-06-30"
	return &fakeCatalog{datasets: []domain.Dataset{{
		ID: id, Name: "orders", TableName: "ds_orders", Status: domain.DatasetStatusReady,
		Origin: domain.OriginUploaded, Schema: "raw",
		RowCount: 1000, SourceFile: "orders.csv",
		Columns: []domain.Column{
			{Name: "order_id", SourceName: "Order ID", Type: domain.ColumnTypeInteger, Role: domain.RoleIdentifier,
				Profile: &domain.ColumnProfile{RowCount: 1000, DistinctCount: 1000}},
			{Name: "created_at", SourceName: "Дата заказа", Type: domain.ColumnTypeTimestamp, Role: domain.RoleTemporal,
				Profile: &domain.ColumnProfile{RowCount: 1000, DistinctCount: 900, MinValue: &minV, MaxValue: &maxV}},
			{Name: "status", SourceName: "Статус", Type: domain.ColumnTypeText, Role: domain.RoleDimension,
				Profile: &domain.ColumnProfile{RowCount: 1000, DistinctCount: 3,
					TopValues: []domain.ValueFrequency{{Value: "paid", Count: 700, Share: 0.7}}}},
			{Name: "amount", SourceName: "Сумма", Type: domain.ColumnTypeNumeric, Role: domain.RoleMeasure,
				Profile: &domain.ColumnProfile{RowCount: 1000, DistinctCount: 400, Mean: &mean}},
		},
	}}}
}

func (c *fakeCatalog) List(context.Context) ([]domain.Dataset, error) {
	return c.datasets, c.err
}

func (c *fakeCatalog) Relations(context.Context) ([]domain.Relation, error) {
	return c.relations, nil
}

func (c *fakeCatalog) Scopes(context.Context) ([]domain.Scope, error) {
	ready := make([]domain.Dataset, 0, len(c.datasets))
	for _, d := range c.datasets {
		if d.Ready() {
			ready = append(ready, d)
		}
	}
	if len(ready) == 0 {
		return nil, nil
	}
	return []domain.Scope{{
		Target:   domain.QueryTarget{Schemas: []string{"raw"}},
		Datasets: ready,
	}}, nil
}

func (c *fakeCatalog) Schema() string { return "raw" }

type fixedModel struct{ model domain.LLM }

func (f fixedModel) Current(context.Context) domain.LLM { return f.model }

type fakeExecutor struct {
	result   *domain.ResultSet
	err      error
	queries  []string
	args     [][]any
	failOnce error
}

func (e *fakeExecutor) Query(_ context.Context, _ domain.QueryTarget, sql string, _ int, args ...any) (*domain.ResultSet, error) {
	e.queries = append(e.queries, sql)
	e.args = append(e.args, args)
	if e.failOnce != nil {
		err := e.failOnce
		e.failOnce = nil
		return nil, err
	}
	if e.err != nil {
		return nil, e.err
	}
	return e.result, nil
}

type memorySessions struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]*domain.Session
	messages map[uuid.UUID][]domain.Message
}

func newMemorySessions() *memorySessions {
	return &memorySessions{
		sessions: map[uuid.UUID]*domain.Session{},
		messages: map[uuid.UUID][]domain.Message{},
	}
}

func (m *memorySessions) CreateSession(_ context.Context, s *domain.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
	return nil
}

func (m *memorySessions) GetSession(_ context.Context, id uuid.UUID) (*domain.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, apperr.ErrNotFound
	}
	return s, nil
}

func (m *memorySessions) ListSessions(context.Context, int) ([]domain.Session, error) {
	return nil, nil
}
func (m *memorySessions) RenameSession(_ context.Context, id uuid.UUID, title string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.Title = title
	}
	return nil
}
func (m *memorySessions) TouchSession(context.Context, uuid.UUID) error  { return nil }
func (m *memorySessions) DeleteSession(context.Context, uuid.UUID) error { return nil }

func (m *memorySessions) AddMessage(_ context.Context, msg *domain.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[msg.SessionID] = append(m.messages[msg.SessionID], *msg)
	return nil
}

func (m *memorySessions) ListMessages(_ context.Context, id uuid.UUID, _ int) ([]domain.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages[id], nil
}

func (m *memorySessions) LastTurns(_ context.Context, id uuid.UUID, _ int) ([]domain.Turn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var turns []domain.Turn
	for _, msg := range m.messages[id] {
		if msg.Role == domain.RoleUser {
			turns = append(turns, domain.Turn{Question: msg.Content})
		}
	}
	return turns, nil
}

type fakeDashboards struct {
	created []domain.Dashboard
	err     error
}

func (d *fakeDashboards) CreateFromWidgets(
	_ context.Context, sessionID *uuid.UUID, title, description string, widgets []domain.Widget,
) (*domain.Dashboard, error) {
	if d.err != nil {
		return nil, d.err
	}
	dash := domain.Dashboard{
		ID: uuid.New(), SessionID: sessionID, Title: title,
		Description: description, Widgets: widgets,
	}
	d.created = append(d.created, dash)
	return &dash, nil
}

// toolLLM — модель с function calling: на каждый ход разговора отдаёт
// следующий заготовленный ответ и запоминает, что ей прислали.
type toolLLM struct {
	*scriptedLLM
	mu       sync.Mutex
	replies  []domain.Reply
	err      error
	requests []domain.Completion
}

func newToolLLM(replies ...domain.Reply) *toolLLM {
	return &toolLLM{scriptedLLM: newScriptedLLM(), replies: replies}
}

func (l *toolLLM) Converse(_ context.Context, req domain.Completion) (domain.Reply, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests = append(l.requests, req)
	if l.err != nil {
		return domain.Reply{}, l.err
	}
	if len(l.replies) == 0 {
		return domain.Reply{}, errors.New("сценарий исчерпан")
	}
	head := l.replies[0]
	l.replies = l.replies[1:]
	return head, nil
}

func (l *toolLLM) conversations() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.requests)
}

// toolMessages возвращает ответы инструментов из последнего запроса к модели.
func (l *toolLLM) toolMessages() []domain.ChatMessage {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.requests) == 0 {
		return nil
	}
	var out []domain.ChatMessage
	for _, m := range l.requests[len(l.requests)-1].Messages {
		if m.Role == domain.RoleTool {
			out = append(out, m)
		}
	}
	return out
}

func call(id, name, args string) domain.ToolCall {
	return domain.ToolCall{ID: id, Name: name, Arguments: json.RawMessage(args)}
}

func calls(c ...domain.ToolCall) domain.Reply { return domain.Reply{ToolCalls: c} }
