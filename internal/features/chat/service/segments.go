package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/sqlguard"
)

const (
	toolCompare = "compare_segments"
	toolWhatIf  = "what_if"
	toolRecalc  = "recalculate"

	analysisWhatIf = "what_if"

	maxSegments        = 12
	maxCompareMeasures = 4
)

func segmentTools() []struct {
	name, desc string
	params     schema
} {
	return []struct {
		name, desc string
		params     schema
	}{
		{
			toolCompare,
			"Сравнить сегменты: группы записей по одному-двум признакам («тип + роль»). Считает размер, долю и средние " +
				"показатели каждого сегмента и находит, в чём сегменты отличаются сильнее всего. " +
				"Для вопросов «как группируются пользователи по типу и роли», «что отличает X от Y».",
			object(schema{
				"dataset":  text("имя датасета"),
				"segments": list(schema{"type": "string"}, "колонки-признаки сегмента, 1-2"),
				"values":   schema{"type": "array", "description": "какие значения сравнить, например [\"X\", \"Y\"]; пусто — крупнейшие сегменты"},
				"metrics":  list(metricSchema("что считать"), "что сравнить; пусто — средние всех числовых показателей"),
				"filters":  filtersSchema(),
				"title":    text("заголовок результата"),
			}, "dataset", "segments"),
		},
		{
			toolWhatIf,
			"Зависимость по истории: как показатель-цель реагировал на показатель-драйвер и что будет с целью, если драйвер изменить на change_pct процентов. " +
				"Для вопросов о реакции и зависимости («как спрос реагирует на цену»). Чтобы просто пересчитать показатель при новой цене, есть recalculate. " +
				"Это оценка по прошлой зависимости, а не доказанная причинность — скажи об этом.",
			seriesSchema(schema{
				"driver":     metricSchema("драйвер"),
				"target":     metricSchema("цель"),
				"change_pct": schema{"type": "number", "description": "изменение драйвера в процентах, например 10 или -5"},
			}, "driver", "target", "change_pct"),
		},
		{
			toolRecalc,
			"Пересчёт показателя при изменении колонки на change_pct процентов и неизменном объёме. " +
				"Для вопросов «как изменится выручка, если поднять цену товаров категории X на 10%». " +
				"Изменение применяется к строкам из filters, а итоги в результате — по всем данным: так и называй их в ответе.",
			object(schema{
				"dataset":    text("имя датасета"),
				"target":     metricSchema("обычно sum"),
				"column":     text("числовая колонка, которая меняется, например цена; должна входить в формулу target"),
				"change_pct": schema{"type": "number", "description": "изменение колонки в процентах, например 10 или -5"},
				"filters":    filtersSchema(),
				"title":      text("заголовок результата"),
			}, "dataset", "target", "column", "change_pct"),
		},
	}
}

func (r *agentRun) compareSegments(ctx context.Context, raw []byte) (string, int, error) {
	var args struct {
		Dataset  string             `json:"dataset"`
		Segments []string           `json:"segments"`
		Values   []any              `json:"values"`
		Metrics  []analytics.Metric `json:"metrics"`
		Filters  []analytics.Filter `json:"filters"`
		Title    string             `json:"title"`
	}
	if err := decodeArgs(normalizeArgs(raw, "segments", "values", "metrics", "filters"), &args); err != nil {
		return "", 0, err
	}
	if len(args.Segments) == 0 || len(args.Segments) > 2 {
		return "", 0, errors.New("укажи 1-2 колонки-признака сегмента в segments")
	}

	metrics := []analytics.Metric{
		{Func: analytics.FuncCount, Label: "Записей"},
		{Func: analytics.FuncShare, Label: "Доля, %"},
	}
	if len(args.Metrics) > 0 {
		for _, m := range args.Metrics {
			fn := analytics.Func(strings.ToLower(strings.TrimSpace(string(m.Func))))
			if (fn == analytics.FuncCount || fn == analytics.FuncShare) && m.Column == "" && m.Expr == "" && m.Per == "" && m.Duration == nil {
				continue
			}
			metrics = append(metrics, m)
		}
	} else {
		metrics = append(metrics, r.measureAverages(args.Dataset)...)
	}
	if len(metrics) > analytics.MaxMetrics {
		metrics = metrics[:analytics.MaxMetrics]
	}

	filters := args.Filters
	if len(args.Values) > 0 {
		if len(args.Segments) != 1 {
			return "", 0, errors.New("values можно задать только для одного признака сегмента")
		}
		filters = append(filters, analytics.Filter{Column: args.Segments[0], Op: analytics.OpIn, Values: args.Values})
	}

	q := analytics.Query{
		Dataset: args.Dataset, GroupBy: args.Segments, Metrics: metrics, Filters: filters,
		Sort: &analytics.Sort{By: "Записей", Desc: true}, Limit: maxSegments,
		Subtotals: len(args.Segments) == 2 && r.fewCombinations(args.Dataset, args.Segments),
	}
	if q.Subtotals {
		q.Limit = 2 * maxSegments
	}
	compiled, err := analytics.Compile(q, r.catalog, r.s.cfg.MaxRows)
	if err != nil {
		return "", 0, err
	}
	title := orDefault(args.Title, "Сравнение сегментов по «"+strings.Join(args.Segments, " + ")+"»")
	_, rows, err := r.run(ctx, compiled, &q, title)
	if err != nil {
		return "", 0, err
	}

	res := r.results[len(r.results)-1]
	res.segments = true
	payload := r.resultOutput(res)
	if q.Subtotals {
		payload["subtotals"] = "строки «" + analytics.SubtotalAll + "» и «" + analytics.SubtotalTotal + "» — подытоги и общий итог, посчитанные базой: бери их, а не складывай сам"
	}
	if diffs := segmentDifferences(res.result, len(args.Segments)); len(diffs) > 0 {
		payload["key_differences"] = diffs
	} else {
		payload["note"] = "сегментов меньше двух: сравнивать не с чем"
	}
	return encodeOutput(payload), rows, nil
}

