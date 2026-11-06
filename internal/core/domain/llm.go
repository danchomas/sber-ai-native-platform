package domain

import (
	"context"
	"encoding/json"
)

const RoleTool MessageRole = "tool"

type Completion struct {
	System      string
	Messages    []ChatMessage
	Tools       []ToolSpec
	Temperature float64
	MaxTokens   int
	JSONOnly    bool

	RequireTool bool

	// Reasoning просит модель рассуждать перед ответом, если клиент умеет
	// этим управлять (OpenRouter); остальные клиенты поле не передают.
	Reasoning bool
}

type ChatMessage struct {
	Role       MessageRole
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
}

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

type Reply struct {
	Content   string
	ToolCalls []ToolCall
}

type LLM interface {
	Complete(ctx context.Context, req Completion) (string, error)
	Model() string
	Ping(ctx context.Context) error
}
