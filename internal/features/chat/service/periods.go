package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/domain"
)

const (
	toolPeriods     = "compare_periods"
	analysisPeriods = "periods"

	historyWeeks  = 26
	historyMonths = 24
	// minHistory — меньше полных периодов в истории — обычный разброс не оценить.
	minHistory = 4
	// periodNoiseZ — отклонение меньше двух обычных разбросов — колебание, а не перемена.
	periodNoiseZ = 2.0
	// coverageGapShare — на столько долей меньше обычного известна длительность
	// в периоде — значение периода смещено незавершёнными записями.
	coverageGapShare = 0.05
	// compositionShiftShare — сдвиг доли значения, о котором стоит сказать.
	compositionShiftShare = 0.05
	maxCompositionDims    = 4
	maxCompositionValues  = 20
	dayLayout             = "2006-01-02"
)

func periodTools() []struct {
	name, desc string
	params     schema
} {
	return []struct {
		name, desc string
		params     schema
	}{{
		toolPeriods,
		"Проверить, изменился ли показатель в заданный период: сравнивает его с предыдущими днями, с тем же периодом год назад " +
			"и с обычным разбросом по неделям или месяцам до него, проверяет полноту длительностей у конца выгрузки и сдвиг " +
			"состава записей и возвращает готовый вывод. Для вопросов «стало ли быстрее/медленнее/больше в конце сентября», " +
			"«можно ли утверждать, что после X показатель изменился», «стала ли поддержка отвечать быстрее в последние месяцы».",
		object(schema{
			"dataset": text("имя датасета"),
			"metric":  metricSchema("обычно avg длительности, share события или sum/count"),
			"date_column": text("колонка с датой, по которой относить записи к периоду. Для длительностей бери дату окончания " +
				"(«Завершено»): по ней у всех записей периода длительность известна; по дате начала свежие незавершённые записи выпадают"),
			"from":    text("начало проверяемого периода, ГГГГ-ММ-ДД"),
			"to":      text("конец проверяемого периода включительно, ГГГГ-ММ-ДД"),
			"filters": filtersSchema(),
			"title":   text("заголовок результата"),
		}, "dataset", "metric", "date_column", "from", "to"),
	}}
}

type periodsArgs struct {
	Dataset    string             `json:"dataset"`
	Metric     *analytics.Metric  `json:"metric"`
	DateColumn string             `json:"date_column"`
	From       string             `json:"from"`
	To         string             `json:"to"`
	Filters    []analytics.Filter `json:"filters"`
	Title      string             `json:"title"`
}

// span — отрезок дат включительно.
type span struct {
	name     string
	from, to time.Time
}

func (s span) days() int { return int(s.to.Sub(s.from).Hours()/24) + 1 }

func (s span) dates() string {
	return s.from.Format("02.01.2006") + "–" + s.to.Format("02.01.2006")
}

func (s span) filter(column string) analytics.Filter {
	return analytics.Filter{Column: column, Op: analytics.OpBetween,
		Values: []any{s.from.Format(dayLayout), s.to.Format(dayLayout)}}
}

// spanStats — показатель на отрезке. value для суммы и количества — в день,
// чтобы отрезки разной длины были сравнимы.
type spanStats struct {
	span
	ok       bool
	n        float64
	value    float64
	total    float64
	coverage float64
}

// periodRun — общее для всех запросов инструмента.
type periodRun struct {
	r         *agentRun
	base      analytics.Query
	metric    analytics.Metric
	column    string
	rate      bool
	extensive bool
	duration  bool
}

func (p periodRun) metrics() (stats []analytics.Metric, total *analytics.Metric) {
	stats = driverMetrics(p.metric, driversArgs{})
	if p.duration {
		total = &analytics.Metric{Func: analytics.FuncCount, Filters: slices.Clone(p.metric.Filters), Label: "total"}
	}
	return stats, total
}

func (p periodRun) query(extra ...analytics.Filter) analytics.Query {
	q := p.base
	q.Filters = append(slices.Clone(p.base.Filters), extra...)
	stats, total := p.metrics()
	q.Metrics = stats
	if total != nil {
		q.Metrics = append(q.Metrics, *total)
	}
	return q
}

