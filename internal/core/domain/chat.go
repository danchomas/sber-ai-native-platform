package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
)

type AnswerIntent string

const (
	IntentOverview  AnswerIntent = "overview"
	IntentQuery     AnswerIntent = "query"
	IntentDashboard AnswerIntent = "dashboard"
	IntentClarify   AnswerIntent = "clarify"
	IntentReject    AnswerIntent = "reject"
	IntentSmalltalk AnswerIntent = "smalltalk"
)

type Session struct {
	ID         uuid.UUID   `json:"id"`
	Title      string      `json:"title"`
	DatasetIDs []uuid.UUID `json:"dataset_ids,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

type Message struct {
	ID        uuid.UUID   `json:"id"`
	SessionID uuid.UUID   `json:"session_id"`
	Role      MessageRole `json:"role"`
	Content   string      `json:"content"`
	Answer    *Answer     `json:"answer,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

type Answer struct {
	Intent      AnswerIntent    `json:"intent"`
	Text        string          `json:"text"`
	SQL         string          `json:"sql,omitempty"`
	Query       json.RawMessage `json:"query,omitempty"`
	Result      *ResultSet      `json:"result,omitempty"`
	Chart       *ChartSpec      `json:"chart,omitempty"`
	DashboardID *uuid.UUID      `json:"dashboard_id,omitempty"`
	Datasets    []string        `json:"datasets,omitempty"`
	Followups   []string        `json:"followups,omitempty"`
	Warnings    []string        `json:"warnings,omitempty"`
	Steps       []AgentStep     `json:"steps,omitempty"`
	Elapsed     int64           `json:"elapsed_ms"`
}

// AgentStep — один вызов инструмента в цикле агента. Показывается
// пользователю как ход работы, поэтому Title — человеческая фраза.
type AgentStep struct {
	Tool    string `json:"tool"`
	Title   string `json:"title"`
	OK      bool   `json:"ok"`
	Detail  string `json:"detail,omitempty"`
	Rows    int    `json:"rows,omitempty"`
	Elapsed int64  `json:"elapsed_ms"`
}

type Turn struct {
	Question string
	Answer   string
	SQL      string
	Query    json.RawMessage
	Chart    *ChartSpec
}
