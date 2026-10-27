package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