func (r *agentRun) fewCombinations(dataset string, segments []string) bool {
	combinations := int64(1)
	for _, name := range segments {
		column, ok := r.findColumn(dataset, name)
		if !ok || column.Profile == nil || column.Profile.DistinctCount == 0 {
			return false
		}
		combinations *= column.Profile.DistinctCount
	}
	return combinations <= maxSegments
}

func (r *agentRun) findColumn(dataset, name string) (domain.Column, bool) {
	for _, d := range r.datasets {
		if !strings.EqualFold(d.Name, dataset) && !strings.EqualFold(d.TableName, dataset) {
			continue
		}
		for _, c := range d.Columns {
			if strings.EqualFold(c.Name, name) || strings.EqualFold(c.SourceName, name) {
				return c, true
			}
		}
	}
	return domain.Column{}, false
}

func (r *agentRun) measureAverages(dataset string) []analytics.Metric {
	var out []analytics.Metric
	for _, d := range r.datasets {
		if !strings.EqualFold(d.Name, dataset) && !strings.EqualFold(d.TableName, dataset) {
			continue
		}
		for _, c := range d.ColumnsByRole(domain.RoleMeasure) {
			if len(out) == maxCompareMeasures {
				break
			}
			out = append(out, analytics.Metric{Func: analytics.FuncAvg, Column: c.Name, Label: "Среднее " + label(c)})
		}
	}
	return out
}

func segmentDifferences(rs *domain.ResultSet, keys int) []map[string]any {
	if rs == nil || len(rs.Rows) < 2 {
		return nil
	}
	type diff struct {
		metric          string
		high, low       string
		highVal, lowVal float64
		gapPct          float64
	}
	var diffs []diff
	for col := keys; col < len(rs.Columns); col++ {
		if !rs.Columns[col].Type.IsNumeric() {
			continue
		}
		var d diff
		found := false
		for _, row := range rs.Rows {
			v, ok := toNumber(row[col])
			if !ok || isSubtotalRow(row[:keys]) {
				continue
			}
			name := segmentName(row[:keys])
			if !found || v > d.highVal {
				d.high, d.highVal = name, v
			}
			if !found || v < d.lowVal {
				d.low, d.lowVal = name, v
			}
			found = true
		}
		if !found || d.high == d.low {
			continue
		}
		d.metric = rs.Columns[col].Name
		if d.lowVal != 0 {
			d.gapPct = (d.highVal - d.lowVal) / math.Abs(d.lowVal) * 100
		}
		diffs = append(diffs, d)
	}
	sort.SliceStable(diffs, func(i, j int) bool { return math.Abs(diffs[i].gapPct) > math.Abs(diffs[j].gapPct) })

	out := make([]map[string]any, 0, len(diffs))
	for _, d := range diffs {
		item := map[string]any{
			"metric":  d.metric,
			"highest": map[string]any{"segment": d.high, "value": round2(d.highVal)},
			"lowest":  map[string]any{"segment": d.low, "value": round2(d.lowVal)},
		}
		if d.lowVal != 0 {
			item["gap_pct"] = math.Round(d.gapPct)
		}
		out = append(out, item)
	}
	return out
}

