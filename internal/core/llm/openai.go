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

type Client struct {
	http        *http.Client
	baseURL     string
	apiKey      string
	model       string
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

var _ domain.LLM = (*Client)(nil)

func (c *Client) Model() string { return c.model }

type chatRequest struct {
	Model          string            `json:"model"`
	Messages       []chatMessageJSON `json:"messages"`
	Temperature    float64           `json:"temperature"`
	MaxTokens      int               `json:"max_tokens,omitempty"`
	Stream         bool              `json:"stream"`
	ResponseFormat *responseFormat   `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatMessageJSON struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (c *Client) Complete(ctx context.Context, req domain.Completion) (string, error) {
	messages := make([]chatMessageJSON, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, chatMessageJSON{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		messages = append(messages, chatMessageJSON{Role: string(m.Role), Content: m.Content})
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
	if req.JSONOnly {
		payload.ResponseFormat = &responseFormat{Type: "json_object"}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("сериализация запроса к модели: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}

		text, retryable, err := c.do(ctx, body)
		if err == nil {
			return text, nil
		}
		lastErr = err
		if !retryable || ctx.Err() != nil {
			break
		}
	}
	return "", fmt.Errorf("%w: %v", apperr.ErrLLMUnavailable, lastErr)
}

func (c *Client) do(ctx context.Context, body []byte) (text string, retryable bool, err error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", true, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", true, err
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return "", true, fmt.Errorf("модель ответила %s: %s", resp.Status, snippet(raw))
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("модель ответила %s: %s", resp.Status, snippet(raw))
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", false, fmt.Errorf("разбор ответа модели: %w", err)
	}
	if parsed.Error != nil {
		return "", false, errors.New(parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", true, errors.New("модель вернула пустой ответ")
	}
	return parsed.Choices[0].Message.Content, false, nil
}

func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", apperr.ErrLLMUnavailable, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s", apperr.ErrLLMUnavailable, resp.Status)
	}
	return nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
