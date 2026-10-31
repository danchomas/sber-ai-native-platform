package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Dashboard struct {
	ID          uuid.UUID  `json:"id"`
	SessionID   *uuid.UUID `json:"session_id,omitempty"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Widgets     []Widget   `json:"widgets"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	RefreshedAt *time.Time `json:"refreshed_at,omitempty"`

	DateRange *DateRange `json:"date_range,omitempty"`

	Period *DateRange `json:"period,omitempty"`
}

const (
	TrashKindDashboard = "dashboard"
	TrashKindWidget    = "widget"
)

type TrashItem struct {
	Kind           string    `json:"kind"`
	ID             uuid.UUID `json:"id"`
	DashboardID    uuid.UUID `json:"dashboard_id"`
	Title          string    `json:"title"`
	DashboardTitle string    `json:"dashboard_title"`
	Widgets        int       `json:"widgets"`
	DeletedAt      time.Time `json:"deleted_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type DateRange struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

type Widget struct {
	ID          uuid.UUID       `json:"id"`
	DashboardID uuid.UUID       `json:"dashboard_id"`
	Position    int             `json:"position"`
	Title       string          `json:"title"`
	Question    string          `json:"question,omitempty"`
	SQL         string          `json:"sql"`
	Query       json.RawMessage `json:"query,omitempty"`
	Chart       ChartSpec       `json:"chart"`
	Width       int             `json:"width"`
	Result      *ResultSet      `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`

	Headline *Headline `json:"headline,omitempty"`

	PeriodApplied bool `json:"period_applied,omitempty"`
}
