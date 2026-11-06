package analytics

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/heinousness/aibi/internal/core/domain"
)

var dateLayouts = []string{
	time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02 15:04",
	"2006-01-02", "02.01.2006", "2006-01", "01.2006", "2006",
}

func coerce(v any, col domain.Column) (any, error) {
	if v == nil {
		return nil, invalid("не задано значение фильтра по «%s»", title(col))
	}

	switch col.Type {
	case domain.ColumnTypeInteger:
		switch x := v.(type) {
		case float64:
			if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
				return int64(x), nil
			}
		case int:
			return int64(x), nil
		case int64:
			return x, nil
		case string:
			if n, err := strconv.ParseInt(compactNumber(x), 10, 64); err == nil {
				return n, nil
			}
		}

	case domain.ColumnTypeNumeric:
		switch x := v.(type) {
		case float64:
			return x, nil
		case int:
			return float64(x), nil
		case int64:
			return float64(x), nil
		case string:
			if f, err := strconv.ParseFloat(compactNumber(x), 64); err == nil {
				return f, nil
			}
		}

	case domain.ColumnTypeBoolean:
		switch x := v.(type) {
		case bool:
			return x, nil
		case string:
			switch strings.ToLower(strings.TrimSpace(x)) {
			case "true", "да", "1", "yes", "истина":
				return true, nil
			case "false", "нет", "0", "no", "ложь":
				return false, nil
			}
		}

	case domain.ColumnTypeDate, domain.ColumnTypeTimestamp:
		switch x := v.(type) {
		case string:
			s := strings.TrimSpace(x)
			for _, layout := range dateLayouts {
				if t, err := time.Parse(layout, s); err == nil {
					return t, nil
				}
			}
		case float64:
			if x == math.Trunc(x) && x >= 1900 && x <= 2200 {
				return time.Date(int(x), time.January, 1, 0, 0, 0, 0, time.UTC), nil
			}
		}

	default:
		var s string
		switch x := v.(type) {
		case string:
			s = x
		case float64:
			s = strconv.FormatFloat(x, 'f', -1, 64)
		case bool:
			s = strconv.FormatBool(x)
		default:
			return nil, invalid("значение фильтра по «%s» должно быть строкой", title(col))
		}
		if len(s) > maxStringValue {
			return nil, invalid("значение фильтра по «%s» длиннее %d символов", title(col), maxStringValue)
		}
		return s, nil
	}
	return nil, invalid("значение %v не подходит к колонке «%s» типа %s", v, title(col), col.Type)
}

var spanLayouts = []struct {
	layout string
	next   func(time.Time) time.Time
}{
	{"2006-01-02", func(t time.Time) time.Time { return t.AddDate(0, 0, 1) }},
	{"02.01.2006", func(t time.Time) time.Time { return t.AddDate(0, 0, 1) }},
	{"2006-01", func(t time.Time) time.Time { return t.AddDate(0, 1, 0) }},
	{"01.2006", func(t time.Time) time.Time { return t.AddDate(0, 1, 0) }},
	{"2006", func(t time.Time) time.Time { return t.AddDate(1, 0, 0) }},
}

// temporalSpan понимает значение фильтра по дате без времени как отрезок:
// «2026-09-30» — весь день, «2026-09» — месяц, «2025» — год. Иначе граница
// «по 30 сентября» по колонке со временем отрезала бы сам день 30 сентября.
func temporalSpan(v any, col domain.Column) (from, to time.Time, ok bool) {
	if !col.Type.IsTemporal() {
		return time.Time{}, time.Time{}, false
	}
	switch x := v.(type) {
	case string:
		s := strings.TrimSpace(x)
		for _, span := range spanLayouts {
			if t, err := time.Parse(span.layout, s); err == nil {
				return t, span.next(t), true
			}
		}
	case float64:
		if x == math.Trunc(x) && x >= 1900 && x <= 2200 {
			t := time.Date(int(x), time.January, 1, 0, 0, 0, 0, time.UTC)
			return t, t.AddDate(1, 0, 0), true
		}
	}
	return time.Time{}, time.Time{}, false
}

func coerceList(values []any, col domain.Column) (any, error) {
	converted := make([]any, 0, len(values))
	for _, v := range values {
		c, err := coerce(v, col)
		if err != nil {
			return nil, err
		}
		converted = append(converted, c)
	}

	switch col.Type {
	case domain.ColumnTypeInteger:
		return typed[int64](converted), nil
	case domain.ColumnTypeNumeric:
		return typed[float64](converted), nil
	case domain.ColumnTypeBoolean:
		return typed[bool](converted), nil
	case domain.ColumnTypeDate, domain.ColumnTypeTimestamp:
		return typed[time.Time](converted), nil
	default:
		return typed[string](converted), nil
	}
}

func typed[T any](values []any) []T {
	out := make([]T, 0, len(values))
	for _, v := range values {
		out = append(out, v.(T))
	}
	return out
}

func compactNumber(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, " ", "")
	return strings.ReplaceAll(s, ",", ".")
}
