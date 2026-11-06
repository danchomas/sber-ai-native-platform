package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/sqlguard"
)

const (
	toolDrivers     = "find_drivers"
	analysisDrivers = "drivers"

	maxDriverColumns  = 8
	maxDriverValues   = 40
	minDriverRows     = 30
	minDriverShare    = 0.01
	driverZ           = 3.0
	driverShownDims   = 4
	driverDetailDims  = 2
	driverDetailRows  = 12
	maxDriverFilters  = analytics.MaxMetricConds
	driverOverallName = "Все записи"
	// driverOverallValue — значение строки итога: график подписывает
	// столбцы значениями, и «—» на оси ничего не говорит.
	driverOverallValue = "В целом"

	// Признак повторяет событие, если в значениях, где событие у всех записей
	// (или ни у одной) с точностью proxyPurity, лежит не меньше proxyCover
	// событий (и остальных записей).
	proxyPurity = 0.005
	proxyCover  = 0.95
)

func driverTools() []struct {
	name, desc string
	params     schema
} {
	return []struct {
		name, desc string
		params     schema
	}{{
		toolDrivers,
		"Найти условия, при которых показатель заметно выше или ниже: перебирает разрезы (категории) и календарные признаки " +
			"(квартал и месяц года, день недели, час) и ранжирует их по силе влияния с учётом числа записей и случайного разброса. " +
			"С group_a и group_b ищет, где сильнее всего меняется разница между двумя группами записей, например крупными и мелкими задачами. " +
			"Для вопросов «при каких условиях», «от чего зависит», «что влияет на», «когда X больше».",
		object(schema{
			"dataset": text("имя датасета"),
			"metric":  metricSchema("обычно avg: средняя длительность, сумма, доля"),
			"filters": filtersSchema(),
			"group_a": conditionsSchema("условия группы А, например плановые трудозатраты gte 16 — крупные задачи"),
			"group_b": conditionsSchema("условия группы Б для сравнения, например плановые трудозатраты lte 4; пусто — все записи"),
			"label_a": text("подпись группы А, например «Крупные задачи»"),
			"label_b": text("подпись группы Б"),
			"dimensions": list(schema{"type": "string"},
				"какие колонки-признаки проверить; пусто — все подходящие категории датасета"),
			"date_column": text("колонка с датой для календарных признаков; пусто — первая колонка с датой"),
			"title":       text("заголовок результата"),
		}, "dataset", "metric"),
	}}
}

type driversArgs struct {
	Dataset    string             `json:"dataset"`
	Metric     *analytics.Metric  `json:"metric"`
	Filters    []analytics.Filter `json:"filters"`
	GroupA     []analytics.Filter `json:"group_a"`
	GroupB     []analytics.Filter `json:"group_b"`
	LabelA     string             `json:"label_a"`
	LabelB     string             `json:"label_b"`
	Dimensions []string           `json:"dimensions"`
	DateColumn string             `json:"date_column"`
	Title      string             `json:"title"`
}

func (a driversArgs) compare() bool { return len(a.GroupA) > 0 }

// driverDim — один проверяемый признак: колонка-разрез или календарная часть даты.
type driverDim struct {
	name   string
	column string
	grain  analytics.Grain
}

func (d driverDim) query(base analytics.Query) analytics.Query {
	if d.grain != "" {
		base.Time = &analytics.TimeGroup{Column: d.column, Grain: d.grain}
	} else {
		base.GroupBy = []string{d.column}
	}
	return base
}

func (d driverDim) valueName(v any) string {
	n, isNum := numberOf(v)
	if d.grain == "" || !isNum {
		return fmt.Sprint(v)
	}
	pos := int(n)
	switch d.grain {
	case analytics.GrainQuarterOfYear:
		return fmt.Sprintf("%d квартал", pos)
	case analytics.GrainMonthOfYear:
		if pos >= 1 && pos <= 12 {
			return monthNames[pos-1]
		}
	case analytics.GrainDayOfWeek:
		if pos >= 1 && pos <= 7 {
			return weekdayNames[pos-1]
		}
	case analytics.GrainHour:
		return fmt.Sprintf("%02d:00–%02d:59", pos, pos)
	}
	return fmt.Sprint(v)
}

