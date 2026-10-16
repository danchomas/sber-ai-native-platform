package ingest

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

func TestSniffDialect(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want rune
	}{
		{"запятая", "a,b,c\n1,2,3", ','},
		{"точка с запятой", "a;b;c\n1;2;3", ';'},
		{"табуляция", "a\tb\tc\n1\t2\t3", '\t'},
		{"вертикальная черта", "a|b|c\n1|2|3", '|'},
		{"запятая внутри кавычек не считается", `a;"b,c";d`, ';'},
		{"одна колонка", "header\nvalue", ','},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SniffDialect([]byte(tc.in)).Delimiter; got != tc.want {
				t.Fatalf("получен %q, ожидался %q", got, tc.want)
			}
		})
	}
}

func TestSniffDialectDetectsWindows1251(t *testing.T) {
	encoded, _, err := transform.Bytes(charmap.Windows1251.NewEncoder(), []byte("Имя;Сумма\nИван;100"))
	if err != nil {
		t.Fatalf("подготовка данных: %v", err)
	}
	d := SniffDialect(encoded)
	if d.Encoding != "windows-1251" {
		t.Fatalf("кодировка определена как %s", d.Encoding)
	}
	if d.Delimiter != ';' {
		t.Fatalf("разделитель определён как %q", d.Delimiter)
	}

	rows, err := NewReader(bytes.NewReader(encoded), d).ReadAll()
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if rows[0][0] != "Имя" {
		t.Fatalf("заголовок прочитан как %q", rows[0][0])
	}
}

func TestNewReaderStripsBOM(t *testing.T) {
	in := "\xEF\xBB\xBFid,name\n1,test"
	rows, err := NewReader(strings.NewReader(in), Dialect{Delimiter: ',', Encoding: "utf-8"}).ReadAll()
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if rows[0][0] != "id" {
		t.Fatalf("BOM не удалён: %q", rows[0][0])
	}
}

func TestNewReaderHandlesRaggedRows(t *testing.T) {
	in := "a,b,c\n1,2\n1,2,3,4"
	rows, err := NewReader(strings.NewReader(in), Dialect{Delimiter: ',', Encoding: "utf-8"}).ReadAll()
	if err != nil {
		t.Fatalf("рваные строки должны читаться: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("прочитано %d строк", len(rows))
	}
}
