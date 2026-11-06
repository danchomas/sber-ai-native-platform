package domain

import (
	"encoding/json"
	"strconv"
	"time"
)

type ResultColumn struct {
	Name string     `json:"name"`
	Type ColumnType `json:"type"`
}

type ResultSet struct {
	Columns   []ResultColumn `json:"columns"`
	Rows      [][]any        `json:"rows"`
	RowCount  int            `json:"row_count"`
	Truncated bool           `json:"truncated"`
	Duration  Millis         `json:"duration_ms"`
	SQL       string         `json:"sql,omitempty"`
}

// Millis — длительность, которая в JSON записывается целыми миллисекундами,
// как обещает имя поля. time.Duration без обёртки уходит наружу наносекундами.
type Millis time.Duration

func (m Millis) MarshalJSON() ([]byte, error) {
	return strconv.AppendInt(nil, time.Duration(m).Milliseconds(), 10), nil
}

func (m *Millis) UnmarshalJSON(data []byte) error {
	var ms int64
	if err := json.Unmarshal(data, &ms); err != nil {
		return err
	}
	*m = Millis(time.Duration(ms) * time.Millisecond)
	return nil
}

func (r *ResultSet) Empty() bool { return r == nil || len(r.Rows) == 0 }

func (r *ResultSet) ColumnIndex(name string) int {
	for i, c := range r.Columns {
		if c.Name == name {
			return i
		}
	}
	return -1
}

func (r *ResultSet) Head(n int) *ResultSet {
	if r == nil {
		return nil
	}
	if n < 0 || n >= len(r.Rows) {
		return r
	}
	return &ResultSet{
		Columns:   r.Columns,
		Rows:      r.Rows[:n],
		RowCount:  r.RowCount,
		Truncated: true,
		Duration:  r.Duration,
		SQL:       r.SQL,
	}
}
