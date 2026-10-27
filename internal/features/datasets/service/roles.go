package service

import (
	"strings"

	"github.com/heinousness/aibi/internal/core/domain"
)

var (
	idSuffixes = []string{
		"_id", "_key", "_code", "_uuid", "_guid", "_no", "_num", "_number",
		"_kod", "_код", "_nomer", "_номер", "_identifikator",
	}
	idPrefixes = []string{"id_", "kod_", "код_", "nomer_", "номер_", "identifikator_"}
	idTokens   = []string{"uuid", "guid", "inn", "ogrn", "snils"}
	idExact    = []string{"id", "key", "code", "pk", "uid", "kod", "код", "nomer", "номер"}
)

func looksLikeIdentifier(names ...string) bool {
	for _, raw := range names {
		n := strings.ToLower(strings.TrimSpace(raw))
		n = strings.NewReplacer(" ", "_", "-", "_", ".", "_").Replace(n)
		if n == "" {
			continue
		}
		for _, exact := range idExact {
			if n == exact {
				return true
			}
		}
		for _, suffix := range idSuffixes {
			if strings.HasSuffix(n, suffix) {
				return true
			}
		}
		for _, token := range idTokens {
			if strings.Contains(n, token) {
				return true
			}
		}
		for _, prefix := range idPrefixes {
			if strings.HasPrefix(n, prefix) {
				return true
			}
		}
	}
	return false
}

func identifiesEnough(c domain.Column, p *domain.ColumnProfile) bool {
	if c.Type != domain.ColumnTypeInteger {
		return true
	}
	if p == nil || p.RowCount == 0 {
		return true
	}
	notNull := p.RowCount - p.NullCount
	if notNull <= 0 {
		return false
	}
	if p.DistinctCount >= minIntegerIdentifierValues {
		return true
	}

	return float64(p.DistinctCount)/float64(notNull) > 0.5
}

const minIntegerIdentifierValues = 1000

func inferRole(c domain.Column, p *domain.ColumnProfile) domain.SemanticRole {
	if c.Type.IsTemporal() {
		return domain.RoleTemporal
	}

	nameIsID := looksLikeIdentifier(c.Name, c.SourceName)
	if nameIsID && c.Type != domain.ColumnTypeNumeric && identifiesEnough(c, p) {
		return domain.RoleIdentifier
	}

	switch c.Type {
	case domain.ColumnTypeBoolean:
		return domain.RoleDimension
	case domain.ColumnTypeInteger, domain.ColumnTypeNumeric:
		if p != nil && p.Unique() && nameIsID {
			return domain.RoleIdentifier
		}

		if p != nil && p.DistinctCount > 0 && p.DistinctCount <= 12 && p.RowCount > 50 {
			return domain.RoleDimension
		}
		return domain.RoleMeasure
	}

	if p == nil {
		return domain.RoleDimension
	}
	notNull := p.RowCount - p.NullCount
	if notNull <= 0 {
		return domain.RoleUnknown
	}
	ratio := float64(p.DistinctCount) / float64(notNull)

	switch {
	case p.Unique():
		return domain.RoleIdentifier
	case p.DistinctCount <= 200 && ratio < 0.5:
		return domain.RoleDimension
	case ratio > 0.9 && avgValueLength(p) > 40:
		return domain.RoleFreeText
	case ratio > 0.9:
		return domain.RoleIdentifier
	default:
		return domain.RoleDimension
	}
}

func avgValueLength(p *domain.ColumnProfile) int {
	if len(p.TopValues) == 0 {
		if p.MaxValue != nil {
			return len(*p.MaxValue)
		}
		return 0
	}
	total := 0
	for _, v := range p.TopValues {
		total += len([]rune(v.Value))
	}
	return total / len(p.TopValues)
}