func isSubtotalRow(keys []any) bool {
	for _, k := range keys {
		if k == analytics.SubtotalAll || k == analytics.SubtotalTotal {
			return true
		}
	}
	return false
}

func segmentName(keys []any) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = formatCell(k)
	}
	return strings.Join(parts, " + ")
}

func toNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, !math.IsNaN(x) && !math.IsInf(x, 0)
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

func (r *agentRun) whatIf(ctx context.Context, raw []byte) (string, int, error) {
	raw = normalizeArgs(raw, "filters")
	var a seriesArgs
	if err := decodeArgs(raw, &a); err != nil {
		return "", 0, err
	}
	var extra struct {
		Driver    *analytics.Metric `json:"driver"`
		Target    *analytics.Metric `json:"target"`
		ChangePct *float64          `json:"change_pct"`
	}
	if err := decodeArgs(raw, &extra); err != nil {
		return "", 0, err
	}
	if extra.Driver == nil || extra.Target == nil || extra.ChangePct == nil {
		return "", 0, errors.New("нужны driver, target и change_pct")
	}
	change := *extra.ChangePct
	if change <= -100 || change > 1000 {
		return "", 0, errors.New("change_pct должен быть больше -100 и не больше 1000")
	}
	driver, target := *extra.Driver, *extra.Target
	driver.Label = orDefault(driver.Label, "Драйвер")
	target.Label = orDefault(target.Label, "Цель")
	if driver.Label == target.Label {
		target.Label += " (цель)"
	}

	rs, sql, tables, err := r.series(ctx, a, driver, target)
	if err != nil {
		return "", 0, err
	}
	var xs, ys []float64
	for _, row := range rs.Rows {
		x, okX := toNumber(row[1])
		y, okY := toNumber(row[2])
		if okX && okY {
			xs, ys = append(xs, x), append(ys, y)
		}
	}
	reg, err := analytics.Fit(xs, ys)
	if err != nil {
		return "", 0, err
	}

	x0, y0 := xs[len(xs)-1], ys[len(ys)-1]
	x1 := x0 * (1 + change/100)
	y1 := y0 + reg.Slope*(x1-x0)
	var effectPct float64
	if y0 != 0 {
		effectPct = (y1 - y0) / math.Abs(y0) * 100
	}

	out := &domain.ResultSet{
		Columns: []domain.ResultColumn{
			{Name: "Вариант", Type: domain.ColumnTypeText},
			{Name: driver.Label, Type: domain.ColumnTypeNumeric},
			{Name: target.Label, Type: domain.ColumnTypeNumeric},
		},
		Rows: [][]any{
			{"Сейчас (последний период)", round2(x0), round2(y0)},
			{fmt.Sprintf("Если «%s» %s", driver.Label, signedPct(change)), round2(x1), round2(y1)},
		},
		RowCount: 2,
	}
	warnings := []string{
		"Это оценка по прошлой зависимости между показателями, а не доказанная причинно-следственная связь.",
	}
	if reg.R2 < 0.3 {
		warnings = append(warnings, fmt.Sprintf(
			"Связь слабая: «%s» объясняет лишь %.0f%% колебаний «%s», оценка ненадёжна.", driver.Label, reg.R2*100, target.Label))
	}

	title := orDefault(a.Title, "Что будет с «"+target.Label+"», если изменить «"+driver.Label+"»")
	res := r.register(title, sql, tables, out, warnings, analysisWhatIf,
		&domain.ChartSpec{Type: domain.ChartTypeBar, X: "Вариант", Y: []string{target.Label}})

	payload := r.resultOutput(res)
	payload["summary"] = map[string]any{
		"change_pct":        change,
		"target_change_pct": round2(effectPct),
		"target_now":        round2(y0),
		"target_scenario":   round2(y1),
		"slope":             round2(reg.Slope),
		"elasticity":        round2(reg.Elasticity),
		"r2":                round2(reg.R2),
		"periods":           reg.Pairs,
		"meaning": fmt.Sprintf("при изменении «%s» на 1%% «%s» в среднем меняется на %.2f%%",
			driver.Label, target.Label, reg.Elasticity),
	}
	return encodeOutput(payload), len(out.Rows), nil
}

