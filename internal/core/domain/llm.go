package domain

import (
	"context"
	"encoding/json"
)

// RoleTool — ответ инструмента в разговоре с моделью. В истории диалога
// не хранится: chat_messages знает только user, assistant и system.
const RoleTool MessageRole = "tool"

type Completion struct {
	System      string
	Messages    []ChatMessage
	Tools       []ToolSpec
	Temperature float64
	MaxTokens   int
	JSONOnly    bool
	// RequireTool просит модель обязательно вызвать инструмент
	// (tool_choice: "required"), а не ответить текстом.
	RequireTool bool
}

type ChatMessage struct {
	Role       MessageRole
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
}

// ToolSpec описывает инструмент для модели: Parameters — JSON Schema аргументов.
type ToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// Reply — ответ модели в разговоре с инструментами: либо текст, либо вызовы.
type Reply struct {
	Content   string
	ToolCalls []ToolCall
}

type LLM interface {
	Complete(ctx context.Context, req Completion) (string, error)
	Model() string
	Ping(ctx context.Context) error
}
