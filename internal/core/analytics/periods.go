package analytics

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// PeriodWindow — полуоткрытый период: From включительно, To исключительно.
type PeriodWindow struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type PeriodComparisonQuery struct {
	Dataset       string       `json:"dataset"`
	TimeColumn    string       `json:"time_column"`
	Metric        Metric       `json:"metric"`
	Baseline      PeriodWindow `json:"baseline"`
	Current       PeriodWindow `json:"current"`
	GroupBy       []string     `json:"group_by,omitempty"`
	Filters       []Filter     `json:"filters,omitempty"`
	Direction     string       `json:"direction"`
	ObservedUntil string       `json:"observed_until,omitempty"`
	FollowupHours float64      `json:"followup_hours,omitempty"`
}

const MaxComparisonRows = 1000

func comparisonDate(s string) (time.Time, error) {
	// Явные границы в календарном времени датасета, без неявной конвертации МСК в UTC.
	for _, layout := range []string{time.DateOnly, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, invalid("граница периода %q: нужна дата ГГГГ-ММ-ДД или дата со временем без часового пояса, в часовом поясе данных", s)
}

func CompilePeriodComparison(q PeriodComparisonQuery, cat Catalog) (*Compiled, error) {
	c := &compiler{cat: cat}
	if err := c.bind(q.Dataset, nil); err != nil {
		return nil, err
	}
	if len(q.GroupBy) > MaxGroupBy {
		return nil, invalid("разрезов не больше %d", MaxGroupBy)
	}
	if q.Direction != "lower" && q.Direction != "higher" {
		return nil, invalid("direction должен быть lower (меньше лучше) или higher (больше лучше)")
	}
	if q.Metric.Func != "" && q.Metric.Func != FuncAvg {
		return nil, invalid("compare_periods сравнивает распределения и средние: fn должен быть avg или не задан")
	}
	ref, _, err := c.temporalRef(q.TimeColumn)
	if err != nil {
		return nil, err
	}
	var bounds [4]time.Time
	for i, s := range []string{q.Baseline.From, q.Baseline.To, q.Current.From, q.Current.To} {
		bounds[i], err = comparisonDate(s)
		if err != nil {
			return nil, err
		}
	}
	if !bounds[0].Before(bounds[1]) || !bounds[2].Before(bounds[3]) || bounds[2].Before(bounds[1]) {
		return nil, invalid("периоды должны быть непустыми, не пересекаться; current следует за baseline")
	}
	if bounds[1].Sub(bounds[0]) != bounds[3].Sub(bounds[2]) {
		c.warns = append(c.warns, "Длины периодов различаются: количество записей не является сопоставимой скоростью потока.")
	}
	params := make([]string, len(bounds))
	for i, b := range bounds {
		params[i] = c.param(b) + "::timestamp"
	}
	base := ref + " >= " + params[0] + " AND " + ref + " < " + params[1]
	current := ref + " >= " + params[2] + " AND " + ref + " < " + params[3]
	where, err := c.where(q.Filters)
	if err != nil {
		return nil, err
	}
	where = append(where, "(("+base+") OR ("+current+"))")
	value, err := c.measureValue(q.Metric)
	if err != nil {
		return nil, err
	}
	invalidValue, unobserved, immature := "false", "false", "false"
	var observed time.Time
	if q.ObservedUntil != "" {
		observed, err = comparisonDate(q.ObservedUntil)
		if err != nil {
			return nil, err
		}
		if bounds[3].After(observed) {
			return nil, invalid("current.to не может быть позже observed_until: период наблюдения ещё не завершён")
		}
	}
	if math.IsNaN(q.FollowupHours) || math.IsInf(q.FollowupHours, 0) || q.FollowupHours < 0 || q.FollowupHours > 24*366 {
		return nil, invalid("followup_hours должен быть от 0 до 8784 часов")
	}
	if q.Metric.Duration == nil && (q.FollowupHours != 0 || q.ObservedUntil != "") {
		return nil, invalid("observed_until и followup_hours применимы только к duration")
	}
	if d := q.Metric.Duration; d != nil {
		start, end, divisor, _ := c.durationRefs(*d)
		if ref == end {
			c.warns = append(c.warns, "Когорта выбрана по концу интервала: незавершённые задачи в неё не попадают. Это срез завершений, а не доказательство ускорения всех начатых задач.")
		}
		invalidValue = "(" + end + " < " + start + ")"
		unobserved = "(" + start + " IS NOT NULL AND " + end + " IS NULL)"
		valid := end + " >= " + start
		if !observed.IsZero() {
			cutoff := c.param(observed) + "::timestamp"
			valid += " AND " + end + " < " + cutoff
			unobserved = "(" + start + " IS NOT NULL AND (" + end + " IS NULL OR " + end + " >= " + cutoff + "))"
			if q.FollowupHours > 0 {
				horizon := start + "::timestamp + " + c.param(q.FollowupHours) + "::numeric * interval '1 hour'"
				immature = "(" + horizon + " > " + cutoff + ")"
				valid += " AND NOT " + immature + " AND " + end + " <= " + horizon
				unobserved = "(" + start + " IS NOT NULL AND (" + end + " IS NULL OR " + end + " > " + horizon + " OR " + end + " >= " + cutoff + "))"
			}
		} else if q.FollowupHours > 0 {
			return nil, invalid("для одинакового followup_hours укажите observed_until — фактическую границу наблюдения, а не текущую дату")
		} else {
			c.warns = append(c.warns, "Граница наблюдения не задана: полнота недавних когорт не подтверждена. Не делайте уверенный вывод об ускорении.")
		}
		value = "CASE WHEN " + valid + " THEN " + durationValue(start, end, divisor) + " END"
		if q.FollowupHours == 0 {
			c.warns = append(c.warns, "Единый горизонт наблюдения не задан: у недавних задач меньше времени на завершение. Сравнение завершённых интервалов — описательное, не доказательство ускорения.")
		}
	}

	var inner, outer strings.Builder
	fmt.Fprintf(&inner, "SELECT CASE WHEN %s THEN 0 ELSE 1 END AS period_id, %s AS value, %s AS invalid_value, %s AS unobserved, %s AS immature", base, value, invalidValue, unobserved, immature)
	outer.WriteString("SELECT period_id")
	groups := []string{"period_id"}
	for i, name := range q.GroupBy {
		bt, col, err := c.column(name)
		if err != nil {
			return nil, err
		}
		alias := fmt.Sprintf("g%d", i)
		fmt.Fprintf(&inner, ", %s AS %s", qualify(bt.alias, col.Name), alias)
		fmt.Fprintf(&outer, ", %s AS %s", alias, ident(title(col)))
		groups = append(groups, alias)
	}
	c.writeFrom(&inner)
	writeWhere(&inner, where)
	outer.WriteString(", count(*) AS total, count(value) AS measured, avg(value)::double precision AS mean, " +
		"var_samp(value)::double precision AS variance, percentile_cont(0.5) WITHIN GROUP (ORDER BY value) AS median, " +
		"percentile_cont(0.9) WITHIN GROUP (ORDER BY value) AS p90, " +
		"count(*) FILTER (WHERE invalid_value) AS invalid, count(*) FILTER (WHERE unobserved) AS unobserved, " +
		"count(*) FILTER (WHERE immature) AS immature FROM observations GROUP BY " + strings.Join(groups, ", ") + " ORDER BY " + strings.Join(groups, ", "))
	return c.result("WITH observations AS (" + inner.String() + ") " + outer.String()), nil
}
