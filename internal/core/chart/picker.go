package chart

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/heinousness/aibi/internal/core/domain"
)

const maxCategories = 40

func Pick(rs *domain.ResultSet, title string) domain.ChartSpec {
	spec := domain.ChartSpec{Type: domain.ChartTypeTable, Title: title}
	if rs == nil || len(rs.Columns) == 0 || len(rs.Rows) == 0 {
		spec.Reason = "нет данных для визуализации"
		return spec
	}

	temporal := columnsOfKind(rs, func(c domain.ResultColumn) bool { return c.Type.IsTemporal() })
	numeric := columnsOfKind(rs, func(c domain.ResultColumn) bool { return c.Type.IsNumeric() })
	categorical := columnsOfKind(rs, func(c domain.ResultColumn) bool {
		return !c.Type.IsNumeric() && !c.Type.IsTemporal()
	})

	switch {
	case len(rs.Rows) == 1 && len(rs.Columns) == 1 && len(numeric) == 1:
		spec.Type = domain.ChartTypeKPI
		spec.Y = []string{rs.Columns[0].Name}
		spec.Reason = "единственное значение показано как показатель"

	case len(rs.Rows) == 1 && len(numeric) >= 1 && len(numeric) <= 4 && len(categorical) == 0:
		spec.Type = domain.ChartTypeKPI
		spec.Y = names(rs, numeric)
		spec.Reason = "строка агрегатов показана набором показателей"

	case len(temporal) >= 1 && len(numeric) >= 1:
		spec.Type = domain.ChartTypeLine
		spec.X = rs.Columns[temporal[0]].Name
		spec.Y = names(rs, numeric)
		if len(categorical) == 1 && len(numeric) == 1 {
			spec.Series = rs.Columns[categorical[0]].Name
		}
		spec.Reason = "временная колонка показана как динамика"

	case len(categorical) >= 1 && len(numeric) >= 1:
		distinct := distinctCount(rs, categorical[0])
		switch {
		case len(categorical) >= 2 && len(numeric) == 1 && distinct > 2:
			spec.Type = domain.ChartTypeBar
			spec.X = rs.Columns[categorical[0]].Name
			spec.Series = rs.Columns[categorical[1]].Name
			spec.Y = names(rs, numeric[:1])
			spec.Stacked = true
			spec.Reason = "две категориальные колонки показаны составными столбцами"
		case distinct > maxCategories:
			spec.Type = domain.ChartTypeTable
			spec.Reason = fmt.Sprintf("слишком много категорий (%d) для диаграммы", distinct)
		case distinct > 12:
			spec.Type = domain.ChartTypeHBar
			spec.X = rs.Columns[categorical[0]].Name
			spec.Y = names(rs, numeric)
			spec.Reason = "много категорий — горизонтальные столбцы читаются лучше"
		default:
			spec.Type = domain.ChartTypeBar
			spec.X = rs.Columns[categorical[0]].Name
			spec.Y = names(rs, numeric)
			spec.Reason = "сравнение значений по категориям"
		}

	case len(numeric) >= 2 && len(categorical) == 0 && len(temporal) == 0:
		spec.Type = domain.ChartTypeScatter
		spec.X = rs.Columns[numeric[0]].Name
		spec.Y = names(rs, numeric[1:2])
		spec.Reason = "две числовые колонки показаны диаграммой рассеяния"

	case len(numeric) == 1 && len(rs.Columns) == 1 && len(rs.Rows) > 10:
		spec.Type = domain.ChartTypeHistogram
		spec.X = rs.Columns[numeric[0]].Name
		spec.Reason = "распределение значений одной метрики"

	default:
		spec.Reason = "форма результата лучше читается таблицей"
	}

	if spec.X != "" {
		spec.XLabel = humanize(spec.X)
	}
	if len(spec.Y) == 1 {
		spec.YLabel = humanize(spec.Y[0])
	}
	return spec
}

const maxScaleRatio = 20

