package domain

import (
	"time"

	"github.com/google/uuid"
)

type DatasetStatus string

const (
	DatasetStatusPending DatasetStatus = "pending"
	DatasetStatusReady   DatasetStatus = "ready"
	DatasetStatusFailed  DatasetStatus = "failed"
)

type ColumnType string

const (
	ColumnTypeText      ColumnType = "text"
	ColumnTypeInteger   ColumnType = "bigint"
	ColumnTypeNumeric   ColumnType = "double precision"
	ColumnTypeBoolean   ColumnType = "boolean"
	ColumnTypeDate      ColumnType = "date"
	ColumnTypeTimestamp ColumnType = "timestamptz"
)

func (t ColumnType) IsNumeric() bool {
	return t == ColumnTypeInteger || t == ColumnTypeNumeric
}

func (t ColumnType) IsTemporal() bool {
	return t == ColumnTypeDate || t == ColumnTypeTimestamp
}

type SemanticRole string

const (
	RoleIdentifier SemanticRole = "identifier"
	RoleTemporal   SemanticRole = "temporal"
	RoleMeasure    SemanticRole = "measure"
	RoleDimension  SemanticRole = "dimension"
	RoleFreeText   SemanticRole = "free_text"
	RoleUnknown    SemanticRole = "unknown"
)

type Dataset struct {
	ID          uuid.UUID     `json:"id"`
	Name        string        `json:"name"`
	Origin      DatasetOrigin `json:"origin"`
	SourceID    *uuid.UUID    `json:"source_id,omitempty"`
	SourceName  string        `json:"source_name,omitempty"`
	Schema      string        `json:"schema"`
	TableName   string        `json:"table_name"`
	SourceFile  string        `json:"source_file"`
	Delimiter   string        `json:"delimiter"`
	RowCount    int64         `json:"row_count"`
	SizeBytes   int64         `json:"size_bytes"`
	Status      DatasetStatus `json:"status"`
	FailReason  string        `json:"fail_reason,omitempty"`
	Description string        `json:"description,omitempty"`
	Columns     []Column      `json:"columns,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

func (d *Dataset) Ready() bool { return d.Status == DatasetStatusReady }

func (d *Dataset) Attached() bool { return d.Origin == OriginAttached }

func (d *Dataset) Ref() TableRef {
	return TableRef{SourceID: d.SourceID, Schema: d.Schema, Table: d.TableName}
}

func (d *Dataset) Column(name string) (Column, bool) {
	for _, c := range d.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return Column{}, false
}

func (d *Dataset) ColumnsByRole(role SemanticRole) []Column {
	out := make([]Column, 0, len(d.Columns))
	for _, c := range d.Columns {
		if c.Role == role {
			out = append(out, c)
		}
	}
	return out
}

type Column struct {
	ID         uuid.UUID      `json:"id"`
	DatasetID  uuid.UUID      `json:"dataset_id"`
	Position   int            `json:"position"`
	SourceName string         `json:"source_name"`
	Name       string         `json:"name"`
	Type       ColumnType     `json:"type"`
	Role       SemanticRole   `json:"role"`
	Nullable   bool           `json:"nullable"`
	Comment    string         `json:"comment,omitempty"`
	Profile    *ColumnProfile `json:"profile,omitempty"`
}

type ColumnProfile struct {
	RowCount      int64            `json:"row_count"`
	NullCount     int64            `json:"null_count"`
	DistinctCount int64            `json:"distinct_count"`
	MinValue      *string          `json:"min_value,omitempty"`
	MaxValue      *string          `json:"max_value,omitempty"`
	Mean          *float64         `json:"mean,omitempty"`
	StdDev        *float64         `json:"stddev,omitempty"`
	TopValues     []ValueFrequency `json:"top_values,omitempty"`
	ProfiledAt    time.Time        `json:"profiled_at"`
}

func (p *ColumnProfile) NullShare() float64 {
	if p == nil || p.RowCount == 0 {
		return 0
	}
	return float64(p.NullCount) / float64(p.RowCount)
}

func (p *ColumnProfile) Unique() bool {
	if p == nil || p.RowCount == 0 {
		return false
	}
	return p.DistinctCount == p.RowCount-p.NullCount && p.DistinctCount > 1
}

type ValueFrequency struct {
	Value string  `json:"value"`
	Count int64   `json:"count"`
	Share float64 `json:"share"`
}

type Relation struct {
	LeftDatasetID  uuid.UUID `json:"left_dataset_id"`
	LeftDataset    string    `json:"left_dataset"`
	LeftColumn     string    `json:"left_column"`
	RightDatasetID uuid.UUID `json:"right_dataset_id"`
	RightDataset   string    `json:"right_dataset"`
	RightColumn    string    `json:"right_column"`
	Overlap        float64   `json:"overlap"`
	MatchedValues  int64     `json:"matched_values"`
	Cardinality    string    `json:"cardinality"`
	Confidence     float64   `json:"confidence"`
}

type TableRef struct {
	SourceID *uuid.UUID
	Schema   string
	Table    string
	Column   string
}

func (r TableRef) SameSource(other TableRef) bool {
	switch {
	case r.SourceID == nil && other.SourceID == nil:
		return true
	case r.SourceID == nil || other.SourceID == nil:
		return false
	default:
		return *r.SourceID == *other.SourceID
	}
}

type Profile struct {
	Dataset       Dataset    `json:"dataset"`
	TimeCoverage  []TimeSpan `json:"time_coverage,omitempty"`
	CandidateKey  []string   `json:"candidate_keys,omitempty"`
	DuplicateRows int64      `json:"duplicate_rows"`
	Relations     []Relation `json:"relations,omitempty"`
	Sample        *ResultSet `json:"sample,omitempty"`
}

type TimeSpan struct {
	Column string     `json:"column"`
	From   *time.Time `json:"from,omitempty"`
	To     *time.Time `json:"to,omitempty"`
	Days   int        `json:"days"`
}
