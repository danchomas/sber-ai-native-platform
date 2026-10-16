package chart

import (
	"testing"

	"github.com/heinousness/aibi/internal/domain"
)

func rs(cols []domain.ResultColumn, rows ...[]any) *domain.ResultSet {
	return &domain.ResultSet{Columns: cols, Rows: rows, RowCount: len(rows)}
}

func col(name string, t domain.ColumnType) domain.ResultColumn {
	return domain.ResultColumn{Name: name, Type: t}
}

func TestPick(t *testing.T) {
	cases := []struct {
		name string
		in   *domain.ResultSet
		want domain.ChartType
	}{
		{
			"одно число — показатель",
			rs([]domain.ResultColumn{col("total", domain.ColumnTypeInteger)}, []any{42}),
			domain.ChartTypeKPI,
		},
		{
			"динамика по месяцам — линия",
			rs([]domain.ResultColumn{col("month", domain.ColumnTypeDate), col("revenue", domain.ColumnTypeNumeric)},
				[]any{"2024-01-01", 10}, []any{"2024-02-01", 20}),
			domain.ChartTypeLine,
		},
		{
			"категории и метрика — столбцы",
			rs([]domain.ResultColumn{col("status", domain.ColumnTypeText), col("cnt", domain.ColumnTypeInteger)},
				[]any{"new", 5}, []any{"paid", 7}),
			domain.ChartTypeBar,
		},
		{
			"много категорий — горизонтальные столбцы",
			manyCategories(20),
			domain.ChartTypeHBar,
		},
		{
			"очень много категорий — таблица",
			manyCategories(60),
			domain.ChartTypeTable,
		},
		{
			"две метрики — рассеяние",
			rs([]domain.ResultColumn{col("x", domain.ColumnTypeNumeric), col("y", domain.ColumnTypeNumeric)},
				[]any{1, 2}, []any{3, 4}),
			domain.ChartTypeScatter,
		},
		{
			"пустой результат — таблица",
			rs([]domain.ResultColumn{col("a", domain.ColumnTypeText)}),
			domain.ChartTypeTable,
		},
		{
			"две категории и метрика — составные столбцы",
			rs([]domain.ResultColumn{col("type", domain.ColumnTypeText), col("role", domain.ColumnTypeText), col("cnt", domain.ColumnTypeInteger)},
				[]any{"a", "x", 1}, []any{"b", "y", 2}, []any{"c", "z", 3}),
			domain.ChartTypeBar,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Pick(tc.in, "тест").Type; got != tc.want {
				t.Fatalf("получено %s, ожидалось %s", got, tc.want)
			}
		})
	}
}

func manyCategories(n int) *domain.ResultSet {
	out := rs([]domain.ResultColumn{col("name", domain.ColumnTypeText), col("cnt", domain.ColumnTypeInteger)})
	for i := 0; i < n; i++ {
		out.Rows = append(out.Rows, []any{string(rune('a'+i%26)) + string(rune('a'+i/26)), i})
	}
	return out
}

func TestNormalizeDropsMissingColumns(t *testing.T) {
	data := rs([]domain.ResultColumn{col("status", domain.ColumnTypeText), col("cnt", domain.ColumnTypeInteger)},
		[]any{"new", 5})

	spec := &domain.ChartSpec{Type: domain.ChartTypeBar, X: "нет_такой", Y: []string{"cnt", "тоже_нет"}}
	got := Normalize(spec, data, "заголовок")

	if got.X != "status" {
		t.Fatalf("ось X не исправлена: %q", got.X)
	}
	if len(got.Y) != 1 || got.Y[0] != "cnt" {
		t.Fatalf("ось Y не очищена: %v", got.Y)
	}
	if got.Title != "заголовок" {
		t.Fatalf("заголовок не подставлен: %q", got.Title)
	}
}

func TestNormalizeRejectsUnknownType(t *testing.T) {
	data := rs([]domain.ResultColumn{col("status", domain.ColumnTypeText), col("cnt", domain.ColumnTypeInteger)},
		[]any{"new", 5})
	got := Normalize(&domain.ChartSpec{Type: "3d-pyramid"}, data, "t")
	if !domain.KnownChartType(got.Type) {
		t.Fatalf("неизвестный тип не заменён: %s", got.Type)
	}
}

func TestNormalizePieWithManySectors(t *testing.T) {
	got := Normalize(&domain.ChartSpec{Type: domain.ChartTypePie, X: "name", Y: []string{"cnt"}},
		manyCategories(20), "t")
	if got.Type != domain.ChartTypeHBar {
		t.Fatalf("круговая диаграмма с 20 секторами должна быть заменена, получено %s", got.Type)
	}
}

func TestNormalizeNilSpec(t *testing.T) {
	data := rs([]domain.ResultColumn{col("total", domain.ColumnTypeInteger)}, []any{1})
	if got := Normalize(nil, data, "t"); got.Type != domain.ChartTypeKPI {
		t.Fatalf("ожидался подбор по форме данных, получено %s", got.Type)
	}
}
