package ingest

import (
	"testing"

	"github.com/heinousness/aibi/internal/domain"
)

func TestInferType(t *testing.T) {
	cases := []struct {
		name    string
		samples []string
		want    domain.ColumnType
	}{
		{"целые", []string{"1", "2", "300"}, domain.ColumnTypeInteger},
		{"целые с разделителями", []string{"1 000", "2 500"}, domain.ColumnTypeInteger},
		{"дробные", []string{"1.5", "2.25"}, domain.ColumnTypeNumeric},
		{"дробные с запятой", []string{"1,5", "2,25"}, domain.ColumnTypeNumeric},
		{"булевы", []string{"true", "false", "да"}, domain.ColumnTypeBoolean},
		{"даты iso", []string{"2024-01-31", "2024-02-01"}, domain.ColumnTypeDate},
		{"даты с точками", []string{"31.01.2024", "01.02.2024"}, domain.ColumnTypeDate},
		{"метки времени", []string{"2024-01-31 12:00:00", "2024-02-01 13:30:00"}, domain.ColumnTypeTimestamp},
		{"rfc3339", []string{"2024-01-31T12:00:00Z"}, domain.ColumnTypeTimestamp},
		{"текст", []string{"abc", "1", "2024-01-31"}, domain.ColumnTypeText},
		{"код с ведущим нулём", []string{"007", "012"}, domain.ColumnTypeText},
		{"только пропуски", []string{"", "null", "-"}, domain.ColumnTypeText},
		{"смешанные числа и текст", []string{"10", "н/д", "20"}, domain.ColumnTypeInteger},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := InferType(tc.samples).Type; got != tc.want {
				t.Fatalf("получено %s, ожидалось %s", got, tc.want)
			}
		})
	}
}

func TestValueParserParse(t *testing.T) {
	p := InferType([]string{"1,5", "2,25"})
	v, err := p.Parse("3,75")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if f, ok := v.(float64); !ok || f != 3.75 {
		t.Fatalf("получено %v (%T), ожидалось 3.75", v, v)
	}

	if v, err := p.Parse("  "); err != nil || v != nil {
		t.Fatalf("пропуск должен разбираться в nil, получено %v, %v", v, err)
	}
	if _, err := p.Parse("не число"); err == nil {
		t.Fatal("ожидалась ошибка разбора")
	}
}

func TestValueParserDate(t *testing.T) {
	p := InferType([]string{"31.01.2024"})
	v, err := p.Parse("01.02.2024")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if v == nil {
		t.Fatal("ожидалось значение даты")
	}
}
