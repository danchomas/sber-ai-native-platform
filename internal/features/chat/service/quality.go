package service

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/sqlguard"
)

// Пороги проверки полноты длительностей. Длительность известна только у строк,
// где заполнены обе даты: незавершённые записи в среднее не попадают, и если
// таких в группе заметно больше обычного, показатель группы смещён.
const (
	coverageDrop      = 0.05
	coverageAlone     = 0.90
	coverageMinRows   = 20
	coverageShownRows = 4
)

// qualityNotes проверяет результат aggregate на ловушки, из-за которых конец
// выгрузки выглядит как перемена: неполный последний период и длительности,
// посчитанные только по успевшим завершиться записям. warnings видит
// пользователь, hints — только модель: в них указания, как пересчитать.
func (r *agentRun) qualityNotes(ctx context.Context, q analytics.Query, rs *domain.ResultSet) (warnings, hints []string) {
	if w := r.partialPeriod(q, rs); w != "" {
		warnings = append(warnings, w)
		hints = append(hints, partialHint)
	}
	if w := r.durationCoverage(ctx, q); w != "" {
		warnings = append(warnings, w)
		hints = append(hints, coverageHint)
	}
	if h := r.periodInShare(q); h != "" {
		hints = append(hints, h)
	}
	if h := r.rateSpread(ctx, q); h != "" {
		hints = append(hints, h)
	}
	return warnings, hints
}

const (
	partialHint = "Последний период неполный, поэтому его строки из таблицы убраны: делай вывод только по полным периодам. " +
		"Про него напиши ровно одну фразу — «последний период неполный, в сравнение его не беру» — и не называй его чисел."
	coverageHint = "Группы с неполной длительностью не сравнивай с остальными напрямую: исключи их " +
		"или сравни долю записей, завершённых за одинаковый срок от начала."
)

// periodInShare ловит долю, у которой в условии метрики стоит период: условие
// share сужает только числитель, и «доля просроченных за 2025 год» выходит
// долей от всех лет группы — вдвое-втрое меньше настоящей частоты.
func (r *agentRun) periodInShare(q analytics.Query) string {
	dataset, ok := r.datasetByName(q.Dataset)
	if !ok {
		return ""
	}
	var labels []string
	for _, m := range q.Metrics {
		if !strings.EqualFold(string(m.Func), string(analytics.FuncShare)) {
			continue
		}
		for _, f := range m.Filters {
			if col, ok := findColumn(dataset, f.Column); ok && col.Type.IsTemporal() {
				labels = append(labels, "«"+orDefault(m.Label, "доля")+"»")
				break
			}
		}
	}
	if len(labels) == 0 {
		return ""
	}
	return "В " + strings.Join(labels, ", ") + " период задан в условии доли: оно сужает только числитель, " +
		"поэтому это доля от всех строк группы за весь срок, а не частота внутри периода. " +
		"Чтобы сравнить частоту по периодам, задай период через time или filters запроса и пересчитай."
}

func periodEnd(grain analytics.Grain, start time.Time) (time.Time, bool) {
	switch grain {
	case analytics.GrainWeek:
		return start.AddDate(0, 0, 7), true
	case analytics.GrainMonth:
		return start.AddDate(0, 1, 0), true
	case analytics.GrainQuarter:
		return start.AddDate(0, 3, 0), true
	case analytics.GrainYear:
		return start.AddDate(1, 0, 0), true
	}
	return time.Time{}, false
}

// partialPeriodStart возвращает метку последнего периода результата, если
// данные заканчиваются раньше его конца.
func (r *agentRun) partialPeriodStart(q analytics.Query, rs *domain.ResultSet) (string, bool) {
	last, dataEnd, end, ok := r.lastPeriod(q, rs)
	if !ok || !end.After(dataEnd.AddDate(0, 0, 1)) {
		return "", false
	}
	return last.Format("2006-01-02"), true
}