// read разбирает строку результата начиная с колонки from.
func (p periodRun) read(row []any, from, days int) (n, value, total float64, ok bool) {
	stats, totalMetric := p.metrics()
	st := driverRow(row[:from+len(stats)], from, false, p.rate)
	if !st.defined {
		return 0, 0, 0, false
	}
	value = st.value
	if p.extensive && days > 0 {
		value /= float64(days)
	}
	if totalMetric != nil {
		total, _ = numberOf(row[from+len(stats)])
	}
	return st.n, value, total, true
}

func (p periodRun) spanStats(ctx context.Context, s span) (spanStats, string, []string, error) {
	out := spanStats{span: s}
	rs, sql, tables, err := p.r.driverQuery(ctx, p.query(s.filter(p.column)))
	if err != nil || len(rs.Rows) != 1 {
		return out, sql, tables, err
	}
	out.n, out.value, out.total, out.ok = p.read(rs.Rows[0], 0, s.days())
	if out.total > 0 {
		out.coverage = out.n / out.total
	}
	return out, sql, tables, nil
}

// history — показатель по полным неделям или месяцам перед периодом:
// по нему видно, какие колебания обычны.
type history struct {
	grain     analytics.Grain
	values    []float64
	mean, std float64
	min, max  float64
	coverage  float64
	n         float64
}

func (h history) unit() string {
	if h.grain == analytics.GrainMonth {
		return "месяцам"
	}
	return "неделям"
}

func (p periodRun) history(ctx context.Context, target span, dataStart time.Time) (history, error) {
	h := history{grain: analytics.GrainWeek}
	from := target.from.AddDate(0, 0, -7*historyWeeks)
	if target.days() > 31 {
		h.grain = analytics.GrainMonth
		from = target.from.AddDate(0, -historyMonths, 0)
	}
	if from.Before(dataStart) {
		from = dataStart
	}
	window := span{from: from, to: target.from.AddDate(0, 0, -1)}
	if !window.to.After(window.from) {
		return h, nil
	}
	q := p.query(window.filter(p.column))
	q.Time = &analytics.TimeGroup{Column: p.column, Grain: h.grain}
	q.Limit = historyWeeks + historyMonths
	rs, _, _, err := p.r.driverQuery(ctx, q)
	if err != nil {
		return h, err
	}
	var known, total float64
	for _, row := range rs.Rows {
		start, ok := parseDay(row[0])
		end, okEnd := periodEnd(h.grain, start)
		if !ok || !okEnd || start.Before(window.from) || end.After(target.from) {
			continue
		}
		n, value, t, ok := p.read(row, 1, int(end.Sub(start).Hours()/24))
		if !ok {
			continue
		}
		h.values = append(h.values, value)
		h.n += n
		known += n
		total += t
	}
	if len(h.values) == 0 {
		return h, nil
	}
	h.min, h.max = slices.Min(h.values), slices.Max(h.values)
	for _, v := range h.values {
		h.mean += v
	}
	h.mean /= float64(len(h.values))
	for _, v := range h.values {
		h.std += (v - h.mean) * (v - h.mean)
	}
	if len(h.values) > 1 {
		h.std = math.Sqrt(h.std / float64(len(h.values)-1))
	}
	if total > 0 {
		h.coverage = known / total
	}
	return h, nil
}

