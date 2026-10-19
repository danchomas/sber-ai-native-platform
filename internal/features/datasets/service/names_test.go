package service

import "testing"

func TestSanitizeIdentifier(t *testing.T) {
	cases := map[string]string{
		"User ID":           "user_id",
		"  Сумма, руб. ":    "summa_rub",
		"2023_год":          "c_2023_god",
		"order":             "order_col",
		"":                  "",
		"transaction--date": "transaction_date",
		"Код Клиента":       "kod_klienta",
		"Тип/Роль":          "tip_rol",
		"amount(USD)":       "amount_usd",
	}
	for in, want := range cases {
		if got := SanitizeIdentifier(in); got != want {
			t.Errorf("SanitizeIdentifier(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}

func TestUniqueNames(t *testing.T) {
	got := UniqueNames([]string{"id", "id", "", "Сумма", "сумма"})
	want := []string{"id", "id_2", "column_3", "summa", "summa_2"}
	if len(got) != len(want) {
		t.Fatalf("длина %d, ожидалось %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("позиция %d: %q, ожидалось %q", i, got[i], want[i])
		}
	}
}
