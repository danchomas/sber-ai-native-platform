package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/config"
)

const OpenRouterBaseURL = "https://openrouter.ai/api/v1"

const (
	headerReferer = "HTTP-Referer"
	headerTitle   = "X-Title"

	appTitle = "AI-Native BI"
	appURL   = "https://github.com/heinousness/aibi"
)

func NewOpenRouter(c config.LLM) *Client {
	if c.BaseURL == "" {
		c.BaseURL = OpenRouterBaseURL
	}
	return New(c).WithoutReasoning().WithHeaders(map[string]string{
		headerReferer: appURL,
		headerTitle:   appTitle,
	})
}

// IsOpenRouter сообщает, что адрес указывает на OpenRouter.
func IsOpenRouter(baseURL string) bool {
	return strings.TrimRight(baseURL, "/") == OpenRouterBaseURL
}

type CatalogEntry struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int    `json:"context_length"`
	PromptPrice   string `json:"prompt_price,omitempty"`

	JSONMode *bool `json:"json_mode,omitempty"`
	Free     bool  `json:"free"`
}

func (c *Client) Catalog(ctx context.Context) ([]CatalogEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperr.ErrLLMUnavailable, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperr.ErrLLMUnavailable, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s: %s", apperr.ErrLLMUnavailable, resp.Status, snippet(body))
	}

	var parsed struct {
		Data []struct {
			ID            string   `json:"id"`
			Name          string   `json:"name"`
			ContextLength int      `json:"context_length"`
			Supported     []string `json:"supported_parameters"`
			Pricing       struct {
				Prompt string `json:"prompt"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("разбор каталога моделей: %w", err)
	}

	out := make([]CatalogEntry, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		e := CatalogEntry{
			ID:            m.ID,
			Name:          orFallback(m.Name, m.ID),
			ContextLength: m.ContextLength,
			PromptPrice:   m.Pricing.Prompt,
			Free:          isFreePrice(m.Pricing.Prompt),
		}

		if len(m.Supported) > 0 {
			jsonMode := slices.Contains(m.Supported, "response_format")
			e.JSONMode = &jsonMode
		}
		out = append(out, e)
	}
	return out, nil
}

func isFreePrice(p string) bool {
	v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
	if err != nil {
		return false
	}
	return v == 0
}

func orFallback(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