// compositionShift ищет категорию, доля которой в периоде заметно другая,
// чем раньше: тогда перемена может быть сменой состава, а не скорости.
func (p periodRun) compositionShift(ctx context.Context, d domain.Dataset, target, before span, args periodsArgs) string {
	used := map[string]bool{}
	for _, list := range [][]analytics.Filter{args.Filters, p.metric.Filters} {
		for _, f := range list {
			if col, ok := findColumn(d, f.Column); ok {
				used[col.Name] = true
			}
		}
	}
	best, bestShift := "", 0.0
	checked := 0
	for _, col := range d.Columns {
		if checked >= maxCompositionDims {
			break
		}
		if used[col.Name] || col.Role != domain.RoleDimension || col.Profile == nil || col.Type.IsTemporal() {
			continue
		}
		if n := col.Profile.DistinctCount; n < 2 || n > maxCompositionValues {
			continue
		}
		checked++
		q := p.base
		q.GroupBy = []string{col.Name}
		q.Limit = maxCompositionValues + 1
		q.Metrics = []analytics.Metric{
			{Func: analytics.FuncCount, Filters: []analytics.Filter{target.filter(p.column)}, Label: "a"},
			{Func: analytics.FuncCount, Filters: []analytics.Filter{before.filter(p.column)}, Label: "b"},
		}
		rs, _, _, err := p.r.driverQuery(ctx, q)
		if err != nil {
			continue
		}
		if len(rs.Columns) < 3 {
			continue
		}
		var totalA, totalB float64
		for _, row := range rs.Rows {
			a, _ := numberOf(row[1])
			b, _ := numberOf(row[2])
			totalA += a
			totalB += b
		}
		if totalA == 0 || totalB == 0 {
			continue
		}
		for _, row := range rs.Rows {
			a, _ := numberOf(row[1])
			b, _ := numberOf(row[2])
			if shift := math.Abs(a/totalA - b/totalB); shift > bestShift {
				bestShift = shift
				best = fmt.Sprintf("доля «%s» по признаку «%s» — %.0f%% в периоде против %.0f%% раньше",
					p.r.filter.clean(fmt.Sprint(row[0]), maxCellRunes), p.r.filter.clean(label(col), maxNameRunes),
					a/totalA*100, b/totalB*100)
			}
		}
	}
	if bestShift < compositionShiftShare {
		return ""
	}
	return best
}

func (r *agentRun) comparePeriods(ctx context.Context, raw []byte) (string, int, error) {
	var args periodsArgs
	if err := decodeArgs(normalizeArgs(raw, "filters"), &args); err != nil {
		return "", 0, err
	}
	if args.Metric == nil || args.Metric.Func == "" {
		return "", 0, errors.New("укажи metric: какой показатель сравнивать")
	}
	dataset, ok := r.datasetByName(args.Dataset)
	if !ok {
		return "", 0, fmt.Errorf("датасета %q нет в каталоге", args.Dataset)
	}
	col, ok := findColumn(dataset, args.DateColumn)
	if !ok {
		return "", 0, missingColumn(dataset, args.DateColumn)
	}
	if !col.Type.IsTemporal() {
		return "", 0, fmt.Errorf("колонка «%s» не содержит дат: укажи date_column с датой", label(col))
	}
	from, okFrom := parseDate(args.From)
	to, okTo := parseDate(args.To)
	if !okFrom || !okTo {
		return "", 0, errors.New("from и to — даты вида ГГГГ-ММ-ДД, например 2026-09-21 и 2026-09-30")
	}
	if to.Before(from) {
		from, to = to, from
	}
	dataStart, okStart := columnBound(dataset, col, false)
	dataEnd, okEnd := columnBound(dataset, col, true)
	if !okStart || !okEnd {
		return "", 0, fmt.Errorf("у колонки «%s» нет границ дат в профиле", label(col))
	}
	var notes []string
	if to.After(dataEnd) {
		to = dataEnd
		notes = append(notes, "период обрезан концом данных: "+dataEnd.Format("02.01.2006"))
	}
	if from.After(dataEnd) || to.Before(dataStart) {
		return "", 0, fmt.Errorf("период вне данных: они идут с %s по %s",
			dataStart.Format("02.01.2006"), dataEnd.Format("02.01.2006"))
	}

	fn := analytics.Func(strings.ToLower(string(args.Metric.Func)))
	p := periodRun{
		r: r, metric: *args.Metric, column: col.Name,
		base:     analytics.Query{Dataset: dataset.Name, Filters: args.Filters, Limit: 2},
		rate:     isRate(*args.Metric),
		duration: analytics.IsDuration(*args.Metric, r.catalog, dataset.Name),
		extensive: (fn == analytics.FuncCount || fn == analytics.FuncSum || fn == analytics.FuncCountDistinct) &&
			args.Metric.Per == "",
	}

	target := span{name: "Проверяемый период", from: from, to: to}
	length := target.days()
	previous := span{name: fmt.Sprintf("Предыдущие %d дн.", length),
		from: from.AddDate(0, 0, -length), to: from.AddDate(0, 0, -1)}
	lastYear := span{name: "Тот же период год назад", from: from.AddDate(-1, 0, 0), to: to.AddDate(-1, 0, 0)}

	tStats, sql, tables, err := p.spanStats(ctx, target)
	if err != nil {
		return "", 0, err
	}
	if !tStats.ok {
		return "", 0, errors.New("в проверяемом периоде нет записей с известным значением показателя: проверь metric, filters и даты")
	}
	spans := []spanStats{tStats}
	for _, s := range []span{previous, lastYear} {
		if s.from.Before(dataStart) {
			continue
		}
		st, _, _, err := p.spanStats(ctx, s)
		if err != nil {
			return "", 0, err
		}
		if st.ok {
			spans = append(spans, st)
		}
	}
	hist, err := p.history(ctx, target, dataStart)
	if err != nil {
		return "", 0, err
	}
	shift := p.compositionShift(ctx, dataset, target, span{from: target.from.AddDate(0, 0, -7*historyWeeks), to: target.from.AddDate(0, 0, -1)}, args)

	metricLabel := orDefault(args.Metric.Label, "Показатель")
	if p.extensive {
		metricLabel += ", в день"
	}
	out := periodsTable(spans, hist, metricLabel, p.duration)
	spec := &domain.ChartSpec{Type: domain.ChartTypeBar, X: "Период", Y: []string{metricLabel}}
	res := r.register(orDefault(args.Title, "Изменился ли показатель в период "+target.dates()), sql, tables, out, nil,
		analysisPeriods, spec)

	payload := r.resultOutput(res)
	payload["summary"] = periodsSummary(spans, hist, p.duration, shift, notes)
	payload["how_to_read"] = "conclusion — готовый вывод расчёта: начни ответ с него и подтверди числами из таблицы " +
		"(значение периода, обычный диапазон, год назад). Обычный диапазон — колебания по полным неделям или месяцам до периода: " +
		"значение внутри него не говорит о перемене. Если есть coverage_note или composition — назови их одной фразой. " +
		"Если есть coverage_note, а показатель — длительность, можно один раз повторить compare_periods с date_column — " +
		"датой окончания: по ней длительность известна у всех записей периода, и сравнение становится честным."
	return encodeOutput(payload), len(out.Rows), nil
}

