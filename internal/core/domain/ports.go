package domain

import (
	"context"
)

type RawDataRepository interface {
	CreateTable(ctx context.Context, table string, cols []Column) error
	DropTable(ctx context.Context, table string) error
	CopyRows(ctx context.Context, table string, cols []Column, rows [][]any) (int64, error)
	Analyze(ctx context.Context, table string) error

	RowCount(ctx context.Context, ref TableRef) (int64, error)
	TableSizeBytes(ctx context.Context, ref TableRef) (int64, error)
	ProfileColumn(ctx context.Context, ref TableRef, col Column, topN int) (*ColumnProfile, error)
	TimeSpan(ctx context.Context, ref TableRef, col Column) (*TimeSpan, error)
	DuplicateRows(ctx context.Context, ref TableRef, cols []Column) (int64, error)
	Sample(ctx context.Context, ref TableRef, limit int) (*ResultSet, error)

	ValueOverlap(ctx context.Context, left, right TableRef) (share float64, matched int64, err error)

	Query(ctx context.Context, target QueryTarget, sql string, maxRows int, args ...any) (*ResultSet, error)

	ExplainCost(ctx context.Context, target QueryTarget, sql string) (float64, error)
}
