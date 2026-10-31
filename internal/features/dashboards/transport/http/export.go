package http

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/export"
	"github.com/heinousness/aibi/internal/core/logger"
	"github.com/heinousness/aibi/internal/core/transport/http/request"
	"github.com/heinousness/aibi/internal/core/transport/http/response"
)

const (
	formatXLSX = "xlsx"
	formatCSV  = "csv"

	mimeXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	mimeCSV  = "text/csv; charset=utf-8"

	minColumnWidth = 8
	maxColumnWidth = 60
)

func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = formatXLSX
	}
	if format != formatXLSX && format != formatCSV {
		response.Error(w, r, apperr.NewValidationError("format", "формат выгрузки: xlsx или csv"))
		return
	}
	var widgetID uuid.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("widget")); raw != "" {
		if widgetID, err = uuid.Parse(raw); err != nil {
			response.Error(w, r, apperr.NewValidationError("widget", "некорректный идентификатор виджета"))
			return
		}
	}
	period := domain.DateRange{
		From: strings.TrimSpace(r.URL.Query().Get("from")),
		To:   strings.TrimSpace(r.URL.Query().Get("to")),
	}

	dashboard, err := h.dashboards.Get(r.Context(), id, true, period)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	widgets := dashboard.Widgets
	name := dashboard.Title
	if widgetID != uuid.Nil {
		widgets = nil
		for _, wd := range dashboard.Widgets {
			if wd.ID == widgetID {
				widgets = []domain.Widget{wd}
				name = dashboard.Title + " — " + wd.Title
			}
		}
		if widgets == nil {
			response.Error(w, r, fmt.Errorf("виджет %s: %w", widgetID, apperr.ErrNotFound))
			return
		}
	}

	var buf bytes.Buffer
	contentType := mimeXLSX
	switch format {
	case formatXLSX:
		sheets := make([]export.Sheet, 0, len(widgets)+1)
		if widgetID == uuid.Nil {
			sheets = append(sheets, summarySheet(dashboard, time.Now()))
		}
		for _, wd := range widgets {
			sheets = append(sheets, widgetSheet(wd))
		}
		err = export.WriteXLSX(&buf, sheets)
	case formatCSV:
		contentType = mimeCSV
		err = export.WriteCSV(&buf, csvRows(widgets, widgetID == uuid.Nil))
	}
	if err != nil {
		logger.From(r.Context()).Error("выгрузка дашборда не собрана", "error", err, "dashboard", id)
		response.Error(w, r, err)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", contentDisposition(name, format))
	w.Header().Set("Content-Length", fmt.Sprint(buf.Len()))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = buf.WriteTo(w)
}

func summarySheet(d *domain.Dashboard, now time.Time) export.Sheet {
	bold := func(v any) export.Cell { return export.Cell{Value: v, Bold: true} }
	plain := func(v any) export.Cell { return export.Cell{Value: v} }

	rows := [][]export.Cell{
		{bold(d.Title)},
	}
	if d.Description != "" {
		rows = append(rows, []export.Cell{plain(d.Description)})
	}
	rows = append(rows, []export.Cell{plain("Выгружен"), plain(now.Format("02.01.2006 15:04"))})
	if d.Period != nil {
		rows = append(rows, []export.Cell{plain("Период"), plain(periodText(d.Period))})
	} else if d.DateRange != nil {
		rows = append(rows, []export.Cell{plain("Данные"), plain(periodText(d.DateRange))})
	}
	rows = append(rows, nil, []export.Cell{
		bold("Виджет"), bold("Показатель"), bold("Значение"), bold("Изменение, %"), bold("Доля, %"), bold("Строк"), bold("Примечание"),
	})
	for _, wd := range d.Widgets {
		row := []export.Cell{plain(wd.Title), {}, {}, {}, {}, {}, {}}
		if wd.Headline != nil {
			row[1] = plain(wd.Headline.Label)
			row[2] = plain(wd.Headline.Value)
			if wd.Headline.ChangePct != nil {
				row[3] = plain(*wd.Headline.ChangePct)
			}
			if wd.Headline.SharePct != nil {
				row[4] = plain(*wd.Headline.SharePct)
			}
		}
		if wd.Result != nil {
			row[5] = plain(len(wd.Result.Rows))
		}
		row[6] = plain(widgetNote(wd, d.Period != nil))
		rows = append(rows, row)
	}
	return export.Sheet{Name: "Сводка", Rows: rows, Widths: []float64{36, 28, 16, 14, 12, 10, 40}}
}