func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{dayLayout, "02.01.2006", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), true
		}
	}
	if len(s) >= len(dayLayout) {
		return parseDay(s)
	}
	return time.Time{}, false
}

// columnBound — первая или последняя дата колонки по профилю каталога.
func columnBound(d domain.Dataset, col domain.Column, last bool) (time.Time, bool) {
	for _, c := range d.Columns {
		if c.Name != col.Name || c.Profile == nil {
			continue
		}
		v := c.Profile.MinValue
		if last {
			v = c.Profile.MaxValue
		}
		if v == nil {
			return time.Time{}, false
		}
		return parseDay(*v)
	}
	return time.Time{}, false
}

func periodsTable(spans []spanStats, h history, metric string, duration bool) *domain.ResultSet {
	rs := &domain.ResultSet{Columns: []domain.ResultColumn{
		{Name: "Период", Type: domain.ColumnTypeText},
		{Name: "Даты", Type: domain.ColumnTypeText},
		{Name: metric, Type: domain.ColumnTypeNumeric},
		{Name: "Записей", Type: domain.ColumnTypeInteger},
	}}
	if duration {
		rs.Columns = append(rs.Columns, domain.ResultColumn{Name: "Длительность известна, %", Type: domain.ColumnTypeNumeric})
	}
	add := func(name, dates string, value, n, coverage float64) {
		row := []any{name, dates, round2(value), int64(math.Round(n))}
		if duration {
			row = append(row, round2(coverage*100))
		}
		rs.Rows = append(rs.Rows, row)
	}
	for _, s := range spans {
		add(s.name, s.dates(), s.value, s.n, s.coverage)
	}
	if len(h.values) > 0 {
		add(fmt.Sprintf("Обычно (%d полн. периодов до него)", len(h.values)),
			fmt.Sprintf("от %s до %s", humanNumber(h.min), humanNumber(h.max)),
			h.mean, h.n/float64(len(h.values)), h.coverage)
	}
	rs.RowCount = len(rs.Rows)
	return rs
}

