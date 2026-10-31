package analytics

import (
	"strconv"
	"strings"
	"unicode"
)

const (
	maxExprRunes = 300
	maxExprDepth = 8
)

const exprRules = "в формуле допустимы только числовые колонки, числа, операции + - * / и скобки"

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
}

func (c *compiler) compileExpr(src string, scale *Scale) (string, error) {
	tokens, err := tokenizeExpr(src)
	if err != nil {
		return "", err
	}
	p := &exprParser{c: c, tokens: tokens, scale: scale}
	sql, err := p.sum(0)
	if err != nil {
		return "", err
	}
	if p.pos != len(p.tokens) {
		return "", invalid("лишнее в конце формулы: %s", exprRules)
	}
	if scale != nil && !p.scaled {
		return "", invalid("показатель не зависит от колонки %q: добавьте её в формулу", scale.Column)
	}
	return sql, nil
}

func (p *exprParser) peekOp(ops string) (string, bool) {
	if p.pos < len(p.tokens) && p.tokens[p.pos].kind == exprOp && strings.Contains(ops, p.tokens[p.pos].value) {
		return p.tokens[p.pos].value, true
	}
	return "", false
}

func (p *exprParser) sum(depth int) (string, error) {
	left, err := p.product(depth)
	if err != nil {
		return "", err
	}
	for {
		op, ok := p.peekOp("+-")
		if !ok {
			return left, nil
		}
		p.pos++
		right, err := p.product(depth)
		if err != nil {
			return "", err
		}
		left = "(" + left + " " + op + " " + right + ")"
	}
}

func (p *exprParser) product(depth int) (string, error) {
	left, err := p.unary(depth)
	if err != nil {
		return "", err
	}
	for {
		op, ok := p.peekOp("*/")
		if !ok {
			return left, nil
		}
		p.pos++
		right, err := p.unary(depth)
		if err != nil {
			return "", err
		}
		if op == "/" {
			right = "NULLIF(" + right + ", 0)"
		}
		left = "(" + left + " " + op + " " + right + ")"
	}
}

func (p *exprParser) unary(depth int) (string, error) {
	if _, ok := p.peekOp("-"); ok {
		p.pos++
		value, err := p.unary(depth)
		if err != nil {
			return "", err
		}
		return "(-" + value + ")", nil
	}
	return p.atom(depth)
}

func (p *exprParser) atom(depth int) (string, error) {
	if p.pos >= len(p.tokens) {
		return "", invalid("формула обрывается: %s", exprRules)
	}
	tok := p.tokens[p.pos]
	p.pos++
	switch {
	case tok.kind == exprOp && tok.value == "(":
		if depth >= maxExprDepth {
			return "", invalid("в формуле больше %d уровней скобок", maxExprDepth)
		}
		inner, err := p.sum(depth + 1)
		if err != nil {
			return "", err
		}
		if _, ok := p.peekOp(")"); !ok {
			return "", invalid("в формуле не закрыта скобка: %s", exprRules)
		}
		p.pos++
		return inner, nil
	case tok.kind == exprNumber:
		v, err := strconv.ParseFloat(tok.value, 64)
		if err != nil {
			return "", invalid("число %q: %s", tok.value, exprRules)
		}
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case tok.kind == exprColumn:
		return p.columnRef(tok.value)
	}
	return "", invalid("неожиданное %q: %s", tok.value, exprRules)
}

func (p *exprParser) columnRef(name string) (string, error) {
	bt, col, err := p.c.column(name)
	if err != nil {
		return "", err
	}
	if !col.Type.IsNumeric() {
		return "", invalid("колонка «%s» типа %s не числовая: %s", title(col), col.Type, exprRules)
	}
	ref := qualify(bt.alias, col.Name) + "::numeric"
	if p.scale != nil {
		_, target, err := p.c.column(p.scale.Column)
		if err != nil {
			return "", err
		}
		if target.Name == col.Name {
			p.scaled = true
			return "(" + ref + " * " + strconv.FormatFloat(p.scale.Factor, 'f', -1, 64) + ")", nil
		}
	}
	return ref, nil
}
