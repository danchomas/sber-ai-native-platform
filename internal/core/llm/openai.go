package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
)

const maxListedInError = 8

// ErrToolsUnsupported — сервер или модель не умеют function calling.
// Агент в этом случае переходит на разбор ответа в JSON-режиме.
var ErrToolsUnsupported = errors.New("модель не поддерживает вызов инструментов")

// ErrToolChoiceUnsupported — сервер не принимает tool_choice: "required".
// Инструменты при этом работают, просто без обязательного вызова.
var ErrToolChoiceUnsupported = errors.New("сервер не поддерживает обязательный вызов инструмента")

type Client struct {
	http        *http.Client
	baseURL     string
	apiKey      string
	model       string
	headers     map[string]string
	maxTokens   int
	temperature float64
	retries     int
}

func New(c config.LLM) *Client {
	return &Client{
		http:        &http.Client{Timeout: c.Timeout},
		baseURL:     strings.TrimRight(c.BaseURL, "/"),
		apiKey:      c.APIKey,
		model:       c.Model,
		maxTokens:   c.MaxTokens,
		temperature: c.Temperature,
		retries:     max(c.Retries, 0),
	}
}

func (c *Client) WithHeaders(h map[string]string) *Client {
	c.headers = h
	return c
}

func (c *Client) setHeaders(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
}

var _ domain.LLM = (*Client)(nil)

func (c *Client) Model() string { return c.model }

type chatRequest struct {
	Model          string            `json:"model"`
	Messages       []chatMessageJSON `json:"messages"`
	Temperature    float64           `json:"temperature"`
	MaxTokens      int               `json:"max_tokens,omitempty"`
	Stream         bool              `json:"stream"`
	ResponseFormat *responseFormat   `json:"response_format,omitempty"`
	Tools          []toolJSON        `json:"tools,omitempty"`
	ToolChoice     string            `json:"tool_choice,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatMessageJSON struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []toolCallJSON `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type toolJSON struct {
	Type     string           `json:"type"`
	Function toolFunctionJSON `json:"function"`
}

type toolFunctionJSON struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type toolCallJSON struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   *string        `json:"content"`
			ToolCalls []toolCallJSON `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (c *Client) Complete(ctx context.Context, req domain.Completion) (string, error) {
	req.Tools = nil
	reply, err := c.Converse(ctx, req)
	if err != nil {
		return "", err
	}
	return reply.Content, nil
}

// Converse — один ход разговора с инструментами по протоколу function calling
// OpenAI. Его понимают Ollama, vLLM, llama.cpp (с --jinja) и OpenRouter.
func (c *Client) Converse(ctx context.Context, req domain.Completion) (domain.Reply, error) {
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		return domain.Reply{}, fmt.Errorf("сериализация запроса к модели: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return domain.Reply{}, ctx.Err()
			case <-time.After(delay):
			}
		}

		reply, retryable, err := c.do(ctx, body)
		if err == nil {
			return reply, nil
		}
		lastErr = err
		if !retryable || ctx.Err() != nil {
			break
		}
	}
	return domain.Reply{}, fmt.Errorf("%w: %w", apperr.ErrLLMUnavailable, lastErr)
}

func (c *Client) buildRequest(req domain.Completion) chatRequest {
	messages := make([]chatMessageJSON, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, chatMessageJSON{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msg := chatMessageJSON{Role: string(m.Role), Content: m.Content, ToolCallID: m.ToolCallID}
		for _, call := range m.ToolCalls {
			tc := toolCallJSON{ID: call.ID, Type: "function"}
			tc.Function.Name = call.Name
			// В протоколе OpenAI аргументы — JSON, упакованный в строку.
			encoded, _ := json.Marshal(string(argumentsOrEmpty(call.Arguments)))
			tc.Function.Arguments = encoded
			msg.ToolCalls = append(msg.ToolCalls, tc)
		}
		messages = append(messages, msg)
	}

	temperature := req.Temperature
	if temperature == 0 {
		temperature = c.temperature
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = c.maxTokens
	}

	payload := chatRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}
	for _, t := range req.Tools {
		payload.Tools = append(payload.Tools, toolJSON{
			Type: "function",
			Function: toolFunctionJSON{
				Name: t.Name, Description: t.Description, Parameters: t.Parameters,
			},
		})
	}
	if req.RequireTool && len(req.Tools) > 0 {
		payload.ToolChoice = "required"
	}
	if req.JSONOnly && len(req.Tools) == 0 {
		payload.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	return payload
}

func (c *Client) do(ctx context.Context, body []byte) (reply domain.Reply, retryable bool, err error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return domain.Reply{}, false, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.setHeaders(httpReq)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return domain.Reply{}, true, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return domain.Reply{}, true, err
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return domain.Reply{}, true, fmt.Errorf("модель ответила %s: %s", resp.Status, snippet(raw))
	}
	if resp.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(string(raw)), "tool_choice") {
		return domain.Reply{}, false, fmt.Errorf("%w: %s", ErrToolChoiceUnsupported, snippet(raw))
	}
	if resp.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(string(raw)), "tool") {
		return domain.Reply{}, false, fmt.Errorf("%w: %s", ErrToolsUnsupported, snippet(raw))
	}
	if resp.StatusCode != http.StatusOK {
		return domain.Reply{}, false, fmt.Errorf("модель ответила %s: %s", resp.Status, snippet(raw))
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return domain.Reply{}, false, fmt.Errorf("разбор ответа модели: %w", err)
	}
	if parsed.Error != nil {
		return domain.Reply{}, false, errors.New(parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return domain.Reply{}, true, errors.New("модель вернула пустой ответ")
	}

	msg := parsed.Choices[0].Message
	if msg.Content != nil {
		reply.Content = *msg.Content
	}
	for i, tc := range msg.ToolCalls {
		id := tc.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		reply.ToolCalls = append(reply.ToolCalls, domain.ToolCall{
			ID: id, Name: tc.Function.Name, Arguments: decodeArguments(tc.Function.Arguments),
		})
	}
	return reply, false, nil
}

// decodeArguments принимает оба варианта, встречающиеся у серверов:
// аргументы строкой с JSON внутри (OpenAI, vLLM) и сразу объектом.
func decodeArguments(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err == nil {
			trimmed = []byte(strings.TrimSpace(s))
		}
	}
	return argumentsOrEmpty(trimmed)
}

func argumentsOrEmpty(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return json.RawMessage("{}")
	}
	return raw
}

func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", apperr.ErrLLMUnavailable, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s", apperr.ErrLLMUnavailable, resp.Status)
	}
	return c.checkModelListed(body)
}

func (c *Client) checkModelListed(body []byte) error {
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil
	}
	if len(list.Data) == 0 {
		return fmt.Errorf("%w: на сервере нет ни одной модели, нужна %q",
			apperr.ErrLLMUnavailable, c.model)
	}

	available := make([]string, 0, maxListedInError)
	for _, m := range list.Data {
		if m.ID == c.model {
			return nil
		}
		if len(available) < maxListedInError {
			available = append(available, m.ID)
		}
	}

	hint := strings.Join(available, ", ")
	if len(list.Data) > len(available) {
		hint += fmt.Sprintf(" и ещё %d", len(list.Data)-len(available))
	}
	return fmt.Errorf("%w: модели %q нет на сервере, доступны: %s",
		apperr.ErrLLMUnavailable, c.model, hint)
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
