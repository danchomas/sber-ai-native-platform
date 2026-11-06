package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/llm"
)

type plan struct {
	Intent     string                           `json:"intent"`
	SQL        string                           `json:"sql"`
	Query      *analytics.Query                 `json:"query"`
	Comparison *analytics.PeriodComparisonQuery `json:"comparison"`
	Chart      *chartJSON                       `json:"chart"`
	Title      string                           `json:"title"`
	Message    string                           `json:"message"`
	Followups  stringList                       `json:"followups"`
	Widgets    []widgetJSON                     `json:"widgets"`
}

type chartJSON struct {
	Type     string     `json:"type"`
	Title    string     `json:"title"`
	X        string     `json:"x"`
	Y        stringList `json:"y"`
	Series   string     `json:"series"`
	Stacked  bool       `json:"stacked"`
	Unit     string     `json:"unit"`
	Subtitle string     `json:"subtitle"`
}

type widgetJSON struct {
	Title    string           `json:"title"`
	Question string           `json:"question"`
	SQL      string           `json:"sql"`
	Query    *analytics.Query `json:"query"`
	Chart    *chartJSON       `json:"chart"`
	Width    int              `json:"width"`
}

type stringList []string

func (s *stringList) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*s = nil
		return nil
	}

	if trimmed[0] == '[' {
		var arr []any
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		out := make([]string, 0, len(arr))
		for _, v := range arr {
			if str := strings.TrimSpace(fmt.Sprint(v)); str != "" && v != nil {
				out = append(out, str)
			}
		}
		*s = out
		return nil
	}

	var single string
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	if single = strings.TrimSpace(single); single != "" {
		*s = []string{single}
	} else {
		*s = nil
	}
	return nil
}

func parsePlan(raw string) (*plan, error) {
	payload, err := llm.ExtractJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("ответ модели не содержит план: %w", err)
	}

	var p plan
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return nil, fmt.Errorf("разбор плана: %w", err)
	}
	if p.Comparison != nil && (p.Query != nil || strings.TrimSpace(p.SQL) != "") {
		return nil, fmt.Errorf("план должен содержать либо comparison, либо query/SQL, но не оба варианта")
	}
	p.SQL = cleanSQL(p.SQL)
	for i := range p.Widgets {
		p.Widgets[i].SQL = cleanSQL(p.Widgets[i].SQL)
	}
	return &p, nil
}

func (p *plan) intent() domain.AnswerIntent {
	switch strings.ToLower(strings.TrimSpace(p.Intent)) {
	case "query", "sql", "data_query":
		if p.hasQuery() {
			return domain.IntentQuery
		}
		return domain.IntentClarify
	case "overview", "describe", "profile":
		return domain.IntentOverview
	case "dashboard", "panel":
		if len(p.Widgets) > 0 {
			return domain.IntentDashboard
		}
		if p.hasQuery() {
			return domain.IntentQuery
		}
		return domain.IntentClarify
	case "clarify", "clarification", "ask":
		return domain.IntentClarify
	case "reject", "refuse", "unsupported":
		return domain.IntentReject
	case "smalltalk", "meta", "help":
		return domain.IntentSmalltalk
	default:
		switch {
		case len(p.Widgets) > 0:
			return domain.IntentDashboard
		case p.hasQuery():
			return domain.IntentQuery
		default:
			return domain.IntentClarify
		}
	}
}

func (p *plan) hasQuery() bool { return p.SQL != "" || p.Query != nil || p.Comparison != nil }

func (c *chartJSON) chartSpec() *domain.ChartSpec {
	if c == nil {
		return nil
	}
	return &domain.ChartSpec{
		Type:     domain.ChartType(strings.ToLower(strings.TrimSpace(c.Type))),
		Title:    c.Title,
		Subtitle: c.Subtitle,
		X:        c.X,
		Y:        c.Y,
		Series:   c.Series,
		Stacked:  c.Stacked,
		Unit:     c.Unit,
	}
}

func cleanSQL(sql string) string {
	s := strings.TrimSpace(sql)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			if lang := strings.TrimSpace(s[:nl]); lang == "" || !strings.ContainsAny(lang, " ()") {
				s = s[nl+1:]
			}
		}
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), ";"))
}
