package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/sqlguard"
)

const (
	toolSeasonality = "seasonality"
	toolCorrelate   = "correlate"
	toolForecast    = "forecast"
	toolAnomalies   = "find_anomalies"
)

const (
	analysisSeasonality = "seasonality"
	analysisCorrelation = "correlation"
	analysisForecast    = "forecast"
	analysisAnomalies   = "anomalies"
)

var (
	monthNames = []string{"Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
		"Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь"}
	weekdayNames = []string{"Понедельник", "Вторник", "Среда", "Четверг", "Пятница", "Суббота", "Воскресенье"}
)

func seriesSchema(extra schema, required ...string) schema {
	props := schema{
		"dataset": text("имя датасета"),
		"time": object(schema{
			"column": text("колонка с датой"),
			"grain":  enum([]analytics.Grain{analytics.GrainDay, analytics.GrainWeek, analytics.GrainMonth, analytics.GrainQuarter, analytics.GrainYear}, "шаг ряда"),
		}, "column", "grain"),
		"metric":  metricSchema("что считать за период"),
		"filters": filtersSchema(),
		"title":   text("заголовок результата"),
	}
	for k, v := range extra {
		props[k] = v
	}
	return object(props, append([]string{"dataset", "time"}, required...)...)
}

func insightTools() []struct {
	name, desc string
	params     schema
} {
	return []struct {
		name, desc string
		params     schema
	}{
		{
			toolSeasonality,
			"Сезонность: насколько каждый месяц, квартал или день недели выше или ниже среднего. " +
				"Для вопросов «что происходит каждый июль», «есть ли пики зимой», «какой паттерн повторяется каждые 12 месяцев».",
			seriesSchema(nil, "metric"),
		},
		{
			toolCorrelate,
			"Корреляция двух показателей по времени с лагом: «изменения X предшествуют росту Y на N периодов?». " +
				"Корреляция не доказывает причинность — скажи об этом пользователю.",
			seriesSchema(schema{
				"x":       metricSchema("что считать за период"),
				"y":       metricSchema("что считать за период"),
				"max_lag": schema{"type": "integer", "description": "наибольший сдвиг в периодах, до 24"},
			}, "x", "y"),
		},
		{
			toolForecast,
			"Прогноз показателя на будущие периоды по истории: тренд и сезонность с интервалом. " +
				"Для вопросов «какой рост ожидается в ближайшие кварталы». Для сценария «что если изменить X» есть what_if.",
			seriesSchema(schema{"horizon": schema{"type": "integer", "description": "на сколько периодов вперёд, до 36"}}, "metric"),
		},
		{
			toolAnomalies,
			"Выбросы во временном ряду: периоды, где показатель резко выше или ниже обычного.",
			seriesSchema(nil, "metric"),
		},
	}
}

type seriesArgs struct {
	Dataset string              `json:"dataset"`
	Time    analytics.TimeGroup `json:"time"`
	Metric  *analytics.Metric   `json:"metric"`
	Filters []analytics.Filter  `json:"filters"`
	Title   string              `json:"title"`
}

func (a seriesArgs) metric() analytics.Metric {
	if a.Metric == nil || a.Metric.Func == "" {
		return analytics.Metric{Func: analytics.FuncCount}
	}
	return *a.Metric
}

func (r *agentRun) series(ctx context.Context, a seriesArgs, metrics ...analytics.Metric) (*domain.ResultSet, string, []string, error) {
	q := analytics.Query{
		Dataset: a.Dataset, Time: &a.Time, Metrics: metrics,
		Filters: a.Filters, Limit: r.s.cfg.MaxRows,
	}
	compiled, err := analytics.Compile(q, r.catalog, r.s.cfg.MaxRows)
	if err != nil {
		return nil, "", nil, err
	}
	scope, checked, err := sqlguard.ResolveScope(compiled.SQL, r.scopes, r.s.catalog.Schema(), r.s.cfg.MaxRows)
	if err != nil {
		return nil, "", nil, err
	}
	rs, err := r.s.exec.Query(ctx, scope.Target, checked.SQL, r.s.cfg.MaxRows, compiled.Args...)
	if err != nil {
		return nil, "", nil, err
	}
	return rs, compiled.SQL, checked.Tables, nil
}

