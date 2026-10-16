package postgres

import (
	"testing"

	"github.com/heinousness/aibi/internal/domain"
)

func TestColumnTypeFromPG(t *testing.T) {
	cases := []struct {
		pgType string
		want   domain.ColumnType
	}{
		{"integer", domain.ColumnTypeInteger},
		{"bigint", domain.ColumnTypeInteger},
		{"int4", domain.ColumnTypeInteger},
		{"numeric(12,2)", domain.ColumnTypeNumeric},
		{"double precision", domain.ColumnTypeNumeric},
		{"money", domain.ColumnTypeNumeric},
		{"boolean", domain.ColumnTypeBoolean},
		{"date", domain.ColumnTypeDate},
		{"timestamp with time zone", domain.ColumnTypeTimestamp},
		{"timestamp without time zone", domain.ColumnTypeTimestamp},
		{"pg_catalog.timestamptz", domain.ColumnTypeTimestamp},
		{"character varying(255)", domain.ColumnTypeText},
		{"text", domain.ColumnTypeText},
		{"uuid", domain.ColumnTypeText},
		{"jsonb", domain.ColumnTypeText},
		{"integer[]", domain.ColumnTypeText},
		{"  TEXT  ", domain.ColumnTypeText},
	}

	for _, c := range cases {
		t.Run(c.pgType, func(t *testing.T) {
			if got := columnTypeFromPG(c.pgType); got != c.want {
				t.Errorf("columnTypeFromPG(%q) = %q, ожидалось %q", c.pgType, got, c.want)
			}
		})
	}
}
