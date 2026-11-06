package service

import (
	"strings"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/domain"
)

// nameCalendar заменяет номера календарных частей названиями: «Декабрь»
// вместо 12, «Понедельник» вместо 1. Порядок строк сохраняется — он уже
// календарный. Возвращает true, если результат изменён.
func nameCalendar(q analytics.Query, rs *domain.ResultSet) bool {
	if q.Time == nil || rs == nil || len(rs.Columns) == 0 {
		return false
	}
	var names []string
	switch q.Time.Grain {
	case analytics.GrainMonthOfYear:
		names = monthNames
	case analytics.GrainDayOfWeek:
		names = weekdayNames
	case analytics.GrainQuarterOfYear:
		names = []string{"I квартал", "II квартал", "III квартал", "IV квартал"}
	default:
		return false
	}
	for _, row := range rs.Rows {
		n, ok := numberOf(row[0])
		if !ok || int(n) < 1 || int(n) > len(names) {
			return false
		}
	}
	for _, row := range rs.Rows {
		n, _ := numberOf(row[0])
		row[0] = names[int(n)-1]
	}
	rs.Columns[0].Type = domain.ColumnTypeText
	return true
}

// additiveMetrics — можно ли складывать значения показателей по группам.
// Доля лидера «от суммы» имеет смысл только для сумм и количеств.
func additiveMetrics(metrics []analytics.Metric) bool {
	if len(metrics) == 0 {
		return true
	}
	for _, m := range metrics {
		switch analytics.Func(strings.ToLower(strings.TrimSpace(string(m.Func)))) {
		case analytics.FuncSum, analytics.FuncCount:
			if m.Per != "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func dropShare(h *domain.Headline) {
	if h == nil || h.SharePct == nil {
		return
	}
	h.SharePct = nil
	if i := strings.LastIndex(h.Text, " ("); i > 0 && strings.Contains(h.Text[i:], "от суммы") {
		h.Text = h.Text[:i]
	}
}