// driverStats — показатель группы и его точность. В режиме сравнения
// value — относительная разница групп А и Б, иначе — значение показателя.
type driverStats struct {
	n         float64
	a, b      float64
	value     float64
	se        float64
	hasSE     bool
	defined   bool
	nB        float64
	valueName string
}

type driverResult struct {
	dim    driverDim
	values []driverStats
	score  float64
	signif int
}

func (r *agentRun) findDrivers(ctx context.Context, raw []byte) (string, int, error) {
	var args driversArgs
	if err := decodeArgs(normalizeArgs(raw, "filters", "group_a", "group_b", "dimensions"), &args); err != nil {
		return "", 0, err
	}
	if args.Metric == nil || args.Metric.Func == "" {
		return "", 0, errors.New("укажи metric: что сравнивать, например avg длительности")
	}
	if len(args.Metric.Filters)+max(len(args.GroupA), len(args.GroupB)) > maxDriverFilters {
		return "", 0, fmt.Errorf("условий метрики и группы вместе не больше %d", maxDriverFilters)
	}
	dataset, ok := r.datasetByName(args.Dataset)
	if !ok {
		return "", 0, fmt.Errorf("датасета %q нет в каталоге", args.Dataset)
	}
	dims, err := r.driverDims(dataset, args)
	if err != nil {
		return "", 0, err
	}

	metrics := driverMetrics(*args.Metric, args)
	base := analytics.Query{Dataset: dataset.Name, Filters: args.Filters, Metrics: metrics, Limit: maxDriverValues + 1}
	overallRows, sql, tables, err := r.driverQuery(ctx, base)
	if err != nil {
		return "", 0, err
	}
	if len(overallRows.Rows) != 1 {
		return "", 0, errors.New("не удалось посчитать показатель по всем записям: проверь metric и filters")
	}
	overall := driverRow(overallRows.Rows[0], 0, args.compare(), isRate(*args.Metric))
	if !overall.defined {
		return "", 0, errors.New("показатель не определён: в группах нет записей или значения пустые, проверь условия групп")
	}

	minRows := math.Max(minDriverRows, minDriverShare*overall.n)
	rate := isRate(*args.Metric)
	var results []driverResult
	var skipped, proxies []string
	for _, d := range dims {
		rs, _, _, err := r.driverQuery(ctx, d.query(base))
		if err != nil {
			skipped = append(skipped, d.name)
			r.s.log.DebugContext(ctx, "признак пропущен", "dimension", d.name, "error", err)
			continue
		}
		res := driverResult{dim: d}
		all := make([]driverStats, 0, len(rs.Rows))
		for _, row := range rs.Rows {
			st := driverRow(row, 1, args.compare(), rate)
			st.valueName = d.valueName(row[0])
			if row[0] == nil {
				st.valueName = "(пусто)"
			}
			all = append(all, st)
			if !st.defined || st.n < minRows || (args.compare() && st.nB < minRows) {
				continue
			}
			res.values = append(res.values, st)
		}
		if rate && outcomeProxy(all) {
			proxies = append(proxies, d.name)
			continue
		}
		if len(res.values) < 2 {
			continue
		}
		res.score, res.signif = driverScore(res.values, overall, args.compare())
		results = append(results, res)
	}
	if len(results) == 0 {
		if len(proxies) > 0 {
			return "", 0, proxyError(r.filter, proxies)
		}
		return "", 0, errors.New("ни один признак не дал групп достаточного размера: ослабь условия или укажи dimensions")
	}
	slices.SortStableFunc(results, func(x, y driverResult) int { return cmp.Compare(y.score, x.score) })

	labelA := orDefault(args.LabelA, "Группа А")
	labelB := orDefault(args.LabelB, "Группа Б")
	if args.compare() && len(args.GroupB) == 0 {
		labelB = orDefault(args.LabelB, "Все записи")
	}
	metricLabel := orDefault(args.Metric.Label, "Показатель")
	out := driversTable(results, overall, args.compare(), labelA, labelB, metricLabel)
	res := r.register(orDefault(args.Title, "Условия, от которых зависит показатель"), sql, tables, out, nil,
		analysisDrivers, &domain.ChartSpec{Type: domain.ChartTypeTable})

	payload := r.resultOutput(res)
	payload["summary"] = driversSummary(r.filter, results, overall, args.compare(), labelA, labelB)
	payload["how_to_read"] = driversHint(args.compare())
	if len(skipped) > 0 {
		for i := range skipped {
			skipped[i] = r.filter.clean(skipped[i], maxNameRunes)
		}
		payload["skipped"] = skipped
	}
	if len(proxies) > 0 {
		payload["excluded"] = proxyNote(r.filter, proxies)
	}
	return encodeOutput(payload), len(out.Rows), nil
}

