package domain

import "context"

type Completion struct {
	System      string
	Messages    []ChatMessage
	Temperature float64
	MaxTokens   int
	JSONOnly    bool
}

type ChatMessage struct {
	Role    MessageRole
	Content string
}

type LLM interface {
	Complete(ctx context.Context, req Completion) (string, error)
	Model() string
	Ping(ctx context.Context) error
}
