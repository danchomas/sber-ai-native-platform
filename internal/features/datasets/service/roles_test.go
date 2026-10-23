package service

import (
	"testing"

	"github.com/heinousness/aibi/internal/core/domain"
)

func profile(rows, nulls, distinct int64, top ...string) *domain.ColumnProfile {
	p := &domain.ColumnProfile{RowCount: rows, NullCount: nulls, DistinctCount: distinct}
	for _, v := range top {
		p.TopValues = append(p.TopValues, domain.ValueFrequency{Value: v, Count: 1})
	}
	return p
}

func TestLooksLikeIdentifier(t *testing.T) {
	yes := []string{"id", "user_id", "ID", "client_code", "order_no", "uuid", "id_клиента", "Код клиента"}
	no := []string{"amount", "created_at", "status", "comment", "идентичность"}

	for _, n := range yes {
		if !looksLikeIdentifier(n) {
			t.Errorf("%q должно распознаваться как идентификатор", n)
		}
	}
	for _, n := range no {
		if looksLikeIdentifier(n) {
			t.Errorf("%q не должно распознаваться как идентификатор", n)
		}
	}
}

func TestInferRole(t *testing.T) {
	cases := []struct {
		name string
		col  domain.Column
		prof *domain.ColumnProfile
		want domain.SemanticRole
	}{
		{"дата", domain.Column{Name: "created_at", Type: domain.ColumnTypeTimestamp}, nil, domain.RoleTemporal},
		{"идентификатор по имени", domain.Column{Name: "user_id", Type: domain.ColumnTypeText}, profile(100, 0, 90), domain.RoleIdentifier},
		{"числовая метрика", domain.Column{Name: "amount", Type: domain.ColumnTypeNumeric}, profile(1000, 0, 800), domain.RoleMeasure},
		{"числовой разрез", domain.Column{Name: "rating", Type: domain.ColumnTypeInteger}, profile(1000, 0, 5), domain.RoleDimension},
		{"категория", domain.Column{Name: "status", Type: domain.ColumnTypeText}, profile(1000, 0, 4, "new", "paid"), domain.RoleDimension},
		{"уникальный текст", domain.Column{Name: "email", Type: domain.ColumnTypeText}, profile(1000, 0, 1000), domain.RoleIdentifier},
		{"свободный текст", domain.Column{Name: "comment", Type: domain.ColumnTypeText}, profile(1000, 0, 980,
			"очень длинный пользовательский комментарий про качество обслуживания"), domain.RoleFreeText},
		{"флаг", domain.Column{Name: "is_active", Type: domain.ColumnTypeBoolean}, profile(100, 0, 2), domain.RoleDimension},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inferRole(tc.col, tc.prof); got != tc.want {
				t.Fatalf("получено %s, ожидалось %s", got, tc.want)
			}
		})
	}
}

func TestInferRoleRejectsLowCardinalityIntegerIdentifiers(t *testing.T) {
	// «Номер позиции»: пять значений на полмиллиона строк. Называется
	// идентификатором, но ничего не идентифицирует — и давал ложные
	// связи со всякой другой последовательностью 1..N.
	col := domain.Column{Name: "nomer_pozicii", SourceName: "Номер позиции", Type: domain.ColumnTypeInteger}
	prof := &domain.ColumnProfile{RowCount: 500000, DistinctCount: 5}

	if got := inferRole(col, prof); got == domain.RoleIdentifier {
		t.Errorf("колонка с 5 значениями на 500000 строк не должна быть идентификатором, получено %q", got)
	}
}

func TestInferRoleKeepsGenuineIntegerIdentifier(t *testing.T) {
	col := domain.Column{Name: "kod_klienta", SourceName: "Код клиента", Type: domain.ColumnTypeInteger}
	prof := &domain.ColumnProfile{RowCount: 500000, DistinctCount: 71839}

	if got := inferRole(col, prof); got != domain.RoleIdentifier {
		t.Errorf("колонка с 71839 значениями должна остаться идентификатором, получено %q", got)
	}
}

func TestInferRoleKeepsTextIdentifierRegardlessOfCardinality(t *testing.T) {
	// Текстовые коды идентифицируют по своей природе: правило про
	// количество значений к ним не применяется.
	col := domain.Column{Name: "kod_magazina", SourceName: "Код магазина", Type: domain.ColumnTypeText}
	prof := &domain.ColumnProfile{RowCount: 500000, DistinctCount: 128}

	if got := inferRole(col, prof); got != domain.RoleIdentifier {
		t.Errorf("текстовый код должен остаться идентификатором, получено %q", got)
	}
}