// outcomeProxy — признак, который почти целиком определяет само событие:
// события приходятся на значения, где оно у всех записей, остальные записи —
// на значения, где его нет ни у одной («Статус» или «Описание ошибки» при
// частоте ошибок). Это не условие, а другое имя события: среди условий он
// всегда первый, а внутри его значений разницы групп нет по построению,
// поэтому он «объяснял» бы любую разницу. Значения — частоты в процентах.
func outcomeProxy(values []driverStats) bool {
	var events, rest, pureEvents, pureRest float64
	for _, v := range values {
		n := v.n + v.nB
		if n <= 0 {
			continue
		}
		p := (v.a*v.n + v.b*v.nB) / n / 100
		k := p * n
		events += k
		rest += n - k
		switch {
		case p >= 1-proxyPurity:
			pureEvents += k
		case p <= proxyPurity:
			pureRest += n - k
		}
	}
	return events > 0 && rest > 0 && pureEvents >= proxyCover*events && pureRest >= proxyCover*rest
}

func proxyNote(f *dataFilter, names []string) string {
	return "не проверялись как условия — повторяют само событие (внутри их значений событие у всех записей или ни у одной): " +
		strings.Join(cleanAll(f, names), ", ")
}

func proxyError(f *dataFilter, names []string) error {
	return fmt.Errorf("признаки %s повторяют само событие: внутри их значений событие у всех записей или ни у одной, "+
		"это не условия; укажи другие признаки", "«"+strings.Join(cleanAll(f, names), "», «")+"»")
}

// conditionColumns — колонки, которыми заданы событие, группы и фильтры:
// они сами задают сравнение и признаками не перебираются.
func conditionColumns(d domain.Dataset, lists ...[]analytics.Filter) map[string]bool {
	used := map[string]bool{}
	for _, list := range lists {
		for _, f := range list {
			if col, ok := findColumn(d, f.Column); ok {
				used[col.Name] = true
			}
		}
	}
	return used
}

func (r *agentRun) datasetByName(name string) (domain.Dataset, bool) {
	for _, d := range r.datasets {
		if d.Ready() && (strings.EqualFold(d.Name, name) || strings.EqualFold(d.TableName, name)) {
			return d, true
		}
	}
	return domain.Dataset{}, false
}

