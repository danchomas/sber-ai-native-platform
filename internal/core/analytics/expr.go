package analytics

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/heinousness/aibi/internal/core/domain"
)

const (
	maxExprRunes = 300
	maxExprDepth = 8
)

const exprRules = "в формуле допустимы числовые колонки, числа, операции + - * / и скобки, " +
	"а также разность двух колонок с датами — длительность в часах"

const calcRules = "в расчёте по итогам колонки стоят внутри sum(), avg(), min(), max(), count() или count_distinct(), " +
	"например (sum(\"Выручка\") - sum(\"Расходы\")) / sum(\"Расходы\") * 100"

var exprAggregates = map[string]string{
	"sum": "sum(%s)", "avg": "avg(%s)", "min": "min(%s)", "max": "max(%s)",
	"count": "count(%s)", "count_distinct": "count(DISTINCT %s)",
}

const exprDateRule = "с датами в формуле допустима только разность двух колонок: " +
	"«Завершено» - «Начало» даёт длительность в часах, в днях — поделите её на 24"

type exprToken struct {
	kind  byte
	value string
}

const (
	exprColumn byte = 'c'
	exprNumber byte = 'n'
	exprOp     byte = 'o'
)

func tokenizeExpr(src string) ([]exprToken, error) {
	runes := []rune(src)
	if len(runes) > maxExprRunes {
		return nil, invalid("формула длиннее %d символов", maxExprRunes)
	}
	var out []exprToken
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case strings.ContainsRune("+-*/()", r):
			out = append(out, exprToken{exprOp, string(r)})
			i++
		case r == '"' || r == '«':
			closing := '"'
			if r == '«' {
				closing = '»'
			}
			end := i + 1
			for end < len(runes) && runes[end] != closing {
				end++
			}
			if end == len(runes) {
				return nil, invalid("в формуле не закрыта кавычка: %s", exprRules)
			}
			out = append(out, exprToken{exprColumn, string(runes[i+1 : end])})
			i = end + 1
		case unicode.IsDigit(r):
			end := i
			for end < len(runes) && (unicode.IsDigit(runes[end]) || runes[end] == '.' || runes[end] == ',') {
				end++
			}
			out = append(out, exprToken{exprNumber, strings.ReplaceAll(string(runes[i:end]), ",", ".")})
			i = end
		case unicode.IsLetter(r) || r == '_':
			end := i
			for end < len(runes) && (unicode.IsLetter(runes[end]) || unicode.IsDigit(runes[end]) || runes[end] == '_') {
				end++
			}
			out = append(out, exprToken{exprColumn, string(runes[i:end])})
			i = end
		default:
			return nil, invalid("символ %q: %s", string(r), exprRules)
		}
	}
	if len(out) == 0 {
		return nil, invalid("пустая формула: %s", exprRules)
	}
	return out, nil
}

type exprParser struct {
	c      *compiler
	tokens []exprToken
	pos    int
	scale  *Scale
	scaled bool

	// Режим расчёта по итогам: формула из агрегатов, условие метрики
	// навешивается на каждый агрегат.
	calc        bool
	cond        metricCond
	inAggregate bool
	aggregates  int
}

type exprValue struct {
	sql      string
	temporal bool
}

func (c *compiler) compileExpr(src string, scale *Scale) (string, error) {
	return c.parseExpr(&exprParser{c: c, scale: scale}, src)
}

// compileCalc собирает показатель из итогов: отношение сумм, разность средних.
// Колонки вне агрегата запрещены — у итоговой строки нет «значения колонки».
func (c *compiler) compileCalc(src string, cond metricCond) (string, error) {
	p := &exprParser{c: c, calc: true, cond: cond}
	sql, err := c.parseExpr(p, src)
	if err != nil {
		return "", err
	}
	if p.aggregates == 0 {
		return "", invalid("в расчёте по итогам нет ни одной функции: %s", calcRules)
	}
	return sql, nil
}

