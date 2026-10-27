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

	// Поля ниже заполняются при пересчёте и не хранятся.

	// RefreshedAt — когда показатели пересчитаны.
	RefreshedAt *time.Time `json:"refreshed_at,omitempty"`
	// DateRange — за какой период есть данные у виджетов с динамикой:
	// по нему интерфейс строит быстрые фильтры «последний год», «квартал».
	DateRange *DateRange `json:"date_range,omitempty"`
	// Period — фильтр периода, применённый при пересчёте.
	Period *DateRange `json:"period,omitempty"`
}

// DateRange — период в днях, границы включительно, формат ГГГГ-ММ-ДД.
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
	// Headline — главное число виджета, считается при пересчёте.
	Headline *Headline `json:"headline,omitempty"`
	// PeriodApplied — к виджету применён фильтр периода панели. Виджет
	// без временной колонки фильтр не принимает и показывает всё.
	PeriodApplied bool `json:"period_applied,omitempty"`
}
