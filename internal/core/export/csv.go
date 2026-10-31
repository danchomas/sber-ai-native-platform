package export

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

const utf8BOM = "\xEF\xBB\xBF"

func WriteCSV(w io.Writer, rows [][]Cell) error {
	if _, err := io.WriteString(w, utf8BOM); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	cw.UseCRLF = true
	for _, row := range rows {
		record := make([]string, len(row))
		for i, cell := range row {
			record[i] = csvValue(cell.Value)
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func csvValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case bool:
		if t {
			return "да"
		}
		return "нет"
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return ""
		}
		return strings.Replace(strconv.FormatFloat(t, 'f', -1, 64), ".", ",", 1)
	case float32:
		return csvValue(float64(t))
	case int, int16, int32, int64:
		return fmt.Sprint(t)
	case time.Time:
		return t.Format("2006-01-02")
	default:
		return neutralizeFormula(fmt.Sprint(t))
	}
}

func neutralizeFormula(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
