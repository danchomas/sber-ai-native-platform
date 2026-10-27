package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
)

type fakeChat struct {
	steps []domain.AgentStep
	err   error
}

func (c *fakeChat) Create(context.Context, string, []uuid.UUID) (*domain.Session, error) {
	return nil, nil
}
func (c *fakeChat) List(context.Context, int) ([]domain.Session, error)     { return nil, nil }
func (c *fakeChat) Get(context.Context, uuid.UUID) (*domain.Session, error) { return nil, nil }
func (c *fakeChat) Messages(context.Context, uuid.UUID, int) ([]domain.Message, error) {
	return nil, nil
}
func (c *fakeChat) Delete(context.Context, uuid.UUID) error { return nil }
func (c *fakeChat) Ask(ctx context.Context, id uuid.UUID, q string) (*domain.Answer, error) {
	return c.AskWithProgress(ctx, id, q, nil)
}
func (c *fakeChat) AskWithProgress(_ context.Context, _ uuid.UUID, _ string, progress func(domain.AgentStep)) (*domain.Answer, error) {
	for _, s := range c.steps {
		if progress != nil {
			progress(s)
		}
	}
	if c.err != nil {
		return nil, c.err
	}
	return &domain.Answer{Intent: domain.IntentQuery, Text: "Готово."}, nil
}

func stream(t *testing.T, chat *fakeChat, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	New(chat).Register(r)
	req := httptest.NewRequest(http.MethodPost, "/sessions/"+uuid.NewString()+"/messages/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestAskStreamSendsStepsThenAnswer(t *testing.T) {
	chat := &fakeChat{steps: []domain.AgentStep{
		{Tool: "describe_dataset", Title: "Изучил структуру данных", OK: true},
		{Tool: "aggregate", Title: "Посчитал показатели", OK: true, Rows: 3},
	}}
	rec := stream(t, chat, `{"question":"что в данных?"}`)

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	first := strings.Index(body, "event: step")
	answer := strings.Index(body, "event: answer")
	if first < 0 || answer < first || strings.Count(body, "event: step") != 2 {
		t.Fatalf("ожидались два шага, затем ответ:\n%s", body)
	}
	if !strings.Contains(body, `"title":"Посчитал показатели"`) || !strings.Contains(body, `"text":"Готово."`) {
		t.Fatalf("содержимое событий:\n%s", body)
	}
}

func TestAskStreamReportsErrorWithoutDetails(t *testing.T) {
	rec := stream(t, &fakeChat{err: apperr.ErrBusy}, `{"question":"что в данных?"}`)
	body := rec.Body.String()
	if !strings.Contains(body, "event: error") || !strings.Contains(body, `"code":"busy"`) {
		t.Fatalf("ожидалось событие ошибки с кодом:\n%s", body)
	}
}

func TestAskStreamValidatesQuestion(t *testing.T) {
	if rec := stream(t, &fakeChat{}, `{"question":"  "}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("пустой вопрос: код %d", rec.Code)
	}
}
