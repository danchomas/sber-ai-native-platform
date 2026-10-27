package chart

import (
	"testing"
	"time"

	"github.com/heinousness/aibi/internal/core/domain"
)

func TestHeadline(t *testing.T) {
	month := func(m int) time.Time { return time.Date(2024, time.Month(m), 1, 0, 0, 0, 0, time.UTC) }
	tests := []struct {
		name      string
		spec      domain.ChartSpec
		rs        *domain.ResultSet
		label     string
		value     float64
		change    *float64
		share     *float64
		text      string
		expectNil bool
	}{
		{
			name:  "kpi",
			spec:  domain.ChartSpec{Type: domain.ChartTypeKPI, Y: []string{"Выручка"}},
			rs:    rs([]domain.ResultColumn{col("Выручка", domain.ColumnTypeNumeric)}, []any{1234567.0}),
			label: "Выручка", value: 1234567, text: "Выручка: 1,2 млн",
		},
		{
			name: "trend with growth",
			spec: domain.ChartSpec{Type: domain.ChartTypeLine, X: "Месяц", Y: []string{"Заказы"}},
			rs: rs([]domain.ResultColumn{col("Месяц", domain.ColumnTypeDate), col("Заказы", domain.ColumnTypeInteger)},
				[]any{month(4), int64(80)}, []any{month(5), int64(100)}, []any{month(6), int64(120)}),
			label: "Последнее значение · 06.2024", value: 120, change: ptr(20),
			text: "Заказы: 120, +20% к предыдущему периоду",
		},
		{
			name: "trend skips missing last value",
			spec: domain.ChartSpec{Type: domain.ChartTypeLine, X: "Месяц", Y: []string{"Заказы"}},
			rs: rs([]domain.ResultColumn{col("Месяц", domain.ColumnTypeDate), col("Заказы", domain.ColumnTypeInteger)},
				[]any{month(4), int64(100)}, []any{month(5), int64(50)}, []any{month(6), nil}),
			label: "Последнее значение · 05.2024", value: 50, change: ptr(-50),
			text: "Заказы: 50, −50% к предыдущему периоду",
		},
		{
			name: "leader with share",
			spec: domain.ChartSpec{Type: domain.ChartTypeBar, X: "Статус", Y: []string{"Количество"}},
			rs: rs([]domain.ResultColumn{col("Статус", domain.ColumnTypeText), col("Количество", domain.ColumnTypeInteger)},
				[]any{"paid", int64(700)}, []any{"new", int64(200)}, []any{"cancelled", int64(100)}),
			label: "Лидер · paid", value: 700, share: ptr(70),
			text: "Больше всего — «paid»: 700 (70% от суммы «Количество»)",
		},
		{
			name: "series has no single headline",
			spec: domain.ChartSpec{Type: domain.ChartTypeLine, X: "Месяц", Y: []string{"Заказы"}, Series: "Статус"},
			rs: rs([]domain.ResultColumn{col("Месяц", domain.ColumnTypeDate), col("Статус", domain.ColumnTypeText), col("Заказы", domain.ColumnTypeInteger)},
				[]any{month(4), "paid", int64(1)}),
			expectNil: true,
		},
		{
			name:      "empty result",
			spec:      domain.ChartSpec{Type: domain.ChartTypeKPI},
			rs:        &domain.ResultSet{Columns: []domain.ResultColumn{col("x", domain.ColumnTypeInteger)}},
			expectNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := Headline(tt.spec, tt.rs)
			if tt.expectNil {
				if h != nil {
					t.Fatalf("ожидался nil, получено %+v", h)
				}
				return
			}
			if h == nil {
				t.Fatal("ожидался заголовок")
			}
			if h.Label != tt.label || h.Value != tt.value || h.Text != tt.text {
				t.Fatalf("получено %q / %v / %q", h.Label, h.Value, h.Text)
			}
			if !samePtr(h.ChangePct, tt.change) || !samePtr(h.SharePct, tt.share) {
				t.Fatalf("изменение %v, доля %v", deref(h.ChangePct), deref(h.SharePct))
			}
		})
	}
}

func TestHuman(t *testing.T) {
	tests := []struct {
		input    float64
		expected string
	}{
		{input: 3.756, expected: "3,76"},
		{input: 950, expected: "950"},
		{input: 12500, expected: "12,5 тыс."},
		{input: 1_250_000, expected: "1,3 млн"},
		{input: -2_000_000_000, expected: "-2 млрд"},
	}
	for _, tt := range tests {
		if got := Human(tt.input); got != tt.expected {
			t.Errorf("Human(%v) = %q, ожидалось %q", tt.input, got, tt.expected)
		}
	}
}

func ptr(f float64) *float64 { return &f }

func samePtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func deref(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}
