package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"
)

func TestWriteXLSXProducesWellFormedParts(t *testing.T) {
	sheets := []Sheet{
		{Name: "Сводка", Rows: [][]Cell{{{Value: "Выручка <&>", Bold: true}, {Value: int64(42)}, {Value: 3.5}}}},
		{Name: "Сводка", Rows: [][]Cell{{{Value: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)}, {Value: true}, {Value: nil}, {Value: "ok\x00"}}}},
	}
	var buf bytes.Buffer
	if err := WriteXLSX(&buf, sheets); err != nil {
		t.Fatalf("WriteXLSX: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("не zip: %v", err)
	}
	parts := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(rc)
		rc.Close()
		parts[f.Name] = string(body)
		dec := xml.NewDecoder(bytes.NewReader(body))
		for {
			if _, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s: некорректный XML: %v", f.Name, err)
			}
		}
	}
	for _, name := range []string{"[Content_Types].xml", "xl/workbook.xml", "xl/styles.xml", "xl/worksheets/sheet1.xml", "xl/worksheets/sheet2.xml"} {
		if _, ok := parts[name]; !ok {
			t.Errorf("нет части %s", name)
		}
	}
	if !strings.Contains(parts["xl/workbook.xml"], `name="Сводка (2)"`) {
		t.Errorf("имена листов не сделаны уникальными: %s", parts["xl/workbook.xml"])
	}
	if !strings.Contains(parts["xl/worksheets/sheet2.xml"], `<v>45352</v>`) {
		t.Errorf("дата не записана числом Excel: %s", parts["xl/worksheets/sheet2.xml"])
	}
}

func TestSheetNames(t *testing.T) {
	got := sheetNames([]Sheet{
		{Name: "Выручка: по/месяцам?"},
		{Name: strings.Repeat("Я", 40)},
		{Name: strings.Repeat("Я", 40)},
		{Name: "  "},
	})
	want := []string{"Выручка  по месяцам", strings.Repeat("Я", 31), strings.Repeat("Я", 27) + " (2)", "Лист4"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("лист %d: %q, ожидалось %q", i, got[i], want[i])
		}
	}
}

func TestCellRef(t *testing.T) {
	for _, tc := range []struct {
		col, row int
		want     string
	}{{0, 0, "A1"}, {25, 9, "Z10"}, {26, 0, "AA1"}, {701, 0, "ZZ1"}, {702, 0, "AAA1"}} {
		if got := cellRef(tc.col, tc.row); got != tc.want {
			t.Errorf("cellRef(%d, %d) = %q, ожидалось %q", tc.col, tc.row, got, tc.want)
		}
	}
}

func TestWriteCSV(t *testing.T) {
	var buf bytes.Buffer
	rows := [][]Cell{
		{{Value: "Регион"}, {Value: "Выручка"}},
		{{Value: "=HYPERLINK(\"x\")"}, {Value: 1234.5}},
		{{Value: "Север; Юг"}, {Value: int64(-7)}},
	}
	if err := WriteCSV(&buf, rows); err != nil {
		t.Fatal(err)
	}
	want := "\xEF\xBB\xBFРегион;Выручка\r\n\"'=HYPERLINK(\"\"x\"\")\";1234,5\r\n\"Север; Юг\";-7\r\n"
	if buf.String() != want {
		t.Fatalf("получено %q,\nожидалось %q", buf.String(), want)
	}
}
