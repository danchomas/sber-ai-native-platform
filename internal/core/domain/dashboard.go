package domain

import (
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
}

type Widget struct {
	ID          uuid.UUID  `json:"id"`
	DashboardID uuid.UUID  `json:"dashboard_id"`
	Position    int        `json:"position"`
	Title       string     `json:"title"`
	Question    string     `json:"question,omitempty"`
	SQL         string     `json:"sql"`
	Chart       ChartSpec  `json:"chart"`
	Width       int        `json:"width"`
	Result      *ResultSet `json:"result,omitempty"`
	Error       string     `json:"error,omitempty"`
}