func (c *compiler) parseExpr(p *exprParser, src string) (string, error) {
	tokens, err := tokenizeExpr(src)
	if err != nil {
		return "", err
	}
	p.tokens = tokens
	v, err := p.sum(0)
	if err != nil {
		return "", err
	}
	if p.pos != len(p.tokens) {
		return "", invalid("лишнее в конце формулы: %s", exprRules)
	}
	if v.temporal {
		return "", invalid("дата не может быть показателем: %s", exprDateRule)
	}
	if p.scale != nil && !p.scaled {
		return "", invalid("показатель не зависит от колонки %q: добавьте её в формулу", p.scale.Column)
	}
	return v.sql, nil
}

func (p *exprParser) peekOp(ops string) (string, bool) {
	if p.pos < len(p.tokens) && p.tokens[p.pos].kind == exprOp && strings.Contains(ops, p.tokens[p.pos].value) {
		return p.tokens[p.pos].value, true
	}
	return "", false
}

func (p *exprParser) sum(depth int) (exprValue, error) {
	left, err := p.product(depth)
	if err != nil {
		return exprValue{}, err
	}
	for {
		op, ok := p.peekOp("+-")
		if !ok {
			return left, nil
		}
		p.pos++
		right, err := p.product(depth)
		if err != nil {
			return exprValue{}, err
		}
		switch {
		case left.temporal && right.temporal && op == "-":
			left = exprValue{sql: "(date_part('epoch', " + left.sql + " - " + right.sql + ") / 3600)::numeric"}
		case left.temporal || right.temporal:
			return exprValue{}, invalid("%s", exprDateRule)
		default:
			left = exprValue{sql: "(" + left.sql + " " + op + " " + right.sql + ")"}
		}
	}
}

func (p *exprParser) product(depth int) (exprValue, error) {
	left, err := p.unary(depth)
	if err != nil {
		return exprValue{}, err
	}
	for {
		op, ok := p.peekOp("*/")
		if !ok {
			return left, nil
		}
		p.pos++
		right, err := p.unary(depth)
		if err != nil {
			return exprValue{}, err
		}
		if left.temporal || right.temporal {
			return exprValue{}, invalid("%s", exprDateRule)
		}
		if op == "/" {
			right.sql = "NULLIF(" + right.sql + ", 0)"
		}
		left = exprValue{sql: "(" + left.sql + " " + op + " " + right.sql + ")"}
	}
}

func (p *exprParser) unary(depth int) (exprValue, error) {
	if _, ok := p.peekOp("-"); ok {
		p.pos++
		value, err := p.unary(depth)
		if err != nil {
			return exprValue{}, err
		}
		if value.temporal {
			return exprValue{}, invalid("%s", exprDateRule)
		}
		return exprValue{sql: "(-" + value.sql + ")"}, nil
	}
	return p.atom(depth)
}

func (p *exprParser) atom(depth int) (exprValue, error) {
	if p.pos >= len(p.tokens) {
		return exprValue{}, invalid("формула обрывается: %s", exprRules)
	}
	tok := p.tokens[p.pos]
	p.pos++
	switch {
	case tok.kind == exprOp && tok.value == "(":
		if depth >= maxExprDepth {
			return exprValue{}, invalid("в формуле больше %d уровней скобок", maxExprDepth)
		}
		inner, err := p.sum(depth + 1)
		if err != nil {
			return exprValue{}, err
		}
		if _, ok := p.peekOp(")"); !ok {
			return exprValue{}, invalid("в формуле не закрыта скобка: %s", exprRules)
		}
		p.pos++
		return inner, nil
	case tok.kind == exprNumber:
		v, err := strconv.ParseFloat(tok.value, 64)
		if err != nil {
			return exprValue{}, invalid("число %q: %s", tok.value, exprRules)
		}
		return exprValue{sql: strconv.FormatFloat(v, 'f', -1, 64)}, nil
	case tok.kind == exprColumn:
		if _, open := p.peekOp("("); open {
			if pattern, ok := exprAggregates[strings.ToLower(tok.value)]; ok {
				return p.aggregate(pattern, depth)
			}
		}
		if p.calc && !p.inAggregate {
			return exprValue{}, invalid("колонка %q вне функции: %s", tok.value, calcRules)
		}
		return p.columnRef(tok.value)
	}
	return exprValue{}, invalid("неожиданное %q: %s", tok.value, exprRules)
}

