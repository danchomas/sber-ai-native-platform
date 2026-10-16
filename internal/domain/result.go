package domain

import "time"

type ResultColumn struct {
	Name string     `json:"name"`
	Type ColumnType `json:"type"`
}

type ResultSet struct {
	Columns   []ResultColumn `json:"columns"`
	Rows      [][]any        `json:"rows"`
	RowCount  int            `json:"row_count"`
	Truncated bool           `json:"truncated"`
	Duration  time.Duration  `json:"duration_ms"`
	SQL       string         `json:"sql,omitempty"`
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
