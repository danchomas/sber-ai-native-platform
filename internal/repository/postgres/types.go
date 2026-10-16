package postgres

import (
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/heinousness/aibi/internal/domain"
)

func resultColumns(fields []pgconn.FieldDescription) []domain.ResultColumn {
	out := make([]domain.ResultColumn, len(fields))
	for i, f := range fields {
		out[i] = domain.ResultColumn{Name: f.Name, Type: columnTypeFromOID(f.DataTypeOID)}
	}
	return out
}

func columnTypeFromOID(oid uint32) domain.ColumnType {
	switch oid {
	case pgtype.Int2OID, pgtype.Int4OID, pgtype.Int8OID:
		return domain.ColumnTypeInteger
	case pgtype.Float4OID, pgtype.Float8OID, pgtype.NumericOID:
		return domain.ColumnTypeNumeric
	case pgtype.BoolOID:
		return domain.ColumnTypeBoolean
	case pgtype.DateOID:
		return domain.ColumnTypeDate
	case pgtype.TimestampOID, pgtype.TimestamptzOID:
		return domain.ColumnTypeTimestamp
	default:
		return domain.ColumnTypeText
	}
}

func normalizeValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case time.Time:
		if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
			return t.Format("2006-01-02")
		}
		return t.Format(time.RFC3339)
	case pgtype.Numeric:
		f, err := t.Float64Value()
		if err != nil || !f.Valid {
			return nil
		}
		return roundFloat(f.Float64)
	case float64:
		return roundFloat(t)
	case float32:
		return roundFloat(float64(t))
	case [16]byte:
		return uuid.UUID(t).String()
	case []byte:
		return string(t)
	case pgtype.Interval:
		return time.Duration(t.Microseconds * int64(time.Microsecond)).String()
	default:
		return v
	}
}

func roundFloat(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	rounded := math.Round(f*1e6) / 1e6
	if rounded == math.Trunc(rounded) && math.Abs(rounded) < 1e15 {
		return rounded
	}
	return rounded
}
