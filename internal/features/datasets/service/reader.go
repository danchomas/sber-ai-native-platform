package service

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

var delimiterCandidates = []rune{',', ';', '\t', '|'}

type Dialect struct {
	Delimiter rune
	Encoding  string
}

func (d Dialect) String() string {
	if d.Delimiter == '\t' {
		return "\\t"
	}
	return string(d.Delimiter)
}

func SniffDialect(head []byte) Dialect {
	d := Dialect{Delimiter: ',', Encoding: "utf-8"}

	head = trimPartialRune(head)
	if !utf8.Valid(head) {
		d.Encoding = "windows-1251"
		if decoded, err := io.ReadAll(transform.NewReader(
			bytes.NewReader(head), charmap.Windows1251.NewDecoder())); err == nil {
			head = decoded
		}
	}

	line := firstLine(stripBOM(head))
	best, bestCount := ',', -1
	for _, c := range delimiterCandidates {
		if n := countOutsideQuotes(line, c); n > bestCount {
			best, bestCount = c, n
		}
	}
	if bestCount > 0 {
		d.Delimiter = best
	}
	return d
}

func trimPartialRune(b []byte) []byte {
	if len(b) == 0 || utf8.Valid(b) {
		return b
	}
	for i := 1; i <= utf8.UTFMax && i <= len(b); i++ {
		if utf8.Valid(b[:len(b)-i]) {
			return b[:len(b)-i]
		}
	}
	return b
}

func NewReader(r io.Reader, d Dialect) *csv.Reader {
	var src io.Reader = r
	if d.Encoding == "windows-1251" {
		src = transform.NewReader(r, charmap.Windows1251.NewDecoder())
	}

	br := bufio.NewReaderSize(src, 1<<20)
	if bom, err := br.Peek(3); err == nil && bytes.Equal(bom, []byte{0xEF, 0xBB, 0xBF}) {
		_, _ = br.Discard(3)
	}

	cr := csv.NewReader(br)
	cr.Comma = d.Delimiter
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	cr.TrimLeadingSpace = true
	cr.ReuseRecord = false
	return cr
}

func stripBOM(b []byte) []byte {
	return bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
}

func firstLine(b []byte) string {
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

func countOutsideQuotes(line string, sep rune) int {
	inQuotes, n := false, 0
	for _, r := range line {
		switch {
		case r == '"':
			inQuotes = !inQuotes
		case r == sep && !inQuotes:
			n++
		}
	}
	return n
}