func (r *agentRun) lastPeriod(q analytics.Query, rs *domain.ResultSet) (last, dataEnd, end time.Time, ok bool) {
	if q.Time == nil || rs == nil || len(rs.Rows) == 0 {
		return
	}
	if dataEnd, ok = r.columnEnd(q.Dataset, q.Time.Column); !ok {
		return
	}
	for _, row := range rs.Rows {
		if t, isDay := parseDay(row[0]); isDay && t.After(last) {
			last = t
		}
	}
	if last.IsZero() {
		return last, dataEnd, end, false
	}
	end, ok = periodEnd(q.Time.Grain, last)
	return last, dataEnd, end, ok
}

func (r *agentRun) partialPeriod(q analytics.Query, rs *domain.ResultSet) string {
	last, dataEnd, end, ok := r.lastPeriod(q, rs)
	if !ok || !end.After(dataEnd.AddDate(0, 0, 1)) {
		return ""
	}
	covered := int(dataEnd.Sub(last).Hours()/24) + 1
	length := int(end.Sub(last).Hours() / 24)
	return fmt.Sprintf("Последний период (с %s) неполный: данные заканчиваются %s, в нём %d дн. из %d, "+
		"поэтому сравнивать его с полными периодами нельзя.",
		last.Format("02.01.2006"), dataEnd.Format("02.01.2006"), covered, length)
}

// dropPartialRows убирает из вывода для модели строки последнего неполного
// периода: модель цитирует то, что видит в таблице, даже с пометкой и
// предупреждением рядом. Таблица пользователя не меняется — он видит период
// на графике с предупреждением.
func dropPartialRows(payload map[string]any, last string) int {
	rows, ok := payload["rows"].([][]any)
	if !ok || last == "" {
		return 0
	}
	kept := rows[:0:0]
	for _, row := range rows {
		if len(row) > 0 && row[0] == last {
			continue
		}
		kept = append(kept, row)
	}
	payload["rows"] = kept
	return len(rows) - len(kept)
}

// withoutPeriod — копия результата без строк неполного периода: главное число
// над графиком считается по последнему полному периоду.
func withoutPeriod(rs *domain.ResultSet, last string) *domain.ResultSet {
	if rs == nil || last == "" {
		return rs
	}
	out := *rs
	out.Rows = make([][]any, 0, len(rs.Rows))
	for _, row := range rs.Rows {
		if len(row) > 0 && fmt.Sprint(row[0]) == last {
			continue
		}
		out.Rows = append(out.Rows, row)
	}
	if len(out.Rows) == 0 {
		return rs
	}
	out.RowCount = len(out.Rows)
	return &out
}

// columnEnd — последняя дата колонки по профилю каталога, без запроса к базе.
func (r *agentRun) columnEnd(dataset, column string) (time.Time, bool) {
	for _, d := range r.datasets {
		if !strings.EqualFold(d.Name, dataset) && !strings.EqualFold(d.TableName, dataset) {
			continue
		}
		for _, c := range d.Columns {
			if !strings.EqualFold(c.Name, column) && !strings.EqualFold(c.SourceName, column) {
				continue
			}
			if c.Profile == nil || c.Profile.MaxValue == nil {
				return time.Time{}, false
			}
			return parseDay(*c.Profile.MaxValue)
		}
	}
	return time.Time{}, false
}

func parseDay(v any) (time.Time, bool) {
	s, ok := v.(string)
	if !ok || len(s) < len("2006-01-02") {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s[:len("2006-01-02")])
	return t, err == nil
}

func (r *agentRun) durationCoverage(ctx context.Context, q analytics.Query) string {
	compiled, ok, err := analytics.CompileCoverage(q, r.catalog, r.s.cfg.MaxRows)
	if err != nil || !ok {
		return ""
	}
	scope, checked, err := sqlguard.ResolveScope(compiled.SQL, r.scopes, r.s.catalog.Schema(), r.s.cfg.MaxRows)
	if err != nil {
		return ""
	}
	rs, err := r.s.exec.Query(ctx, scope.Target, checked.SQL, r.s.cfg.MaxRows, compiled.Args...)
	if err != nil {
		r.s.log.WarnContext(ctx, "проверка полноты длительностей не выполнилась", "error", err)
		return ""
	}
	var grain analytics.Grain
	if q.Time != nil {
		grain = q.Time.Grain
	}
	return coverageNote(r.filter, rs, grain)
}