func (r *agentRun) driverDims(d domain.Dataset, args driversArgs) ([]driverDim, error) {
	var dims []driverDim
	if len(args.Dimensions) > 0 {
		for _, name := range args.Dimensions {
			col, ok := findColumn(d, name)
			if !ok {
				return nil, missingColumn(d, name)
			}
			if col.Type.IsTemporal() {
				continue
			}
			dims = append(dims, driverDim{name: label(col), column: col.Name})
		}
	} else {
		used := conditionColumns(d, args.Metric.Filters, args.GroupA, args.GroupB, args.Filters)
		for _, col := range d.Columns {
			if len(dims) >= maxDriverColumns {
				break
			}
			if used[col.Name] || col.Role != domain.RoleDimension || col.Profile == nil || col.Type.IsTemporal() {
				continue
			}
			if n := col.Profile.DistinctCount; n < 2 || n > maxDriverValues {
				continue
			}
			dims = append(dims, driverDim{name: label(col), column: col.Name})
		}
	}

	dateCol, ok := domain.Column{}, false
	if strings.TrimSpace(args.DateColumn) != "" {
		if dateCol, ok = findColumn(d, args.DateColumn); !ok || !dateCol.Type.IsTemporal() {
			return nil, fmt.Errorf("колонка %q не содержит дат", args.DateColumn)
		}
	} else {
		for _, col := range d.Columns {
			if col.Type.IsTemporal() {
				dateCol, ok = col, true
				break
			}
		}
	}
	if ok {
		calendar := []struct {
			grain analytics.Grain
			name  string
		}{
			{analytics.GrainQuarterOfYear, "Квартал года"},
			{analytics.GrainMonthOfYear, "Месяц года"},
			{analytics.GrainDayOfWeek, "День недели"},
		}
		if dateCol.Type == domain.ColumnTypeTimestamp {
			calendar = append(calendar, struct {
				grain analytics.Grain
				name  string
			}{analytics.GrainHour, "Час"})
		}
		for _, c := range calendar {
			dims = append(dims, driverDim{name: c.name + " («" + label(dateCol) + "»)", column: dateCol.Name, grain: c.grain})
		}
	}
	if len(dims) == 0 {
		return nil, errors.New("в датасете нет подходящих признаков: укажи колонки в dimensions")
	}
	return dims, nil
}

func findColumn(d domain.Dataset, name string) (domain.Column, bool) {
	return analytics.FindColumn(d, name)
}

func missingColumn(d domain.Dataset, name string) error {
	return analytics.MissingColumn(d, name)
}

// driverMetrics собирает метрики одного запроса: для каждой группы — число
// записей с известным значением, сам показатель и его разброс, если это среднее.
// Частота события (share с условием) считается из двух количеств — записей
// группы и событий в ней: share с условием группы делил бы события группы на
// все строки разреза, а не на строки самой группы.
func driverMetrics(m analytics.Metric, args driversArgs) []analytics.Metric {
	group := func(conds []analytics.Filter, suffix string) []analytics.Metric {
		filters := append(slices.Clone(m.Filters), conds...)
		if isRate(m) {
			// n — записи группы, у которых исход известен: посылка без даты
			// вручения не опоздала и не пришла вовремя, частоту она занижает.
			known := append(slices.Clone(conds), analytics.KnownFilters(m.Filters)...)
			return []analytics.Metric{
				{Func: analytics.FuncCount, Filters: known, Label: "n" + suffix},
				{Func: analytics.FuncCount, Filters: filters, Label: "k" + suffix},
			}
		}
		count := analytics.Metric{Func: analytics.FuncCount, Expr: m.Expr, Column: m.Column, Filters: filters, Label: "n" + suffix}
		if !hasValue(m) {
			count.Expr, count.Column = "", ""
		}
		value := m
		value.Filters, value.Label = filters, "v"+suffix
		out := []analytics.Metric{count, value}
		if isAverage(m) {
			spread := analytics.Metric{Func: analytics.FuncStddev, Expr: m.Expr, Column: m.Column, Filters: filters, Label: "s" + suffix}
			out = append(out, spread)
		}
		return out
	}
	if !args.compare() {
		return group(nil, "")
	}
	return append(group(args.GroupA, "a"), group(args.GroupB, "b")...)
}

// isRate — частота события в группе: доля строк с условием без колонки.
func isRate(m analytics.Metric) bool {
	return analytics.Func(strings.ToLower(string(m.Func))) == analytics.FuncShare &&
		m.Column == "" && m.Expr == "" && len(m.Filters) > 0
}

