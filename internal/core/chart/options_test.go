package chart

import (
	"slices"
	"testing"

	"github.com/heinousness/aibi/internal/core/domain"
)

func TestOptions(t *testing.T) {
	cases := []struct {
		name    string
		current domain.ChartType
		rs      *domain.ResultSet
		want    []domain.ChartType
	}{
		{
			name:    "категории и одна метрика",
			current: domain.ChartTypeBar,
			rs: &domain.ResultSet{
				Columns: []domain.ResultColumn{{Name: "Статус", Type: domain.ColumnTypeText}, {Name: "Количество", Type: domain.ColumnTypeInteger}},
				Rows:    [][]any{{"paid", 700}, {"new", 200}, {"cancelled", 100}},
			},
			want: []domain.ChartType{domain.ChartTypeBar, domain.ChartTypeHBar, domain.ChartTypePie, domain.ChartTypeDonut, domain.ChartTypeTable},
		},
		{
			name:    "динамика",
			current: domain.ChartTypeLine,
			rs: &domain.ResultSet{
				Columns: []domain.ResultColumn{{Name: "Месяц", Type: domain.ColumnTypeDate}, {Name: "Сумма", Type: domain.ColumnTypeNumeric}},
				Rows:    [][]any{{"2024-01-01", 1.0}, {"2024-02-01", 2.0}, {"2024-03-01", 3.0}},
			},
			want: []domain.ChartType{domain.ChartTypeBar, domain.ChartTypeHBar, domain.ChartTypeLine, domain.ChartTypeArea, domain.ChartTypeTable},
		},
		{
			name:    "одно число",
			current: domain.ChartTypeKPI,
			rs: &domain.ResultSet{
				Columns: []domain.ResultColumn{{Name: "Количество", Type: domain.ColumnTypeInteger}},
				Rows:    [][]any{{1000}},
			},
			want: []domain.ChartType{domain.ChartTypeKPI, domain.ChartTypeTable},
		},
		{
			name:    "пустой результат",
			current: domain.ChartTypeTable,
			rs:      &domain.ResultSet{Columns: []domain.ResultColumn{{Name: "x", Type: domain.ColumnTypeText}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Options(domain.ChartSpec{Type: tc.current, Title: "Заголовок"}, tc.rs)
			types := make([]domain.ChartType, 0, len(got))
			for _, spec := range got {
				types = append(types, spec.Type)
				if spec.Title != "Заголовок" {
					t.Errorf("вариант %s потерял заголовок", spec.Type)
				}
			}
			if !slices.Equal(types, tc.want) {
				t.Fatalf("варианты %v, ожидались %v", types, tc.want)
			}
		})
	}
}

func TestNormalizeDropsIncomparableMetrics(t *testing.T) {
	rs := &domain.ResultSet{
		Columns: []domain.ResultColumn{
			{Name: "Квартал", Type: domain.ColumnTypeText},
			{Name: "Выручка", Type: domain.ColumnTypeNumeric},
			{Name: "Себестоимость", Type: domain.ColumnTypeNumeric},
			{Name: "Заказы", Type: domain.ColumnTypeInteger},
		},
		Rows: [][]any{{"1 кв. 2025", 300000000.0, 180000000.0, int64(28000)}, {"2 кв. 2025", 320000000.0, 190000000.0, int64(29000)}},
	}
	got := Normalize(&domain.ChartSpec{Type: domain.ChartTypeLine, X: "Квартал", Y: []string{"Выручка", "Себестоимость", "Заказы"}}, rs, "Динамика")
	if len(got.Y) != 2 || got.Y[1] != "Себестоимость" {
		t.Fatalf("на графике должны остаться показатели одного масштаба: %v", got.Y)
	}
}