func sameScale(names []string, rs *domain.ResultSet) []string {
	if len(names) < 2 {
		return names
	}
	peak := func(name string) float64 {
		col, top := rs.ColumnIndex(name), 0.0
		for _, row := range rs.Rows {
			if v, ok := number(row[col]); ok {
				top = max(top, math.Abs(v))
			}
		}
		return top
	}
	base := peak(names[0])
	kept := names[:1:1]
	for _, name := range names[1:] {
		p := peak(name)
		if base == 0 || p == 0 || (p/base <= maxScaleRatio && base/p <= maxScaleRatio) {
			kept = append(kept, name)
		}
	}
	return kept
}

func Normalize(spec *domain.ChartSpec, rs *domain.ResultSet, title string) domain.ChartSpec {
	fallback := Pick(rs, title)
	if spec == nil {
		return fallback
	}

	out := *spec
	if out.Title == "" {
		out.Title = title
	}
	if !domain.KnownChartType(out.Type) {
		out.Type = fallback.Type
		out.Reason = "предложенный тип визуализации не поддерживается"
	}
	if out.Type == domain.ChartTypeTable || out.Type == domain.ChartTypeKPI {
		if out.Type == domain.ChartTypeKPI && len(out.Y) == 0 {
			out.Y = fallback.Y
		}
		return out
	}
	if rs == nil || len(rs.Rows) == 0 {
		return fallback
	}

	if out.X != "" && rs.ColumnIndex(out.X) < 0 {
		out.X = fallback.X
	}
	if out.Series != "" && !validSeries(out, rs) {
		out.Series = ""
	}
	y := make([]string, 0, len(out.Y))
	for _, name := range out.Y {
		if rs.ColumnIndex(name) >= 0 {
			y = append(y, name)
		}
	}
	out.Y = sameScale(y, rs)

	if out.X == "" && out.Type != domain.ChartTypeHistogram {
		out.X = fallback.X
	}
	if len(out.Y) == 0 {
		out.Y = fallback.Y
	}
	if out.X == "" && len(out.Y) == 0 {
		return fallback
	}

	if (out.Type == domain.ChartTypePie || out.Type == domain.ChartTypeDonut) && len(rs.Rows) > 12 {
		out.Type = domain.ChartTypeHBar
		out.Reason = "слишком много секторов для круговой диаграммы, показаны столбцы"
	}
	if out.XLabel == "" && out.X != "" {
		out.XLabel = humanize(out.X)
	}
	if out.YLabel == "" && len(out.Y) == 1 {
		out.YLabel = humanize(out.Y[0])
	}
	return out
}

func columnsOfKind(rs *domain.ResultSet, pred func(domain.ResultColumn) bool) []int {
	var out []int
	for i, c := range rs.Columns {
		if pred(c) {
			out = append(out, i)
		}
	}
	return out
}

func names(rs *domain.ResultSet, idx []int) []string {
	out := make([]string, 0, len(idx))
	for _, i := range idx {
		out = append(out, rs.Columns[i].Name)
	}
	return out
}

func distinctCount(rs *domain.ResultSet, col int) int {
	seen := make(map[string]struct{}, len(rs.Rows))
	for _, row := range rs.Rows {
		if col < len(row) {
			seen[fmt.Sprint(row[col])] = struct{}{}
		}
	}
	return len(seen)
}

func humanize(name string) string {
	s := strings.ReplaceAll(name, "_", " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return name
	}
	first, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(first)) + s[size:]
}

// validSeries проверяет разбивку на серии: это существующая нечисловая колонка,
// не совпадающая с осью X и показателями. Числовая колонка в роли серии
// превращает каждое значение в отдельную серию, и легенда показывает числа.
func validSeries(spec domain.ChartSpec, rs *domain.ResultSet) bool {
	idx := rs.ColumnIndex(spec.Series)
	if idx < 0 || rs.Columns[idx].Type.IsNumeric() || spec.Series == spec.X {
		return false
	}
	return !slices.Contains(spec.Y, spec.Series)
}
