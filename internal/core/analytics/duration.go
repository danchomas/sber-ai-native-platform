package analytics

import (
	"fmt"
	"slices"
	"strings"

	"github.com/heinousness/aibi/internal/core/domain"
)

// Duration задаёт календарный интервал между двумя колонками дат, а не трудозатраты.
type Duration struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Unit  string `json:"unit"`
}

var DurationUnits = []string{"minute", "hour", "day"}

func (c *compiler) temporalRef(name string) (string, domain.Column, error) {
	bt, col, err := c.column(name)
	if err != nil {
		return "", domain.Column{}, err
	}
	if !col.Type.IsTemporal() {
		return "", domain.Column{}, invalid("колонка «%s» не содержит дат", title(col))
	}
	return qualify(bt.alias, col.Name), col, nil
}

func (c *compiler) durationRefs(d Duration) (start, end, divisor string, err error) {
	start, _, err = c.temporalRef(d.Start)
	if err != nil {
		return
	}
	end, _, err = c.temporalRef(d.End)
	if err != nil {
		return
	}
	if start == end {
		err = invalid("начало и конец длительности должны быть разными колонками")
		return
	}
	switch d.Unit {
	case "minute":
		divisor = "60.0"
	case "hour":
		divisor = "3600.0"
	case "day":
		divisor = "86400.0"
	default:
		err = invalid("единица длительности должна быть minute, hour или day")
	}
	return
}

func durationValue(start, end, divisor string) string {
	return fmt.Sprintf("date_part('epoch', (%s::timestamp - %s::timestamp))::numeric / %s", end, start, divisor)
}

func (c *compiler) durationExpr(d Duration) (string, error) {
	start, end, divisor, err := c.durationRefs(d)
	if err != nil {
		return "", err
	}
	const warning = "Длительность — календарное время, не трудозатраты и не рабочие часы. Пропущенные даты и отрицательные интервалы исключены из метрики, а не заменены нулём; count без duration считает все строки."
	if !slices.Contains(c.warns, warning) {
		c.warns = append(c.warns, warning)
	}
	return fmt.Sprintf("CASE WHEN %s >= %s THEN %s END", end, start, durationValue(start, end, divisor)), nil
}

func (c *compiler) measureValue(m Metric) (string, error) {
	count := 0
	for _, present := range []bool{strings.TrimSpace(m.Column) != "", strings.TrimSpace(m.Expr) != "", m.Duration != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return "", invalid("для показателя укажите ровно одно: column, expr или duration")
	}
	if m.Per != "" || m.Scale != nil {
		return "", invalid("сравнение распределений не поддерживает per и scale")
	}
	if m.Duration != nil {
		return c.durationExpr(*m.Duration)
	}
	if m.Expr != "" {
		return c.compileExpr(m.Expr, nil)
	}
	bt, col, err := c.column(m.Column)
	if err != nil {
		return "", err
	}
	if !col.Type.IsNumeric() {
		return "", invalid("для сравнения нужна числовая колонка или duration")
	}
	return qualify(bt.alias, col.Name) + "::numeric", nil
}
