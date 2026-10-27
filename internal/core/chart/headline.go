package chart

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/heinousness/aibi/internal/core/domain"
)

// Headline выбирает главное в результате по форме графика: для динамики —
// последнее значение и изменение к предыдущему периоду, для сравнения
// категорий — лидера и его долю, для показателя — само число. Для форм,
// где одно число ничего не скажет (точки, распределение, серии), — nil.
func Headline(spec domain.ChartSpec, rs *domain.ResultSet) *domain.Headline {
	if rs == nil || len(rs.Rows) == 0 {
		return nil
	}
	metric := metricColumn(spec, rs)
	if metric < 0 {
		return nil
	}
	name := rs.Columns[metric].Name

	switch spec.Type {
	case domain.ChartTypeKPI:
		v, ok := number(rs.Rows[0][metric])
		if !ok {
			return nil
		}
		return &domain.Headline{Label: name, Value: v, Text: name + ": " + Human(v)}

	case domain.ChartTypeLine, domain.ChartTypeArea:
		if spec.Series != "" {
			return nil
		}
		return trend(rs, rs.ColumnIndex(spec.X), metric, name)

	case domain.ChartTypeBar, domain.ChartTypeHBar, domain.ChartTypePie,
		domain.ChartTypeDonut, domain.ChartTypeTable:
		if spec.Series != "" {
			return nil
		}
		return leader(rs, categoryColumn(spec, rs, metric), metric, name)
	}
	return nil
}

func trend(rs *domain.ResultSet, x, metric int, name string) *domain.Headline {
	last, prev := -1, -1
	for i := len(rs.Rows) - 1; i >= 0; i-- {
		if _, ok := number(rs.Rows[i][metric]); !ok {
			continue
		}
		if last < 0 {
			last = i
			continue
		}
		prev = i
		break
	}
	if last < 0 {
		return nil
	}

	value, _ := number(rs.Rows[last][metric])
	h := &domain.Headline{Label: "Последнее значение", Value: value}
	if x >= 0 {
		h.Label += " · " + period(rs.Rows[last][x])
	}
	h.Text = fmt.Sprintf("%s: %s", name, Human(value))
	if prev >= 0 {
		before, _ := number(rs.Rows[prev][metric])
		if before != 0 {
			change := round1((value - before) / math.Abs(before) * 100)
			h.ChangePct = &change
			h.Text += fmt.Sprintf(", %s к предыдущему периоду", signed(change))
		}
	}
	return h
}

func leader(rs *domain.ResultSet, cat, metric int, name string) *domain.Headline {
	if cat < 0 || len(rs.Rows) < 2 {
		return nil
	}
	best, total, negative := -1, 0.0, false
	var bestValue float64
	for i, row := range rs.Rows {
		v, ok := number(row[metric])
		if !ok {
			continue
		}
		negative = negative || v < 0
		total += v
		if best < 0 || v > bestValue {
			best, bestValue = i, v
		}
	}
	if best < 0 {
		return nil
	}

	label := period(rs.Rows[best][cat])
	h := &domain.Headline{
		Label: "Лидер · " + label,
		Value: bestValue,
		Text:  fmt.Sprintf("Больше всего — «%s»: %s", label, Human(bestValue)),
	}
	// Доля честна, только если видны все строки и нет отрицательных значений.
	if !negative && !rs.Truncated && total > 0 {
		share := round1(bestValue / total * 100)
		h.SharePct = &share
		h.Text += fmt.Sprintf(" (%s%% от суммы «%s»)", trimFloat(share), name)
	}
	return h
}

func metricColumn(spec domain.ChartSpec, rs *domain.ResultSet) int {
	for _, y := range spec.Y {
		if i := rs.ColumnIndex(y); i >= 0 && rs.Columns[i].Type.IsNumeric() {
			return i
		}
	}
	for i, c := range rs.Columns {
		if c.Type.IsNumeric() {
			return i
		}
	}
	return -1
}

func categoryColumn(spec domain.ChartSpec, rs *domain.ResultSet, metric int) int {
	if i := rs.ColumnIndex(spec.X); i >= 0 && i != metric {
		return i
	}
	for i, c := range rs.Columns {
		if i != metric && !c.Type.IsNumeric() {
			return i
		}
	}
	return -1
}

func number(v any) (float64, bool) {
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
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

func period(v any) string {
	switch x := v.(type) {
	case nil:
		return "—"
	case time.Time:
		if x.Day() == 1 && x.Hour() == 0 && x.Minute() == 0 {
			return x.Format("01.2006")
		}
		return x.Format("02.01.2006")
	case string:
		return x
	}
	return fmt.Sprint(v)
}

// Human — число для фразы: «1,2 млн», «12,5 тыс.», «3,75».
func Human(v float64) string {
	abs := math.Abs(v)
	switch {
	case abs >= 1e9:
		return trimFloat(round1(v/1e9)) + " млрд"
	case abs >= 1e6:
		return trimFloat(round1(v/1e6)) + " млн"
	case abs >= 1e4:
		return trimFloat(round1(v/1e3)) + " тыс."
	case abs >= 100:
		return trimFloat(math.Round(v))
	default:
		return trimFloat(math.Round(v*100) / 100)
	}
}

func signed(pct float64) string {
	switch {
	case pct > 0:
		return "+" + trimFloat(pct) + "%"
	case pct < 0:
		return "−" + trimFloat(-pct) + "%"
	}
	return "без изменений"
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

func trimFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	return strings.Replace(s, ".", ",", 1)
}
