package service

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/heinousness/aibi/internal/core/domain"
)

var nullTokens = map[string]struct{}{
	"": {}, "null": {}, "nil": {}, "none": {}, "nan": {}, "n/a": {}, "na": {},
	"-": {}, "--": {}, "\\n": {}, "undefined": {}, "нет данных": {}, "неизвестно": {},
	"н/д": {}, "н.д.": {}, "нд": {}, "не указано": {}, "отсутствует": {}, "без данных": {},
}

func IsNull(s string) bool {
	_, ok := nullTokens[strings.ToLower(strings.TrimSpace(s))]
	return ok
}

var boolTrue = map[string]struct{}{
	"true": {}, "t": {}, "yes": {}, "y": {}, "1": {}, "да": {}, "истина": {},
}

var boolFalse = map[string]struct{}{
	"false": {}, "f": {}, "no": {}, "n": {}, "0": {}, "нет": {}, "ложь": {},
}

var dateLayouts = []string{
	"2006-01-02",
	"02.01.2006",
	"01/02/2006",
	"02/01/2006",
	"2006/01/02",
	"20060102",
	"02-01-2006",
	"Jan 2, 2006",
}

var timestampLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999-07",
	"2006-01-02 15:04:05.999999",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"02.01.2006 15:04:05",
	"02.01.2006 15:04",
	"01/02/2006 15:04:05",
	"2006-01-02 15:04:05 MST",
}

type ValueParser struct {
	Type         domain.ColumnType
	Layout       string
	DecimalComma bool
}

var ErrNotParseable = errors.New("значение не соответствует типу колонки")

func (p ValueParser) Parse(raw string) (any, error) {
	s := strings.TrimSpace(raw)
	if IsNull(s) {
		return nil, nil
	}

	switch p.Type {
	case domain.ColumnTypeBoolean:
		low := strings.ToLower(s)
		if _, ok := boolTrue[low]; ok {
			return true, nil
		}
		if _, ok := boolFalse[low]; ok {
			return false, nil
		}
		return nil, ErrNotParseable

	case domain.ColumnTypeInteger:
		n, err := strconv.ParseInt(cleanNumber(s, false), 10, 64)
		if err != nil {
			return nil, ErrNotParseable
		}
		return n, nil

	case domain.ColumnTypeNumeric:
		f, err := strconv.ParseFloat(cleanNumber(s, p.DecimalComma), 64)
		if err != nil {
			return nil, ErrNotParseable
		}
		return f, nil

	case domain.ColumnTypeDate, domain.ColumnTypeTimestamp:
		t, err := time.Parse(p.Layout, s)
		if err != nil {
			return nil, ErrNotParseable
		}
		return t, nil

	default:
		return s, nil
	}
}

func cleanNumber(s string, decimalComma bool) string {
	s = strings.NewReplacer(" ", "", " ", "", "_", "").Replace(s)
	if decimalComma {
		s = strings.ReplaceAll(s, ",", ".")
	} else {
		s = strings.ReplaceAll(s, ",", "")
	}
	return strings.TrimPrefix(s, "+")
}

func InferType(samples []string) ValueParser {
	values := make([]string, 0, len(samples))
	for _, s := range samples {
		if s = strings.TrimSpace(s); !IsNull(s) {
			values = append(values, s)
		}
	}
	if len(values) == 0 {
		return ValueParser{Type: domain.ColumnTypeText}
	}

	if allMatch(values, isBool) {
		return ValueParser{Type: domain.ColumnTypeBoolean}
	}
	if !anyMatch(values, hasLeadingZero) {
		if decimalComma := decimalCommaStyle(values); decimalComma {
			if allMatch(values, func(s string) bool { return isFloat(s, true) }) {
				return ValueParser{Type: domain.ColumnTypeNumeric, DecimalComma: true}
			}
		} else {
			if allMatch(values, isInteger) {
				return ValueParser{Type: domain.ColumnTypeInteger}
			}
			if allMatch(values, func(s string) bool { return isFloat(s, false) }) {
				return ValueParser{Type: domain.ColumnTypeNumeric}
			}
		}
	}
	if layout, ok := matchLayout(values, dateLayouts); ok {
		return ValueParser{Type: domain.ColumnTypeDate, Layout: layout}
	}
	if layout, ok := matchLayout(values, timestampLayouts); ok {
		return ValueParser{Type: domain.ColumnTypeTimestamp, Layout: layout}
	}
	return ValueParser{Type: domain.ColumnTypeText}
}

func matchLayout(values []string, layouts []string) (string, bool) {
	for _, layout := range layouts {
		if allMatch(values, func(s string) bool {
			_, err := time.Parse(layout, s)
			return err == nil
		}) {
			return layout, true
		}
	}
	return "", false
}

func allMatch(values []string, pred func(string) bool) bool {
	for _, v := range values {
		if !pred(v) {
			return false
		}
	}
	return true
}

func anyMatch(values []string, pred func(string) bool) bool {
	for _, v := range values {
		if pred(v) {
			return true
		}
	}
	return false
}

func isBool(s string) bool {
	low := strings.ToLower(s)
	_, t := boolTrue[low]
	_, f := boolFalse[low]
	return t || f
}

func isInteger(s string) bool {
	_, err := strconv.ParseInt(cleanNumber(s, false), 10, 64)
	return err == nil
}

func isFloat(s string, decimalComma bool) bool {
	_, err := strconv.ParseFloat(cleanNumber(s, decimalComma), 64)
	return err == nil
}

func decimalCommaStyle(values []string) bool {
	found := false
	for _, v := range values {
		if !strings.Contains(v, ",") {
			continue
		}
		parts := strings.Split(v, ",")
		if len(parts) != 2 || !allDigits(parts[1]) || len(parts[1]) == 3 {
			return false
		}
		found = true
	}
	return found
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func hasLeadingZero(s string) bool {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "+"), "-")
	return len(s) > 1 && s[0] == '0' && s[1] != '.' && s[1] != ','
}