func (r *agentRun) insight(ctx context.Context, call domain.ToolCall) (string, int, error) {
	raw := normalizeArgs(call.Arguments, "filters")
	var a seriesArgs
	if err := decodeArgs(raw, &a); err != nil {
		return "", 0, err
	}
	if len(r.results) >= maxResultsPerRun {
		return "", 0, errors.New("достигнут предел запросов на один вопрос: подведи итог через answer")
	}

	switch call.Name {
	case toolSeasonality:
		return r.seasonality(ctx, a)
	case toolCorrelate:
		var extra struct {
			X      *analytics.Metric `json:"x"`
			Y      *analytics.Metric `json:"y"`
			MaxLag int               `json:"max_lag"`
		}
		if err := decodeArgs(raw, &extra); err != nil {
			return "", 0, err
		}
		if extra.X == nil || extra.Y == nil {
			return "", 0, errors.New("нужны оба показателя: x и y")
		}
		return r.correlate(ctx, a, *extra.X, *extra.Y, extra.MaxLag)
	case toolForecast:
		var extra struct {
			Horizon int `json:"horizon"`
		}
		if err := decodeArgs(raw, &extra); err != nil {
			return "", 0, err
		}
		return r.forecast(ctx, a, extra.Horizon)
	default:
		return r.anomalies(ctx, a)
	}
}

func (r *agentRun) seasonality(ctx context.Context, a seriesArgs) (string, int, error) {
	rs, sql, tables, err := r.series(ctx, a, a.metric())
	if err != nil {
		return "", 0, err
	}
	points, err := analytics.Series(rs, 1)
	if err != nil {
		return "", 0, err
	}
	indices, err := analytics.SeasonalIndices(points, a.Time.Grain)
	if err != nil {
		return "", 0, err
	}

	label := map[analytics.Grain]string{analytics.GrainMonth: "Месяц", analytics.GrainQuarter: "Квартал", analytics.GrainDay: "День недели"}[a.Time.Grain]
	out := &domain.ResultSet{Columns: []domain.ResultColumn{
		{Name: label, Type: domain.ColumnTypeText},
		{Name: "Среднее за период", Type: domain.ColumnTypeNumeric},
		{Name: "Отклонение от среднего, %", Type: domain.ColumnTypeNumeric},
		{Name: "Наблюдений", Type: domain.ColumnTypeInteger},
	}}
	peak, low := indices[0], indices[0]
	for _, ix := range indices {
		out.Rows = append(out.Rows, []any{
			seasonName(a.Time.Grain, ix.Position), round2(ix.Mean), round2((ix.Index - 1) * 100), int64(ix.Count),
		})
		if ix.Index > peak.Index {
			peak = ix
		}
		if ix.Index < low.Index {
			low = ix
		}
	}
	out.RowCount = len(out.Rows)

	var warnings []string
	if years := len(points) / max(len(indices), 1); years < 2 {
		warnings = append(warnings, "История короче двух полных сезонов: сезонность может быть случайной.")
	}
	res := r.register(orDefault(a.Title, "Сезонность"), sql, tables, out, warnings, analysisSeasonality,
		&domain.ChartSpec{Type: domain.ChartTypeBar, X: label, Y: []string{"Отклонение от среднего, %"}})

	payload := r.resultOutput(res)
	payload["summary"] = map[string]any{
		"peak":            seasonName(a.Time.Grain, peak.Position),
		"peak_above_pct":  round2((peak.Index - 1) * 100),
		"low":             seasonName(a.Time.Grain, low.Position),
		"low_below_pct":   round2((1 - low.Index) * 100),
		"periods_in_data": len(points),
	}
	return encodeOutput(payload), len(out.Rows), nil
}

