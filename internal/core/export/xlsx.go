package export

import (
	"archive/zip"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Cell struct {
	Value any
	Bold  bool
}

type Sheet struct {
	Name   string
	Rows   [][]Cell
	Widths []float64
}

const (
	styleDefault = 0
	styleBold    = 1
	styleDate    = 2

	maxSheetName = 31
	excelEpoch   = -2209161600
)

func WriteXLSX(w io.Writer, sheets []Sheet) error {
	if len(sheets) == 0 {
		sheets = []Sheet{{Name: "Лист1"}}
	}
	names := sheetNames(sheets)

	zw := zip.NewWriter(w)
	files := []struct {
		name string
		body func(io.Writer) error
	}{
		{"[Content_Types].xml", func(w io.Writer) error { return contentTypes(w, len(sheets)) }},
		{"_rels/.rels", staticPart(rootRels)},
		{"xl/workbook.xml", func(w io.Writer) error { return workbook(w, names) }},
		{"xl/_rels/workbook.xml.rels", func(w io.Writer) error { return workbookRels(w, len(sheets)) }},
		{"xl/styles.xml", staticPart(styles)},
	}
	for i := range sheets {
		sheet := sheets[i]
		files = append(files, struct {
			name string
			body func(io.Writer) error
		}{fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), func(w io.Writer) error { return worksheet(w, sheet) }})
	}

	for _, f := range files {
		part, err := zw.Create(f.name)
		if err != nil {
			return fmt.Errorf("xlsx: %s: %w", f.name, err)
		}
		if err := f.body(part); err != nil {
			return fmt.Errorf("xlsx: %s: %w", f.name, err)
		}
	}
	return zw.Close()
}

func staticPart(body string) func(io.Writer) error {
	return func(w io.Writer) error {
		_, err := io.WriteString(w, body)
		return err
	}
}

func contentTypes(w io.Writer, n int) error {
	var sb strings.Builder
	sb.WriteString(xmlHeader)
	sb.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	sb.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	sb.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	sb.WriteString(`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`)
	sb.WriteString(`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&sb, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i)
	}
	sb.WriteString(`</Types>`)
	_, err := io.WriteString(w, sb.String())
	return err
}

func workbook(w io.Writer, names []string) error {
	var sb strings.Builder
	sb.WriteString(xmlHeader)
	sb.WriteString(`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for i, name := range names {
		fmt.Fprintf(&sb, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, escape(name), i+1, i+1)
	}
	sb.WriteString(`</sheets></workbook>`)
	_, err := io.WriteString(w, sb.String())
	return err
}

func workbookRels(w io.Writer, n int) error {
	var sb strings.Builder
	sb.WriteString(xmlHeader)
	sb.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&sb, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i, i)
	}
	fmt.Fprintf(&sb, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, n+1)
	sb.WriteString(`</Relationships>`)
	_, err := io.WriteString(w, sb.String())
	return err
}

func worksheet(w io.Writer, sheet Sheet) error {
	var sb strings.Builder
	sb.WriteString(xmlHeader)
	sb.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	if len(sheet.Widths) > 0 {
		sb.WriteString(`<cols>`)
		for i, width := range sheet.Widths {
			fmt.Fprintf(&sb, `<col min="%d" max="%d" width="%.1f" customWidth="1"/>`, i+1, i+1, width)
		}
		sb.WriteString(`</cols>`)
	}
	sb.WriteString(`<sheetData>`)
	for r, row := range sheet.Rows {
		fmt.Fprintf(&sb, `<row r="%d">`, r+1)
		for c, cell := range row {
			writeCell(&sb, cellRef(c, r), cell)
		}
		sb.WriteString(`</row>`)
	}
	sb.WriteString(`</sheetData></worksheet>`)
	_, err := io.WriteString(w, sb.String())
	return err
}

func writeCell(sb *strings.Builder, ref string, cell Cell) {
	style := styleDefault
	if cell.Bold {
		style = styleBold
	}
	switch v := cell.Value.(type) {
	case nil:
		return
	case bool:
		b := "0"
		if v {
			b = "1"
		}
		fmt.Fprintf(sb, `<c r="%s" s="%d" t="b"><v>%s</v></c>`, ref, style, b)
	case int:
		fmt.Fprintf(sb, `<c r="%s" s="%d"><v>%d</v></c>`, ref, style, v)
	case int16:
		fmt.Fprintf(sb, `<c r="%s" s="%d"><v>%d</v></c>`, ref, style, v)
	case int32:
		fmt.Fprintf(sb, `<c r="%s" s="%d"><v>%d</v></c>`, ref, style, v)
	case int64:
		fmt.Fprintf(sb, `<c r="%s" s="%d"><v>%d</v></c>`, ref, style, v)
	case float32:
		writeFloat(sb, ref, style, float64(v))
	case float64:
		writeFloat(sb, ref, style, v)
	case time.Time:
		days := float64(v.Unix()-excelEpoch) / 86400
		fmt.Fprintf(sb, `<c r="%s" s="%d"><v>%s</v></c>`, ref, styleDate, strconv.FormatFloat(days, 'f', -1, 64))
	default:
		text := fmt.Sprint(v)
		if text == "" {
			return
		}
		fmt.Fprintf(sb, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, escape(text))
	}
}

func writeFloat(sb *strings.Builder, ref string, style int, f float64) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return
	}
	fmt.Fprintf(sb, `<c r="%s" s="%d"><v>%s</v></c>`, ref, style, strconv.FormatFloat(f, 'f', -1, 64))
}

func cellRef(col, row int) string {
	name := ""
	for col >= 0 {
		name = string(rune('A'+col%26)) + name
		col = col/26 - 1
	}
	return name + strconv.Itoa(row+1)
}

func sheetNames(sheets []Sheet) []string {
	used := make(map[string]bool, len(sheets))
	out := make([]string, len(sheets))
	for i, s := range sheets {
		base := strings.Map(func(r rune) rune {
			if strings.ContainsRune(`[]:*?/\`, r) || r < 0x20 {
				return ' '
			}
			return r
		}, strings.TrimSpace(s.Name))
		base = strings.Trim(base, "' ")
		if base == "" {
			base = fmt.Sprintf("Лист%d", i+1)
		}
		name := truncateRunes(base, maxSheetName)
		for n := 2; used[strings.ToLower(name)]; n++ {
			suffix := fmt.Sprintf(" (%d)", n)
			name = truncateRunes(base, maxSheetName-utf8.RuneCountInString(suffix)) + suffix
		}
		used[strings.ToLower(name)] = true
		out[i] = name
	}
	return out
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n]))
}

func escape(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			sb.WriteString("&amp;")
		case r == '<':
			sb.WriteString("&lt;")
		case r == '>':
			sb.WriteString("&gt;")
		case r == '"':
			sb.WriteString("&quot;")
		case r == '\t' || r == '\n' || r == '\r':
			sb.WriteRune(r)
		case r < 0x20 || r == 0xFFFE || r == 0xFFFF || r == utf8.RuneError:
			continue
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const rootRels = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
	`</Relationships>`

const styles = xmlHeader + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
	`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
	`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="3">` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="14" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`</cellXfs>` +
	`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
	`</styleSheet>`
