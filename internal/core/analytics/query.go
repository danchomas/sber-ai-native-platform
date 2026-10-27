// Package analytics переводит типизированный аналитический запрос в SQL.
//
// Это главная граница безопасности агента: модель не пишет SQL, а заполняет
// поля Query. Каждое поле сверяется с белым списком — датасеты и колонки
// только из каталога, функции и операторы только из перечислений ниже, —
// а значения фильтров уходят в базу параметрами, а не текстом запроса.
// Вредный запрос в таком языке невыразим. Готовый SQL всё равно проходит
// sqlguard: второй слой защиты не полагается на корректность первого.
package analytics

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/heinousness/aibi/internal/core/apperr"
)

// Query — запрос в терминах каталога: что посчитать, в каком разрезе, по каким строкам.
type Query struct {
	Dataset string     `json:"dataset"`
	Join    []string   `json:"join,omitempty"`
	Metrics []Metric   `json:"metrics,omitempty"`
	GroupBy []string   `json:"group_by,omitempty"`
	Time    *TimeGroup `json:"time,omitempty"`
	Filters []Filter   `json:"filters,omitempty"`
	Sort    *Sort      `json:"sort,omitempty"`
	Limit   int        `json:"limit,omitempty"`
}

type Metric struct {
	Func   Func   `json:"fn"`
	Column string `json:"column,omitempty"`
	Label  string `json:"label,omitempty"`
}

type TimeGroup struct {
	Column string `json:"column"`
	Grain  Grain  `json:"grain"`
}

type Filter struct {
	Column string `json:"column"`
	Op     Op     `json:"op"`
	Value  any    `json:"value,omitempty"`
	Values []any  `json:"values,omitempty"`
}

type Sort struct {
	By   string `json:"by"`
	Desc bool   `json:"desc"`
}

type Func string

const (
	FuncCount         Func = "count"
	FuncCountDistinct Func = "count_distinct"
	FuncSum           Func = "sum"
	FuncAvg           Func = "avg"
	FuncMin           Func = "min"
	FuncMax           Func = "max"
	FuncMedian        Func = "median"
	FuncShare         Func = "share"
)

var Funcs = []Func{FuncCount, FuncCountDistinct, FuncSum, FuncAvg, FuncMin, FuncMax, FuncMedian, FuncShare}

type Grain string

const (
	GrainDay           Grain = "day"
	GrainWeek          Grain = "week"
	GrainMonth         Grain = "month"
	GrainQuarter       Grain = "quarter"
	GrainYear          Grain = "year"
	GrainMonthOfYear   Grain = "month_of_year"
	GrainQuarterOfYear Grain = "quarter_of_year"
	GrainDayOfWeek     Grain = "day_of_week"
	GrainHour          Grain = "hour_of_day"
)

var Grains = []Grain{
	GrainDay, GrainWeek, GrainMonth, GrainQuarter, GrainYear,
	GrainMonthOfYear, GrainQuarterOfYear, GrainDayOfWeek, GrainHour,
}

type Op string

const (
	OpEq       Op = "eq"
	OpNe       Op = "ne"
	OpGt       Op = "gt"
	OpGte      Op = "gte"
	OpLt       Op = "lt"
	OpLte      Op = "lte"
	OpIn       Op = "in"
	OpNotIn    Op = "not_in"
	OpBetween  Op = "between"
	OpContains Op = "contains"
	OpIsNull   Op = "is_null"
	OpNotNull  Op = "not_null"
)

var Ops = []Op{OpEq, OpNe, OpGt, OpGte, OpLt, OpLte, OpIn, OpNotIn, OpBetween, OpContains, OpIsNull, OpNotNull}

// Пределы держат запрос в размерах, которые база посчитает быстро,
// а человек прочитает на одном графике.
const (
	MaxMetrics     = 6
	MaxGroupBy     = 3
	MaxFilters     = 10
	MaxJoins       = 2
	MaxInValues    = 100
	DefaultLimit   = 100
	maxLabelBytes  = 63
	maxStringValue = 500
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", apperr.ErrValidation, fmt.Sprintf(format, args...))
}

// label обрезает подпись до предела идентификатора PostgreSQL по границе
// символа: иначе база сама обрежет её посреди кириллической буквы.
func label(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= maxLabelBytes {
		return s
	}
	cut := maxLabelBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimSpace(s[:cut])
}