func (r *agentRun) correlate(ctx context.Context, a seriesArgs, x, y analytics.Metric, maxLag int) (string, int, error) {
	if x.Label == "" {
		x.Label = "X"
	}
	if y.Label == "" {
		y.Label = "Y"
	}
	if x.Label == y.Label {
		y.Label += " (2)"
	}
	rs, sql, tables, err := r.series(ctx, a, x, y)
	if err != nil {
		return "", 0, err
	}
	xs, err := analytics.Series(rs, 1)
	if err != nil {
		return "", 0, err
	}
	ys, err := analytics.Series(rs, 2)
	if err != nil {
		return "", 0, err
	}

	maxLag = min(max(maxLag, 0), 24)
	lags := analytics.LagCorrelations(xs, ys, maxLag)
	if len(lags) == 0 {
		return "", 0, errors.New("слишком короткий или постоянный ряд: корреляцию не посчитать, возьми шаг мельче или период шире")
	}

	out := &domain.ResultSet{Columns: []domain.ResultColumn{
		{Name: "Сдвиг, периодов", Type: domain.ColumnTypeInteger},
		{Name: "Корреляция", Type: domain.ColumnTypeNumeric},
		{Name: "Пар периодов", Type: domain.ColumnTypeInteger},
	}}
	best := lags[0]
	for _, l := range lags {
		out.Rows = append(out.Rows, []any{int64(l.Lag), round2(l.R), int64(l.Pairs)})
		if math.Abs(l.R) > math.Abs(best.R) {
			best = l
		}
	}
	out.RowCount = len(out.Rows)

	res := r.register(orDefault(a.Title, "Связь «"+x.Label+"» и «"+y.Label+"»"), sql, tables, out,
		[]string{"Корреляция показывает совместное изменение, но не доказывает причинно-следственную связь."},
		analysisCorrelation, &domain.ChartSpec{Type: domain.ChartTypeBar, X: "Сдвиг, периодов", Y: []string{"Корреляция"}})

	payload := r.resultOutput(res)
	payload["summary"] = map[string]any{
		"best_lag":    best.Lag,
		"best_r":      round2(best.R),
		"strength":    strength(best.R),
		"x_leads_y":   best.Lag > 0,
		"lag_meaning": "сдвиг k означает, что «" + x.Label + "» в периоде t сравнивается с «" + y.Label + "» в периоде t+k",
	}
	return encodeOutput(payload), len(out.Rows), nil
}

func (r *agentRun) forecast(ctx context.Context, a seriesArgs, horizon int) (string, int, error) {
	if horizon <= 0 {
		horizon = map[analytics.Grain]int{analytics.GrainDay: 30, analytics.GrainWeek: 8, analytics.GrainMonth: 6, analytics.GrainQuarter: 4, analytics.GrainYear: 2}[a.Time.Grain]
	}
	rs, sql, tables, err := r.series(ctx, a, a.metric())
	if err != nil {
		return "", 0, err
	}
	points, err := analytics.Series(rs, 1)
	if err != nil {
		return "", 0, err
	}
	var cut bool
	if last, ok := r.lastDate(a.Dataset, a.Time.Column); ok {
		points, cut = analytics.CompletePeriods(points, a.Time.Grain, last)
	}
	forecast, summary, err := analytics.Forecast(points, a.Time.Grain, horizon)
	if err != nil {
		return "", 0, err
	}

	period := rs.Columns[0].Name
	out := &domain.ResultSet{Columns: []domain.ResultColumn{
		{Name: period, Type: domain.ColumnTypeDate},
		{Name: "Факт", Type: domain.ColumnTypeNumeric},
		{Name: "Прогноз", Type: domain.ColumnTypeNumeric},
		{Name: "Нижняя граница", Type: domain.ColumnTypeNumeric},
		{Name: "Верхняя граница", Type: domain.ColumnTypeNumeric},
	}}
	for _, p := range forecast {
		out.Rows = append(out.Rows, []any{p.Period, optional(p.Actual), optional(p.Forecast), optional(p.Lower), optional(p.Upper)})
	}
	out.RowCount = len(out.Rows)

	warnings := []string{fmt.Sprintf("Прогноз построен моделью «%s» по %d периодам истории и предполагает, что условия не изменятся.",
		summary.Method, len(points))}
	if cut {
		warnings = append(warnings, "Последний период в данных неполный, поэтому в расчёт не вошёл.")
	}
	res := r.register(orDefault(a.Title, "Прогноз"), sql, tables, out, warnings, analysisForecast,
		&domain.ChartSpec{Type: domain.ChartTypeLine, X: period, Y: []string{"Факт", "Прогноз", "Нижняя граница", "Верхняя граница"}})

	payload := r.resultOutput(res)
	payload["summary"] = map[string]any{
		"method":             summary.Method,
		"seasonal":           summary.Seasonal,
		"history_periods":    len(points),
		"horizon":            horizon,
		"last_actual":        round2(summary.LastActual),
		"forecast_at_end":    round2(summary.LastValue),
		"growth_pct":         round2(summary.Growth * 100),
		"fit_error_pct":      round2(summary.FitError * 100),
		"interval_meaning":   "границы — примерно 95% интервал",
		"history_rows_shown": "строки с прогнозом — в конце результата",
	}
	return encodeOutput(payload), len(out.Rows), nil
}

