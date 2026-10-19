package service

import (
	"fmt"
	"strings"
	"unicode"
)

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "h", 'ц': "c", 'ч': "ch", 'ш': "sh", 'щ': "sch", 'ъ': "",
	'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

var reservedWords = map[string]struct{}{
	"select": {}, "from": {}, "where": {}, "group": {}, "order": {}, "by": {},
	"join": {}, "on": {}, "and": {}, "or": {}, "not": {}, "null": {}, "as": {},
	"table": {}, "column": {}, "user": {}, "all": {}, "any": {}, "case": {},
	"when": {}, "then": {}, "else": {}, "end": {}, "limit": {}, "offset": {},
	"union": {}, "default": {}, "primary": {}, "references": {}, "check": {},
	"create": {}, "drop": {}, "set": {}, "update": {}, "delete": {}, "insert": {},
	"into": {}, "values": {}, "distinct": {}, "having": {}, "using": {},
}

func SanitizeIdentifier(raw string) string {
	var sb strings.Builder
	prevUnderscore := false

	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			sb.WriteRune(r)
			prevUnderscore = false
		case translit[r] != "":
			sb.WriteString(translit[r])
			prevUnderscore = false
		case r == 'ъ' || r == 'ь':
		default:
			if !prevUnderscore && sb.Len() > 0 {
				sb.WriteByte('_')
				prevUnderscore = true
			}
		}
	}

	name := strings.Trim(sb.String(), "_")
	if name == "" {
		return ""
	}
	if name[0] >= '0' && name[0] <= '9' {
		name = "c_" + name
	}
	if _, reserved := reservedWords[name]; reserved {
		name += "_col"
	}
	if len(name) > 55 {
		name = strings.Trim(name[:55], "_")
	}
	return name
}

func UniqueNames(raw []string) []string {
	used := make(map[string]int, len(raw))
	out := make([]string, len(raw))

	for i, r := range raw {
		name := SanitizeIdentifier(r)
		if name == "" {
			name = fmt.Sprintf("column_%d", i+1)
		}
		if n, exists := used[name]; exists {
			used[name] = n + 1
			name = fmt.Sprintf("%s_%d", name, n+1)
		}
		used[name] = 1
		out[i] = name
	}
	return out
}

func TableName(datasetName, suffix string) string {
	base := SanitizeIdentifier(datasetName)
	if base == "" {
		base = "dataset"
	}
	if len(base) > 40 {
		base = strings.Trim(base[:40], "_")
	}
	return fmt.Sprintf("ds_%s_%s", base, suffix)
}
