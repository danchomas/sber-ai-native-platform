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

type Headline struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`

	ChangePct *float64 `json:"change_pct,omitempty"`

	SharePct *float64 `json:"share_pct,omitempty"`
	Text     string   `json:"text"`
}