func widgetSheet(wd domain.Widget) export.Sheet {
	sheet := export.Sheet{Name: wd.Title}
	if wd.Error != "" {
		sheet.Rows = [][]export.Cell{{{Value: "Ошибка: " + wd.Error}}}
		sheet.Widths = []float64{80}
		return sheet
	}
	header, body := resultCells(wd.Result)
	sheet.Rows = append([][]export.Cell{header}, body...)
	if wd.Result != nil && wd.Result.Truncated {
		sheet.Rows = append(sheet.Rows, nil, []export.Cell{{Value: fmt.Sprintf("Показаны первые %d строк", len(wd.Result.Rows))}})
	}
	sheet.Widths = columnWidths(sheet.Rows)
	return sheet
}

func csvRows(widgets []domain.Widget, sections bool) [][]export.Cell {
	var rows [][]export.Cell
	for i, wd := range widgets {
		if sections {
			if i > 0 {
				rows = append(rows, nil)
			}
			rows = append(rows, []export.Cell{{Value: wd.Title}})
		}
		if wd.Error != "" {
			rows = append(rows, []export.Cell{{Value: "Ошибка: " + wd.Error}})
			continue
		}
		header, body := resultCells(wd.Result)
		rows = append(rows, header)
		rows = append(rows, body...)
	}
	return rows
}

func resultCells(rs *domain.ResultSet) (header []export.Cell, body [][]export.Cell) {
	if rs == nil {
		return nil, nil
	}
	header = make([]export.Cell, len(rs.Columns))
	for i, c := range rs.Columns {
		header[i] = export.Cell{Value: c.Name, Bold: true}
	}
	body = make([][]export.Cell, 0, len(rs.Rows))
	for _, row := range rs.Rows {
		cells := make([]export.Cell, len(row))
		for i, v := range row {
			if i < len(rs.Columns) && rs.Columns[i].Type == domain.ColumnTypeDate {
				if s, ok := v.(string); ok {
					if t, err := time.Parse(time.DateOnly, s); err == nil {
						v = t
					}
				}
			}
			cells[i] = export.Cell{Value: v}
		}
		body = append(body, cells)
	}
	return header, body
}

func columnWidths(rows [][]export.Cell) []float64 {
	var widths []float64
	for _, row := range rows {
		for i, cell := range row {
			for len(widths) <= i {
				widths = append(widths, minColumnWidth)
			}
			n := float64(utf8.RuneCountInString(fmt.Sprint(cell.Value))) + 2
			if _, ok := cell.Value.(time.Time); ok {
				n = 12
			}
			widths[i] = min(max(widths[i], n), maxColumnWidth)
		}
	}
	return widths
}

func widgetNote(wd domain.Widget, filtered bool) string {
	switch {
	case wd.Error != "":
		return "Ошибка: " + wd.Error
	case filtered && !wd.PeriodApplied:
		return "за весь период"
	case wd.Result != nil && wd.Result.Truncated:
		return "показана часть строк"
	}
	return ""
}

func periodText(p *domain.DateRange) string {
	day := func(s string) string {
		if t, err := time.Parse(time.DateOnly, s); err == nil {
			return t.Format("02.01.2006")
		}
		if s == "" {
			return "…"
		}
		return s
	}
	return day(p.From) + " — " + day(p.To)
}

func contentDisposition(name, ext string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		name = "dashboard"
	}
	filename := name + "." + ext
	return fmt.Sprintf(`attachment; filename="dashboard.%s"; filename*=UTF-8''%s`, ext, url.PathEscape(filename))
}
