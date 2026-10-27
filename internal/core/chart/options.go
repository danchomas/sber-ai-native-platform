package chart

import "github.com/heinousness/aibi/internal/core/domain"

var optionOrder = []domain.ChartType{
	domain.ChartTypeKPI, domain.ChartTypeBar, domain.ChartTypeHBar, domain.ChartTypeLine,
	domain.ChartTypeArea, domain.ChartTypePie, domain.ChartTypeDonut, domain.ChartTypeScatter,
	domain.ChartTypeHeatmap, domain.ChartTypeTable,
}

// Options — визуализации, которыми можно показать этот результат. По ним
// пользователь переключает вид кнопками, не спрашивая модель ещё раз
// («покажи в другом виде»). Совместимость решается по форме результата,
// как и в Pick: типы колонок и число строк.
func Options(current domain.ChartSpec, rs *domain.ResultSet) []domain.ChartSpec {
	if rs.Empty() {
		return nil
	}

	numeric := columnsOfKind(rs, func(c domain.ResultColumn) bool { return c.Type.IsNumeric() })
	temporal := columnsOfKind(rs, func(c domain.ResultColumn) bool { return c.Type.IsTemporal() })
	categorical := columnsOfKind(rs, func(c domain.ResultColumn) bool {
		return !c.Type.IsNumeric() && !c.Type.IsTemporal()
	})
	rows := len(rs.Rows)
	dims := len(categorical) + len(temporal)

	fits := func(t domain.ChartType) bool {
		switch t {
		case domain.ChartTypeTable:
			return true
		case domain.ChartTypeKPI:
			return rows == 1 && len(numeric) >= 1 && len(numeric) <= 4
		case domain.ChartTypeBar, domain.ChartTypeHBar:
			return rows >= 2 && rows <= 60 && len(numeric) >= 1 && (dims >= 1 || len(numeric) >= 2)
		case domain.ChartTypeLine, domain.ChartTypeArea:
			return rows >= 3 && len(numeric) >= 1 && (len(temporal) >= 1 || len(numeric) >= 2)
		case domain.ChartTypePie, domain.ChartTypeDonut:
			return rows >= 2 && rows <= 12 && len(numeric) >= 1 && dims == 1 && len(temporal) == 0
		case domain.ChartTypeScatter:
			return rows >= 5 && len(numeric) >= 2
		case domain.ChartTypeHeatmap:
			return rows >= 4 && len(numeric) >= 1 && dims >= 2
		}
		return false
	}

	var out []domain.ChartSpec
	for _, t := range optionOrder {
		if !fits(t) && t != current.Type {
			continue
		}
		candidate := current
		candidate.Type = t
		candidate.Reason = ""
		if t != current.Type {
			candidate.X, candidate.Y, candidate.Series, candidate.XLabel, candidate.YLabel = "", nil, "", "", ""
			if t == domain.ChartTypeHeatmap && len(categorical)+len(temporal) >= 2 {
				candidate.X = rs.Columns[firstOf(temporal, categorical)].Name
			}
		}
		spec := Normalize(&candidate, rs, current.Title)
		if spec.Type == t {
			out = append(out, spec)
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

func firstOf(groups ...[]int) int {
	for _, g := range groups {
		if len(g) > 0 {
			return g[0]
		}
	}
	return 0
}
