package domain

type ChartType string

const (
	ChartTypeTable     ChartType = "table"
	ChartTypeKPI       ChartType = "kpi"
	ChartTypeBar       ChartType = "bar"
	ChartTypeHBar      ChartType = "hbar"
	ChartTypeLine      ChartType = "line"
	ChartTypeArea      ChartType = "area"
	ChartTypePie       ChartType = "pie"
	ChartTypeDonut     ChartType = "donut"
	ChartTypeScatter   ChartType = "scatter"
	ChartTypeHistogram ChartType = "histogram"
	ChartTypeHeatmap   ChartType = "heatmap"
)

func KnownChartType(t ChartType) bool {
	switch t {
	case ChartTypeTable, ChartTypeKPI, ChartTypeBar, ChartTypeHBar, ChartTypeLine,
		ChartTypeArea, ChartTypePie, ChartTypeDonut, ChartTypeScatter,
		ChartTypeHistogram, ChartTypeHeatmap:
		return true
	}
	return false
}

type ChartSpec struct {
	Type     ChartType `json:"type"`
	Title    string    `json:"title,omitempty"`
	Subtitle string    `json:"subtitle,omitempty"`
	X        string    `json:"x,omitempty"`
	Y        []string  `json:"y,omitempty"`
	Series   string    `json:"series,omitempty"`
	Stacked  bool      `json:"stacked,omitempty"`
	XLabel   string    `json:"x_label,omitempty"`
	YLabel   string    `json:"y_label,omitempty"`
	Unit     string    `json:"unit,omitempty"`
	Reason   string    `json:"reason,omitempty"`
}

// Headline — главное в результате: одно число и фраза для
// бизнес-пользователя. Считается платформой по строкам результата,
// а не моделью, поэтому числа в ней всегда совпадают с графиком.
type Headline struct {
	// Label — что это за число: «Последний период · 06.2024», «Лидер · Москва».
	Label string  `json:"label"`
	Value float64 `json:"value"`
	// ChangePct — изменение к предыдущему периоду в процентах, для динамики.
	ChangePct *float64 `json:"change_pct,omitempty"`
	// SharePct — доля лидера в сумме, для сравнения категорий.
	SharePct *float64 `json:"share_pct,omitempty"`
	Text     string   `json:"text"`
}
