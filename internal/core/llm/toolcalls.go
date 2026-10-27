package llm

import (
	"encoding/json"
	"strings"

	"github.com/heinousness/aibi/internal/core/domain"
)

func ToolCallsFromText(s string, known func(name string) bool) []domain.ToolCall {
	var chunks []string
	rest := s
	for {
		start := strings.Index(rest, "<tool_call>")
		if start < 0 {
			break
		}
		rest = rest[start+len("<tool_call>"):]
		end := strings.Index(rest, "</tool_call>")
		if end < 0 {
			chunks = append(chunks, rest)
			break
		}
		chunks = append(chunks, rest[:end])
		rest = rest[end+len("</tool_call>"):]
	}
	if len(chunks) == 0 {
		chunks = []string{s}
	}

	var calls []domain.ToolCall
	for _, chunk := range chunks {
		payload, err := ExtractJSON(chunk)
		if err != nil {
			continue
		}
		var raw struct {
			Name       string          `json:"name"`
			Arguments  json.RawMessage `json:"arguments"`
			Parameters json.RawMessage `json:"parameters"`
		}
		if err := json.Unmarshal([]byte(payload), &raw); err != nil || !known(raw.Name) {
			continue
		}
		args := raw.Arguments
		if len(args) == 0 {
			args = raw.Parameters
		}
		calls = append(calls, domain.ToolCall{
			ID:        "text_" + raw.Name,
			Name:      raw.Name,
			Arguments: decodeArguments(args),
		})
	}
	return calls
}