func (r *agentRun) recalculate(ctx context.Context, raw []byte) (string, int, error) {
	var args struct {
		Dataset   string             `json:"dataset"`
		Target    *analytics.Metric  `json:"target"`
		Column    string             `json:"column"`
		ChangePct *float64           `json:"change_pct"`
		Filters   []analytics.Filter `json:"filters"`
		Title     string             `json:"title"`
	}
	if err := decodeArgs(normalizeArgs(raw, "filters"), &args); err != nil {
		return "", 0, err
	}
	if args.Target == nil || strings.TrimSpace(args.Column) == "" || args.ChangePct == nil {
		return "", 0, errors.New("нужны target, column и change_pct")
	}
	change := *args.ChangePct
	if change <= -100 || change > 1000 {
		return "", 0, errors.New("change_pct должен быть больше -100 и не больше 1000")
	}

	base := *args.Target
	base.Label = orDefault(base.Label, "Показатель")
	if strings.TrimSpace(base.Expr) == "" {
		if base.Column == "" {
			return "", 0, errors.New("в target нужна формула expr или колонка")
		}
		base.Expr, base.Column = `"`+base.Column+`"`, ""
	}
	scaled := base
	scaled.Label = "Сценарий"
	scaled.Scale = &analytics.Scale{Column: args.Column, Factor: 1 + change/100}

	totals, sql, tables, err := r.scalars(ctx, args.Dataset, nil, base)
	if err != nil {
		return "", 0, err
	}
	affected, affectedSQL, _, err := r.scalars(ctx, args.Dataset, args.Filters, base, scaled)
	if err != nil {
		return "", 0, err
	}
	now := totals[0]
	scenario := now - affected[0] + affected[1]

	out := &domain.ResultSet{
		Columns: []domain.ResultColumn{
			{Name: "Вариант", Type: domain.ColumnTypeText},
			{Name: base.Label, Type: domain.ColumnTypeNumeric},
		},
		Rows: [][]any{
			{"Сейчас, всего", round2(now)},
			{"Сценарий " + signedPct(change) + ", всего", round2(scenario)},
		},
		RowCount: 2,
	}
	warnings := []string{"Расчёт при неизменном объёме: реакция покупателей на изменение не учтена."}
	title := orDefault(args.Title, "Пересчёт «"+base.Label+"» при изменении на "+signedPct(change))
	res := r.register(title, sql+";\n"+affectedSQL, tables, out, warnings, analysisWhatIf,
		&domain.ChartSpec{Type: domain.ChartTypeBar, X: "Вариант", Y: []string{base.Label}})

	payload := r.resultOutput(res)
	summary := map[string]any{
		"total_now":      round2(now),
		"total_scenario": round2(scenario),
		"delta":          round2(scenario - now),
		"affected_now":   round2(affected[0]),
		"meaning":        "total — показатель по всем данным, affected — только по строкам из filters",
		"assumption":     "объём не меняется",
	}
	if now != 0 {
		summary["total_delta_pct"] = round2((scenario - now) / math.Abs(now) * 100)
		summary["affected_share_pct"] = round2(affected[0] / math.Abs(now) * 100)
	}
	payload["summary"] = summary
	return encodeOutput(payload), len(out.Rows), nil
}

func (r *agentRun) scalars(ctx context.Context, dataset string, filters []analytics.Filter, metrics ...analytics.Metric) ([]float64, string, []string, error) {
	q := analytics.Query{Dataset: dataset, Metrics: metrics, Filters: filters}
	compiled, err := analytics.Compile(q, r.catalog, r.s.cfg.MaxRows)
	if err != nil {
		return nil, "", nil, err
	}
	scope, checked, err := sqlguard.ResolveScope(compiled.SQL, r.scopes, r.s.catalog.Schema(), r.s.cfg.MaxRows)
	if err != nil {
		return nil, "", nil, err
	}
	rs, err := r.s.exec.Query(ctx, scope.Target, checked.SQL, 1, compiled.Args...)
	if err != nil {
		return nil, "", nil, err
	}
	if len(rs.Rows) == 0 {
		return nil, "", nil, errors.New("нет строк для расчёта: проверь фильтры")
	}
	values := make([]float64, len(metrics))
	for i := range metrics {
		v, ok := toNumber(rs.Rows[0][i])
		if !ok {
			return nil, "", nil, errors.New("нет строк для расчёта: проверь фильтры")
		}
		values[i] = v
	}
	return values, compiled.SQL, checked.Tables, nil
}

func signedPct(v float64) string {
	if v > 0 {
		return fmt.Sprintf("+%g%%", v)
	}
	return fmt.Sprintf("%g%%", v)
}
