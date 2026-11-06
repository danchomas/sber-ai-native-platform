package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
)

func TestConverseSendsToolsAndParsesCalls(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("тело запроса: %v", err)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":null,"tool_calls":[
			{"id":"c1","type":"function","function":{"name":"aggregate","arguments":"{\"dataset\":\"orders\"}"}},
			{"type":"function","function":{"name":"answer","arguments":{"text":"готово"}}}
		]}}]}`)
	}))
	defer srv.Close()

	client := New(config.LLM{BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second})
	reply, err := client.Converse(context.Background(), domain.Completion{
		System: "система",
		Messages: []domain.ChatMessage{
			{Role: domain.RoleUser, Content: "вопрос"},
			{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
				{ID: "c0", Name: "describe_dataset", Arguments: json.RawMessage(`{"dataset":"orders"}`)},
			}},
			{Role: domain.RoleTool, ToolCallID: "c0", Content: `{"columns":[]}`},
		},
		Tools: []domain.ToolSpec{{
			Name: "aggregate", Description: "агрегат",
			Parameters: json.RawMessage(`{"type":"object"}`),
		}},
		JSONOnly: true,
	})
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	if len(got.Tools) != 1 || got.Tools[0].Function.Name != "aggregate" || got.Tools[0].Type != "function" {
		t.Fatalf("инструменты в запросе: %+v", got.Tools)
	}
	if got.ResponseFormat != nil {
		t.Fatal("json_object несовместим с инструментами и не должен отправляться")
	}
	if len(got.Messages) != 4 || got.Messages[3].Role != "tool" || got.Messages[3].ToolCallID != "c0" {
		t.Fatalf("сообщения в запросе: %+v", got.Messages)
	}
	var packed string
	if err := json.Unmarshal(got.Messages[2].ToolCalls[0].Function.Arguments, &packed); err != nil ||
		!strings.Contains(packed, "orders") {
		t.Fatalf("аргументы прошлого вызова должны уходить строкой: %s", got.Messages[2].ToolCalls[0].Function.Arguments)
	}

	if len(reply.ToolCalls) != 2 {
		t.Fatalf("вызовы в ответе: %+v", reply.ToolCalls)
	}
	if string(reply.ToolCalls[0].Arguments) != `{"dataset":"orders"}` {
		t.Fatalf("аргументы-строка не распакованы: %s", reply.ToolCalls[0].Arguments)
	}
	if reply.ToolCalls[1].ID == "" || !strings.Contains(string(reply.ToolCalls[1].Arguments), "готово") {
		t.Fatalf("вызов без id и с аргументами-объектом: %+v", reply.ToolCalls[1])
	}
}

func TestConverseRequiresTool(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"tool_choice \"required\" is not supported"}}`)
	}))
	defer srv.Close()

	client := New(config.LLM{BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second})
	_, err := client.Converse(context.Background(), domain.Completion{
		Messages:    []domain.ChatMessage{{Role: domain.RoleUser, Content: "вопрос"}},
		Tools:       []domain.ToolSpec{{Name: "answer", Parameters: json.RawMessage(`{"type":"object"}`)}},
		RequireTool: true,
	})
	if got.ToolChoice != "required" {
		t.Fatalf("tool_choice = %q", got.ToolChoice)
	}
	if !errors.Is(err, ErrToolChoiceUnsupported) || errors.Is(err, ErrToolsUnsupported) {
		t.Fatalf("отказ от tool_choice не должен выглядеть как отказ от инструментов: %v", err)
	}
}

func TestOpenRouterSendsProviderOrder(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ок"}}]}`)
	}))
	defer srv.Close()

	base := config.LLM{BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second, Providers: []string{"Near AI"}}
	if _, err := NewOpenRouter(base).Converse(context.Background(), domain.Completion{}); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if _, err := New(base).Converse(context.Background(), domain.Completion{}); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if !strings.Contains(bodies[0], `"provider":{"order":["Near AI"]}`) {
		t.Errorf("OpenRouter должен получить порядок провайдеров: %s", bodies[0])
	}
	if strings.Contains(bodies[1], `"provider"`) {
		t.Errorf("обычный сервер не должен получать поле provider: %s", bodies[1])
	}
}