func hasValue(m analytics.Metric) bool {
	switch analytics.Func(strings.ToLower(string(m.Func))) {
	case analytics.FuncCount, analytics.FuncShare, analytics.FuncCalc:
		return false
	}
	return m.Expr != "" || m.Column != ""
}

func isAverage(m analytics.Metric) bool {
	return analytics.Func(strings.ToLower(string(m.Func))) == analytics.FuncAvg && m.Per == "" && (m.Expr != "" || m.Column != "")
}

func (r *agentRun) driverQuery(ctx context.Context, q analytics.Query) (*domain.ResultSet, string, []string, error) {
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

// relChange — относительная разница a к base. Делится на модуль base:
// при отрицательной базе (ROMI, прибыль) a/base-1 переворачивает знак —
// ROMI 74% против −48% выходил «на 254% ниже». base не должен быть нулём.
func relChange(a, base float64) float64 {
	return (a - base) / math.Abs(base)
}

// driverRow читает метрики строки начиная с колонки from. Порядок метрик —
// как в driverMetrics: n, v[, s] для группы А и затем для группы Б; у частоты
// события — n и k, из них частота в процентах и её стандартная ошибка.
func driverRow(row []any, from int, compare, rate bool) driverStats {
	cells := row[from:]
	width := len(cells)
	if compare {
		width /= 2
	}
	read := func(part []any) (n, v, se float64, hasSE, ok bool) {
		n, okN := numberOf(part[0])
		v, okV := numberOf(part[1])
		if rate {
			if !okN || !okV || n <= 0 {
				return n, 0, 0, false, false
			}
			p := v / n
			return n, 100 * p, 100 * math.Sqrt(p*(1-p)/n), true, true
		}
		if len(part) > 2 {
			if s, ok := numberOf(part[2]); ok && n > 1 {
				se, hasSE = s/math.Sqrt(n), true
			}
		}
		return n, v, se, hasSE, okN && okV
	}
	n, a, seA, hasSEA, okA := read(cells[:width])
	st := driverStats{n: n, a: a}
	if !compare {
		st.value, st.defined = a, okA
		st.se, st.hasSE = seA, hasSEA
		return st
	}
	nB, b, seB, hasSEB, okB := read(cells[width:])
	st.b, st.nB = b, nB
	if !okA || !okB || b == 0 {
		return st
	}
	st.value, st.defined = relChange(a, b), true
	if hasSEA && hasSEB && a != 0 {
		// Относительная ошибка отношения двух показателей.
		rel := math.Sqrt(math.Pow(seA/a, 2) + math.Pow(seB/b, 2))
		st.se, st.hasSE = math.Abs(a/b)*rel, true
	}
	return st
}

// driverScore — сила признака: взвешенное по числу записей отклонение групп
// от общего значения. Отклонения в пределах случайного разброса (|z| < 3)
// не учитываются, чтобы мелкие шумные группы не выходили в лидеры.
// Без сравнения отклонение берётся относительно общего значения.
func driverScore(values []driverStats, overall driverStats, compare bool) (float64, int) {
	var sum, weight float64
	signif := 0
	for _, v := range values {
		dev := v.value - overall.value
		if v.hasSE && v.se > 0 {
			if math.Abs(dev)/v.se < driverZ {
				dev = 0
			} else {
				signif++
			}
		}
		if !compare && overall.value != 0 {
			dev /= math.Abs(overall.value)
		}
		sum += v.n * dev * dev
		weight += v.n
	}
	if weight == 0 {
		return 0, signif
	}
	return math.Sqrt(sum / weight), signif
}

func driversTable(results []driverResult, overall driverStats, compare bool, labelA, labelB, metric string) *domain.ResultSet {
	rs := &domain.ResultSet{}
	if compare {
		rs.Columns = []domain.ResultColumn{
			{Name: "Признак", Type: domain.ColumnTypeText},
			{Name: "Значение", Type: domain.ColumnTypeText},
			{Name: "Записей: " + labelA, Type: domain.ColumnTypeInteger},
			{Name: metric + ": " + labelA, Type: domain.ColumnTypeNumeric},
			{Name: metric + ": " + labelB, Type: domain.ColumnTypeNumeric},
			{Name: "Разница, %", Type: domain.ColumnTypeNumeric},
		}
	} else {
		rs.Columns = []domain.ResultColumn{
			{Name: "Признак", Type: domain.ColumnTypeText},
			{Name: "Значение", Type: domain.ColumnTypeText},
			{Name: "Записей", Type: domain.ColumnTypeInteger},
			{Name: metric, Type: domain.ColumnTypeNumeric},
			{Name: "Отклонение от общего, %", Type: domain.ColumnTypeNumeric},
		}
	}
	add := func(dim string, v driverStats) {
		if compare {
			rs.Rows = append(rs.Rows, []any{dim, v.valueName, int64(v.n), round2(v.a), round2(v.b), round2(v.value * 100)})
			return
		}
		dev := 0.0
		if overall.value != 0 {
			dev = relChange(v.value, overall.value) * 100
		}
		rs.Rows = append(rs.Rows, []any{dim, v.valueName, int64(v.n), round2(v.value), round2(dev)})
	}
	all := overall
	all.valueName = driverOverallValue
	add(driverOverallName, all)

	for i, res := range results {
		if i >= driverShownDims {
			break
		}
		values := slices.Clone(res.values)
		slices.SortStableFunc(values, func(x, y driverStats) int { return cmp.Compare(y.value, x.value) })
		if i < driverDetailDims {
			if len(values) > driverDetailRows {
				values = append(values[:driverDetailRows-1], values[len(values)-1])
			}
		} else {
			values = []driverStats{values[0], values[len(values)-1]}
		}
		for _, v := range values {
			add(res.dim.name, v)
		}
	}
	rs.RowCount = len(rs.Rows)
	return rs
}

func driversSummary(f *dataFilter, results []driverResult, overall driverStats, compare bool, labelA, labelB string) map[string]any {
	ranking := make([]map[string]any, 0, len(results))
	for i, res := range results {
		top, bottom := res.values[0], res.values[0]
		for _, v := range res.values {
			if v.value > top.value {
				top = v
			}
			if v.value < bottom.value {
				bottom = v
			}
		}
		item := map[string]any{
			"rank":           i + 1,
			"dimension":      f.clean(res.dim.name, maxNameRunes),
			"strength":       round2(res.score * 100),
			"highest":        f.clean(top.valueName, maxCellRunes),
			"lowest":         f.clean(bottom.valueName, maxCellRunes),
			"groups_checked": len(res.values),
		}
		if compare {
			item["highest_gap_pct"], item["lowest_gap_pct"] = round2(top.value*100), round2(bottom.value*100)
		} else {
			item["highest_value"], item["lowest_value"] = round2(top.value), round2(bottom.value)
		}
		if top.hasSE {
			item["significant_groups"] = res.signif
		}
		ranking = append(ranking, item)
	}
	out := map[string]any{"ranking": ranking}
	if compare {
		out["overall"] = map[string]any{
			labelA: round2(overall.a), labelB: round2(overall.b),
			"gap_pct": round2(overall.value * 100), "difference": round2(overall.a - overall.b),
		}
	} else {
		out["overall"] = round2(overall.value)
	}
	return out
}

func driversHint(compare bool) string {
	hint := "ranking — признаки от сильного к слабому; strength — насколько группы признака отклоняются от общего значения с учётом их размера, " +
		"0 — отличия в пределах случайного разброса. "
	if compare {
		hint += "Разница, % — насколько показатель группы А выше показателя группы Б при этом значении признака, в процентах от показателя Б; " +
			"сравни её с общей разницей в первой строке. " + differenceHint
	}
	return hint + "Назови в ответе один-два сильнейших признака с числами из таблицы и скажи, что при остальных условиях разница мала. " +
		"Статус и другие поля «на момент выгрузки» — следствие, а не условие: не выдавай их за причину. Это связь в данных, а не доказанная причина."
}