func (r *agentRun) lastDate(dataset, column string) (time.Time, bool) {
	c, ok := r.findColumn(dataset, column)
	if !ok || c.Profile == nil || c.Profile.MaxValue == nil {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02 15:04:05-07", time.DateOnly, time.RFC3339} {
		if t, err := time.Parse(layout, *c.Profile.MaxValue); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func (r *agentRun) anomalies(ctx context.Context, a seriesArgs) (string, int, error) {
	rs, sql, tables, err := r.series(ctx, a, a.metric())
	if err != nil {
		return "", 0, err
	}
	points, err := analytics.Series(rs, 1)
	if err != nil {
		return "", 0, err
	}
	found := analytics.Anomalies(points)

	period := rs.Columns[0].Name
	out := &domain.ResultSet{Columns: []domain.ResultColumn{
		{Name: period, Type: domain.ColumnTypeDate},
		{Name: "Значение", Type: domain.ColumnTypeNumeric},
		{Name: "Типичное значение", Type: domain.ColumnTypeNumeric},
		{Name: "Отклонение", Type: domain.ColumnTypeText},
	}}
	for _, an := range found {
		direction := "выше обычного"
		if an.Score < 0 {
			direction = "ниже обычного"
		}
		out.Rows = append(out.Rows, []any{an.Period, round2(an.Value), round2(an.Expected), direction})
	}
	out.RowCount = len(out.Rows)

	res := r.register(orDefault(a.Title, "Выбросы"), sql, tables, out, nil, analysisAnomalies,
		&domain.ChartSpec{Type: domain.ChartTypeTable})
	payload := r.resultOutput(res)
	payload["summary"] = map[string]any{
		"periods_checked": len(points),
		"anomalies":       len(found),
		"method":          "устойчивое отклонение от медианы (порог 3.5)",
	}
	if len(found) == 0 {
		payload["note"] = "выбросов не найдено: все периоды в пределах обычного разброса"
	}
	return encodeOutput(payload), len(out.Rows), nil
}

func (r *agentRun) register(title, sql string, tables []string, rs *domain.ResultSet,
	warnings []string, analysis string, spec *domain.ChartSpec,
) *agentResult {
	spec.Title = title
	res := &agentResult{
		id: "r" + strconv.Itoa(len(r.results)+1), title: title, sql: sql,
		result: rs, tables: tables, warnings: warnings, analysis: analysis, chart: spec,
	}
	r.results = append(r.results, res)
	return res
}

func seasonName(grain analytics.Grain, pos int) string {
	switch grain {
	case analytics.GrainMonth:
		if pos >= 1 && pos <= 12 {
			return monthNames[pos-1]
		}
	case analytics.GrainDay:
		if pos >= 1 && pos <= 7 {
			return weekdayNames[pos-1]
		}
	case analytics.GrainQuarter:
		return strconv.Itoa(pos) + " квартал"
	}
	return strconv.Itoa(pos)
}

func strength(r float64) string {
	switch a := math.Abs(r); {
	case a >= 0.7:
		return "сильная"
	case a >= 0.4:
		return "умеренная"
	case a >= 0.2:
		return "слабая"
	}
	return "практически отсутствует"
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

func optional(f *float64) any {
	if f == nil {
		return nil
	}
	return round2(*f)
}
