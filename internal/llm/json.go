package llm

import (
	"errors"
	"strings"
)

func ExtractJSON(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("пустой ответ модели")
	}

	if idx := strings.Index(s, "```"); idx >= 0 {
		rest := s[idx+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			if lang := strings.TrimSpace(rest[:nl]); lang == "" || !strings.ContainsAny(lang, " {") {
				rest = rest[nl+1:]
			}
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			rest = rest[:end]
		}
		s = strings.TrimSpace(rest)
	}

	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return "", errors.New("в ответе модели нет JSON")
	}

	open := rune(s[start])
	close := '}'
	if open == '[' {
		close = ']'
	}

	depth, inString, escaped := 0, false, false
	for i, r := range s[start:] {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inString:
			escaped = true
		case r == '"':
			inString = !inString
		case inString:
		case r == open:
			depth++
		case r == close:
			depth--
			if depth == 0 {
				return s[start : start+i+1], nil
			}
		}
	}
	return "", errors.New("незакрытый JSON в ответе модели")
}