func (p *exprParser) aggregate(pattern string, depth int) (exprValue, error) {
	switch {
	case !p.calc:
		return exprValue{}, invalid("функции sum(), avg() и другие допустимы только в метрике с fn calc: %s", calcRules)
	case p.inAggregate:
		return exprValue{}, invalid("функции нельзя вкладывать друг в друга: %s", calcRules)
	case depth >= maxExprDepth:
		return exprValue{}, invalid("в формуле больше %d уровней скобок", maxExprDepth)
	}
	p.pos++
	p.aggregates++
	if _, closed := p.peekOp(")"); closed && pattern == "count(%s)" {
		p.pos++
		return exprValue{sql: p.cond.apply("count(*)")}, nil
	}
	if p.pos+1 < len(p.tokens) && p.tokens[p.pos].value == "*" && p.tokens[p.pos+1].value == ")" && pattern == "count(%s)" {
		p.pos += 2
		return exprValue{sql: p.cond.apply("count(*)")}, nil
	}
	p.inAggregate = true
	inner, err := p.sum(depth + 1)
	p.inAggregate = false
	if err != nil {
		return exprValue{}, err
	}
	if inner.temporal {
		return exprValue{}, invalid("дату нельзя складывать или усреднять: %s", exprDateRule)
	}
	if _, ok := p.peekOp(")"); !ok {
		return exprValue{}, invalid("в формуле не закрыта скобка функции: %s", calcRules)
	}
	p.pos++
	return exprValue{sql: p.cond.apply(fmt.Sprintf(pattern, inner.sql))}, nil
}

func (p *exprParser) columnRef(name string) (exprValue, error) {
	bt, col, err := p.c.column(name)
	if err != nil {
		return exprValue{}, err
	}
	switch {
	case col.Type == domain.ColumnTypeDate:
		return exprValue{sql: qualify(bt.alias, col.Name) + "::timestamp", temporal: true}, nil
	case col.Type.IsTemporal():
		return exprValue{sql: qualify(bt.alias, col.Name), temporal: true}, nil
	case !col.Type.IsNumeric():
		return exprValue{}, invalid("колонка «%s» типа %s не числовая: %s", title(col), col.Type, exprRules)
	}
	ref := qualify(bt.alias, col.Name) + "::numeric"
	if p.scale != nil {
		_, target, err := p.c.column(p.scale.Column)
		if err != nil {
			return exprValue{}, err
		}
		if target.Name == col.Name {
			p.scaled = true
			return exprValue{sql: "(" + ref + " * " + strconv.FormatFloat(p.scale.Factor, 'f', -1, 64) + ")"}, nil
		}
	}
	return exprValue{sql: ref}, nil
}

func (c *compiler) exprUsesDates(src string) bool {
	tokens, err := tokenizeExpr(src)
	if err != nil {
		return false
	}
	for _, tok := range tokens {
		if tok.kind != exprColumn {
			continue
		}
		if _, col, err := c.column(tok.value); err == nil && col.Type.IsTemporal() {
			return true
		}
	}
	return false
}

// IsDuration сообщает, что формула метрики — длительность между датами:
// у таких метрик значение известно не у всех строк, и полноту надо проверять.
func IsDuration(m Metric, cat Catalog, dataset string) bool {
	if strings.TrimSpace(m.Expr) == "" || strings.EqualFold(string(m.Func), string(FuncCalc)) {
		return false
	}
	c := &compiler{cat: cat}
	if err := c.bind(dataset, nil); err != nil {
		return false
	}
	return c.exprUsesDates(m.Expr)
}