func periodsSummary(spans []spanStats, h history, duration bool, shift string, notes []string) map[string]any {
	target := spans[0]
	out := map[string]any{"period": target.dates(), "value": round2(target.value)}
	var parts []string
	verdict := ""

	coverageDrop := duration && h.coverage > 0 && target.coverage < h.coverage-coverageGapShare
	if coverageDrop {
		out["coverage_note"] = fmt.Sprintf("длительность известна у %.0f%% записей периода против обычных %.0f%%: "+
			"часть записей ещё не завершена, и значение периода смещено — долгие не успели закончиться",
			target.coverage*100, h.coverage*100)
	}

	note, _ := out["coverage_note"].(string)
	switch {
	case len(h.values) >= minHistory && h.std > 0:
		scale := math.Sqrt(periodLength(h.grain) / float64(target.days()))
		z := (target.value - h.mean) / (h.std * math.Max(scale, 1))
		change := 0.0
		if h.mean != 0 {
			change = relChange(target.value, h.mean) * 100
		}
		out["typical_mean"] = round2(h.mean)
		out["typical_range"] = []float64{round2(h.min), round2(h.max)}
		out["change_vs_typical_pct"] = round2(change)
		out["z"] = round2(z)
		usual := fmt.Sprintf("по %d полным %s до периода показатель был от %s до %s, в среднем %s",
			len(h.values), h.unit(), humanNumber(h.min), humanNumber(h.max), humanNumber(h.mean))
		dir := "выше"
		if z < 0 {
			dir = "ниже"
		}
		within := math.Abs(z) < periodNoiseZ
		switch {
		case coverageDrop && within:
			verdict = fmt.Sprintf("Нельзя утверждать: %s. Даже без поправки значение периода %s в пределах обычных колебаний — %s",
				note, humanNumber(target.value), usual)
		case coverageDrop:
			verdict = fmt.Sprintf("Нельзя утверждать: %s. Без поправки значение периода %s %s обычного на %.1f%% (%s), "+
				"но это след незавершённых записей, а не перемена", note, humanNumber(target.value), dir, math.Abs(change), usual)
		case within:
			verdict = fmt.Sprintf("Перемены нет: значение периода %s в пределах обычных колебаний — %s",
				humanNumber(target.value), usual)
		default:
			verdict = fmt.Sprintf("Перемена есть: значение периода %s заметно %s обычного (%+.1f%%) — %s",
				humanNumber(target.value), dir, change, usual)
		}
	case coverageDrop:
		verdict = "Нельзя утверждать: " + note
	default:
		verdict = "Истории до периода мало, чтобы оценить обычные колебания: сравни только с предыдущими днями и годом ранее"
	}
	parts = append(parts, verdict)

	for _, s := range spans[1:] {
		if s.value == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%s): %s, разница %+.1f%%",
			lowerFirst(s.name), s.dates(), humanNumber(s.value), relChange(target.value, s.value)*100))
	}
	if shift != "" {
		out["composition"] = shift
		parts = append(parts, "состав записей изменился: "+shift+" — часть разницы может объясняться этим")
	}
	if len(notes) > 0 {
		out["notes"] = notes
		parts = append(parts, strings.Join(notes, "; "))
	}
	out["conclusion"] = strings.Join(parts, ". ") + "."
	return out
}

func periodLength(g analytics.Grain) float64 {
	if g == analytics.GrainMonth {
		return 30.4
	}
	return 7
}

// humanNumber — число для текста вывода: без лишних знаков после запятой.
func humanNumber(v float64) string {
	switch a := math.Abs(v); {
	case a >= 100:
		return readableCell(math.Round(v))
	case a >= 10:
		return readableCell(math.Round(v*10) / 10)
	}
	return readableCell(math.Round(v*100) / 100)
}

func lowerFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToLower(string(r[:1])) + string(r[1:])
}