// periodName — подпись периода для пользователя: «неделя с 21.09.2026»,
// «сентябрь 2026», а не «2026-09-21».
func periodName(grain analytics.Grain, t time.Time) string {
	switch grain {
	case analytics.GrainWeek:
		return "неделя с " + t.Format("02.01.2006")
	case analytics.GrainMonth:
		return strings.ToLower(monthNames[t.Month()-1]) + " " + t.Format("2006")
	case analytics.GrainQuarter:
		return fmt.Sprintf("%d квартал %d", (int(t.Month())-1)/3+1, t.Year())
	case analytics.GrainYear:
		return t.Format("2006") + " год"
	}
	return t.Format("02.01.2006")
}

type coverageGroup struct {
	name         string
	total, known float64
}

func (g coverageGroup) share() float64 { return g.known / g.total }

func coverageNote(f *dataFilter, rs *domain.ResultSet, grain analytics.Grain) string {
	if rs == nil || len(rs.Columns) < 2 {
		return ""
	}
	dims := len(rs.Columns) - 2
	var groups []coverageGroup
	for _, row := range rs.Rows {
		total, okT := numberOf(row[dims])
		known, okK := numberOf(row[dims+1])
		if !okT || !okK || total <= 0 {
			continue
		}
		names := make([]string, dims)
		for i := range dims {
			names[i] = cellText(f, row[i])
			if t, ok := parseDay(row[i]); ok && i == 0 && grain != "" {
				names[i] = periodName(grain, t)
			}
		}
		groups = append(groups, coverageGroup{name: strings.Join(names, " / "), total: total, known: known})
	}
	if len(groups) == 0 {
		return ""
	}

	if dims == 0 || len(groups) == 1 {
		g := groups[0]
		if g.share() >= coverageAlone {
			return ""
		}
		return fmt.Sprintf("Длительность известна только у %s записей (%s из %s): у остальных нет второй даты, "+
			"например они ещё не завершены, и в расчёт они не вошли. Если это незавершённые записи, показатель занижен: "+
			"долгие ещё не закончились к концу выгрузки.", percent(g.share()), groupNumber(g.known), groupNumber(g.total))
	}

	var shares []float64
	for _, g := range groups {
		if g.total >= coverageMinRows {
			shares = append(shares, g.share())
		}
	}
	if len(shares) == 0 {
		return ""
	}
	slices.Sort(shares)
	typical := shares[len(shares)/2]

	var low []coverageGroup
	for _, g := range groups {
		if g.share() < typical-coverageDrop {
			low = append(low, g)
		}
	}
	if len(low) == 0 {
		return ""
	}
	slices.SortFunc(low, func(a, b coverageGroup) int { return cmp.Compare(a.share(), b.share()) })
	if len(low) > coverageShownRows {
		low = low[:coverageShownRows]
	}
	parts := make([]string, len(low))
	for i, g := range low {
		parts[i] = fmt.Sprintf("«%s» — у %s", g.name, percent(g.share()))
	}
	return fmt.Sprintf("Длительность известна не у всех записей: %s (в остальных группах обычно у %s). "+
		"Незавершённые записи в расчёт не вошли, поэтому в этих группах показатель смещён: "+
		"долгие записи ещё не закончились к концу выгрузки.",
		strings.Join(parts, ", "), percent(typical))
}

func percent(share float64) string {
	return fmt.Sprintf("%.0f%%", math.Round(share*100))
}

func groupNumber(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

func cellText(f *dataFilter, v any) string {
	switch x := v.(type) {
	case nil:
		return "(пусто)"
	case string:
		return f.clean(x, maxCellRunes)
	}
	return fmt.Sprint(v)
}

func numberOf(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	case int16:
		return float64(x), true
	}
	return 0, false
}