func TestOpenRouterPingProbesOneModel(t *testing.T) {
	tests := []struct {
		name, model string
		wantErr     bool
	}{
		{"модель есть", "qwen/qwen3.8-27b", false},
		{"вариант модели", "qwen/qwen3.8-27b:free", false},
		{"модели нет", "qwen/none", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if r.URL.Path != "/models/qwen/qwen3.8-27b/endpoints" {
					http.NotFound(w, r)
					return
				}
				_, _ = io.WriteString(w, `{"data":{"id":"qwen/qwen3.8-27b"}}`)
			}))
			defer srv.Close()

			err := NewOpenRouter(config.LLM{BaseURL: srv.URL, Model: tt.model, Timeout: 5 * time.Second}).Ping(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Ping: %v, ожидалась ошибка: %v", err, tt.wantErr)
			}
			if tt.wantErr && (!errors.Is(err, apperr.ErrLLMUnavailable) || !strings.Contains(err.Error(), tt.model)) {
				t.Fatalf("ошибка должна называть модель: %v", err)
			}
			if slices.Contains(paths, "/models") {
				t.Fatalf("полный список моделей запрашиваться не должен: %v", paths)
			}
		})
	}
}

func TestFallbackPlansOverviewAndWritesNoText(t *testing.T) {
	plan, err := NewFallback().Complete(context.Background(), domain.Completion{JSONOnly: true})
	if err != nil || !strings.Contains(plan, `"intent":"overview"`) {
		t.Fatalf("план: %q, ошибка %v", plan, err)
	}
	if text, err := NewFallback().Complete(context.Background(), domain.Completion{}); err == nil {
		t.Fatalf("текст ответа не должен приходить от заглушки: %q", text)
	}
}

func TestRetriesProviderErrorInBody(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			_, _ = io.WriteString(w, `{"error":{"message":"unexpected EOF","type":"provider_error"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"готово"}}]}`)
	}))
	defer srv.Close()

	c := New(config.LLM{BaseURL: srv.URL, Model: "m", Retries: 1, Timeout: 5 * time.Second})
	got, err := c.Complete(context.Background(), domain.Completion{Messages: []domain.ChatMessage{{Role: domain.RoleUser, Content: "?"}}})
	if err != nil || got != "готово" || calls != 2 {
		t.Fatalf("сбой провайдера в теле ответа должен повторяться: %q, %v, обращений %d", got, err, calls)
	}
}

func TestRetriesTruncatedBody(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"обо`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"готово"}}]}`)
	}))
	defer srv.Close()

	c := New(config.LLM{BaseURL: srv.URL, Model: "m", Retries: 1, Timeout: 5 * time.Second})
	got, err := c.Complete(context.Background(), domain.Completion{Messages: []domain.ChatMessage{{Role: domain.RoleUser, Content: "?"}}})
	if err != nil || got != "готово" || calls != 2 {
		t.Fatalf("оборванное тело ответа должно повторяться: %q, %v, обращений %d", got, err, calls)
	}
}

func TestReasoningOnlyWhenAsked(t *testing.T) {
	c := New(config.LLM{BaseURL: "http://x", Model: "m", MaxTokens: 2048}).WithoutReasoning()
	plain := c.buildRequest(domain.Completion{})
	if plain.Reasoning == nil || plain.Reasoning.Enabled || plain.MaxTokens != 2048 {
		t.Fatalf("по умолчанию рассуждение выключено: %+v", plain.Reasoning)
	}
	asked := c.buildRequest(domain.Completion{Reasoning: true})
	if asked.Reasoning == nil || !asked.Reasoning.Enabled || asked.MaxTokens < reasoningMaxTokens {
		t.Fatalf("по просьбе рассуждение включается с запасом токенов: %+v, %d", asked.Reasoning, asked.MaxTokens)
	}
	local := New(config.LLM{BaseURL: "http://x", Model: "m"}).buildRequest(domain.Completion{Reasoning: true})
	if local.Reasoning != nil {
		t.Fatal("клиенту без управления рассуждением поле не передаётся")
	}
}
