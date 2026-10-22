package sqlguard

import (
	"fmt"
	"strings"
	"unicode"
)

type tokenKind int

const (
	tokWord tokenKind = iota
	tokQuotedIdent
	tokString
	tokNumber
	tokPunct
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

func (t token) upper() string { return strings.ToUpper(t.text) }

func (t token) isWord(word string) bool {
	return t.kind == tokWord && t.upper() == word
}

func (t token) isPunct(p string) bool {
	return t.kind == tokPunct && t.text == p
}

func tokenize(sql string) ([]token, error) {
	var (
		out []token
		r   = []rune(sql)
		i   int
	)

	for i < len(r) {
		c := r[i]

		switch {
		case unicode.IsSpace(c):
			i++

		case c == '-' && i+1 < len(r) && r[i+1] == '-':
			for i < len(r) && r[i] != '\n' {
				i++
			}

		case c == '/' && i+1 < len(r) && r[i+1] == '*':
			depth, start := 1, i
			i += 2
			for i < len(r) && depth > 0 {
				switch {
				case r[i] == '/' && i+1 < len(r) && r[i+1] == '*':
					depth++
					i += 2
				case r[i] == '*' && i+1 < len(r) && r[i+1] == '/':
					depth--
					i += 2
				default:
					i++
				}
			}
			if depth > 0 {
				return nil, fmt.Errorf("незакрытый комментарий на позиции %d", start)
			}

		case c == '\'':
			start := i
			i++
			for i < len(r) {
				if r[i] == '\'' {
					if i+1 < len(r) && r[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}

				if r[i] == '\\' && i+1 < len(r) {
					i += 2
					continue
				}
				i++
			}
			if i > len(r) || (i == len(r) && r[len(r)-1] != '\'') {
				return nil, fmt.Errorf("незакрытый строковый литерал на позиции %d", start)
			}
			out = append(out, token{kind: tokString, text: string(r[start:i]), pos: start})

		case c == '"':
			start := i
			i++
			var sb strings.Builder
			closed := false
			for i < len(r) {
				if r[i] == '"' {
					if i+1 < len(r) && r[i+1] == '"' {
						sb.WriteRune('"')
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				sb.WriteRune(r[i])
				i++
			}
			if !closed {
				return nil, fmt.Errorf("незакрытый идентификатор в кавычках на позиции %d", start)
			}
			out = append(out, token{kind: tokQuotedIdent, text: sb.String(), pos: start})

		case c == '$':

			if tag, end, ok := dollarTag(r, i); ok {
				closeIdx := indexRunes(r, end, tag)
				if closeIdx < 0 {
					return nil, fmt.Errorf("незакрытая доллар-кавычка на позиции %d", i)
				}
				out = append(out, token{kind: tokString, text: string(r[i : closeIdx+len(tag)]), pos: i})
				i = closeIdx + len(tag)
				continue
			}
			out = append(out, token{kind: tokPunct, text: "$", pos: i})
			i++

		case unicode.IsDigit(c):
			start := i
			for i < len(r) && (unicode.IsDigit(r[i]) || r[i] == '.' || r[i] == 'e' || r[i] == 'E') {
				i++
			}
			out = append(out, token{kind: tokNumber, text: string(r[start:i]), pos: start})

		case isIdentStart(c):
			start := i
			for i < len(r) && isIdentPart(r[i]) {
				i++
			}
			out = append(out, token{kind: tokWord, text: string(r[start:i]), pos: start})

		default:
			out = append(out, token{kind: tokPunct, text: string(c), pos: i})
			i++
		}
	}
	return out, nil
}

func dollarTag(r []rune, i int) (tag []rune, end int, ok bool) {
	j := i + 1
	for j < len(r) && (isIdentPart(r[j])) {
		j++
	}
	if j < len(r) && r[j] == '$' {
		return r[i : j+1], j + 1, true
	}
	return nil, i, false
}

func indexRunes(r []rune, from int, needle []rune) int {
	for i := from; i+len(needle) <= len(r); i++ {
		match := true
		for j := range needle {
			if r[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func isIdentStart(c rune) bool {
	return c == '_' || unicode.IsLetter(c)
}

func isIdentPart(c rune) bool {
	return c == '_' || c == '$' || unicode.IsLetter(c) || unicode.IsDigit(c)
}
