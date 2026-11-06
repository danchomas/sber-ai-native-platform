package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/promptguard"
)

const (
	maxTopValuesInContext = 6
	maxNameRunes          = 80
	maxCommentRunes       = 200
)

type dataFilter struct {
	hidden int
}

func (f *dataFilter) clean(s string, maxRunes int) string {
	out, hidden := promptguard.Data(s, maxRunes)
	if hidden && f != nil {
		f.hidden++
	}
	return out
}

func (f *dataFilter) securityNote() string {
	if f == nil || f.hidden == 0 {
		return ""
	}
	return fmt.Sprintf("В данных встретился текст, похожий на инструкцию для ИИ (%d %s). Он скрыт от модели и на ответ не повлиял.",
		f.hidden, pluralValues(f.hidden))
}

func pluralValues(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "значение"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
		return "значения"
	default:
		return "значений"
	}
}

var roleNames = map[domain.SemanticRole]string{
	domain.RoleIdentifier: "идентификатор",
	domain.RoleTemporal:   "дата/время",
	domain.RoleMeasure:    "метрика",
	domain.RoleDimension:  "разрез",
	domain.RoleFreeText:   "свободный текст",
	domain.RoleUnknown:    "тип не определён",
}

func buildSchemaContext(f *dataFilter, schema string, datasets []domain.Dataset, relations []domain.Relation) string {
	var sb strings.Builder

	sb.WriteString("Все таблицы доступны только для чтения.\n")
	if len(datasets) == 0 {
		sb.WriteString("Данные не загружены.\n")
		return sb.String()
	}

	var sources []string
	grouped := make(map[string][]domain.Dataset)
	for _, d := range datasets {
		if !d.Ready() {
			continue
		}
		if _, seen := grouped[d.SourceName]; !seen {
			sources = append(sources, d.SourceName)
		}
		grouped[d.SourceName] = append(grouped[d.SourceName], d)
	}

	for _, source := range sources {
		if source == "" {
			sb.WriteString("\nДАННЫЕ, ЗАГРУЖЕННЫЕ В ПЛАТФОРМУ:\n")
		} else {
			fmt.Fprintf(&sb, "\nИСТОЧНИК «%s» (подключённая база):\n", f.clean(source, maxNameRunes))
		}

		for _, d := range grouped[source] {
			tableSchema := d.Schema
			if tableSchema == "" {
				tableSchema = schema
			}
			fmt.Fprintf(&sb, "\nТАБЛИЦА %s.%s — датасет «%s», строк: %d, колонок: %d\n",
				tableSchema, d.TableName, f.clean(d.Name, maxNameRunes), d.RowCount, len(d.Columns))

			for _, c := range d.Columns {
				sb.WriteString(describeColumn(f, c))
			}
		}
	}

	if len(sources) > 1 {
		sb.WriteString("\nВАЖНО: таблицы разных источников лежат в разных базах. " +
			"Соединить их одним запросом невозможно — отвечай по одному источнику " +
			"или скажи, что для ответа нужны данные из разных баз.\n")
	}

	if len(relations) > 0 {
		sb.WriteString("\nСВЯЗИ МЕЖДУ ТАБЛИЦАМИ (выведены по пересечению значений, внешних ключей в данных нет):\n")
		for _, r := range relations {
			fmt.Fprintf(&sb, "- «%s».%s → «%s».%s: пересечение %.0f%%, тип %s\n",
				r.LeftDataset, r.LeftColumn, r.RightDataset, r.RightColumn, r.Overlap*100, r.Cardinality)
		}
	}
	return sb.String()
}

func buildToolContext(f *dataFilter, datasets []domain.Dataset, relations []domain.Relation) string {
	var sb strings.Builder
	sources := map[string]struct{}{}
	for _, d := range datasets {
		if !d.Ready() {
			continue
		}
		sources[d.SourceName] = struct{}{}
		origin := "загружен из файла " + f.clean(d.SourceFile, maxNameRunes)
		if d.Attached() {
			origin = "подключённая база «" + f.clean(d.SourceName, maxNameRunes) + "»"
		}
		fmt.Fprintf(&sb, "\nДАТАСЕТ «%s» — строк: %d, %s\n", f.clean(d.Name, maxNameRunes), d.RowCount, origin)
		for _, c := range d.Columns {
			sb.WriteString(describeColumn(f, c))
		}
	}
	if sb.Len() == 0 {
		return "Данные не загружены."
	}

	if len(sources) > 1 {
		sb.WriteString("\nДатасеты разных источников лежат в разных базах, соединить их нельзя.\n")
	}
	if len(relations) > 0 {
		sb.WriteString("\nСВЯЗИ (выведены по пересечению значений, их можно использовать в join):\n")
		for _, r := range relations {
			fmt.Fprintf(&sb, "- «%s».%s → «%s».%s: пересечение %.0f%%, тип %s\n",
				r.LeftDataset, r.LeftColumn, r.RightDataset, r.RightColumn, r.Overlap*100, r.Cardinality)
		}
	}
	return sb.String()
}

func describeColumn(f *dataFilter, c domain.Column) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "  - %s %s [%s]", f.clean(c.Name, maxNameRunes), c.Type, roleNames[c.Role])
	if c.SourceName != "" && c.SourceName != c.Name {
		fmt.Fprintf(&sb, " заголовок в файле: «%s»", f.clean(c.SourceName, maxNameRunes))
	}
	if c.Comment != "" {
		fmt.Fprintf(&sb, " комментарий: «%s»", f.clean(c.Comment, maxCommentRunes))
	}

	p := c.Profile
	if p == nil {
		sb.WriteString("\n")
		return sb.String()
	}

	fmt.Fprintf(&sb, "; различных значений: %d", p.DistinctCount)
	if share := p.NullShare(); share > 0.001 {
		fmt.Fprintf(&sb, "; пропусков: %.0f%%", share*100)
	}
	if p.Unique() {
		sb.WriteString("; уникальна")
	}

	switch {
	case c.Type.IsTemporal() && p.MinValue != nil && p.MaxValue != nil:
		fmt.Fprintf(&sb, "; период: %s … %s", *p.MinValue, *p.MaxValue)
	case c.Type.IsNumeric() && p.MinValue != nil && p.MaxValue != nil:
		fmt.Fprintf(&sb, "; от %s до %s", *p.MinValue, *p.MaxValue)
		if p.Mean != nil {
			fmt.Fprintf(&sb, ", среднее %.2f", *p.Mean)
		}
	case c.Role == domain.RoleDimension && len(p.TopValues) > 0:
		fmt.Fprintf(&sb, "; частые значения: %s", formatTopValues(f, p.TopValues, p.DistinctCount))
	}

	sb.WriteString("\n")
	return sb.String()
}

func formatTopValues(f *dataFilter, values []domain.ValueFrequency, distinct int64) string {
	if len(values) > maxTopValuesInContext {
		values = values[:maxTopValuesInContext]
	}
	more := ""
	if distinct > int64(len(values)) {
		more = fmt.Sprintf(" — список неполный, всего значений: %d", distinct)
	}
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, fmt.Sprintf("«%s» (%.0f%%)", f.clean(v.Value, 40), v.Share*100))
	}
	return strings.Join(parts, ", ") + more
}

const (
	historyResultTurns = 2
	historyResultRows  = 10
)

func formatHistory(f *dataFilter, turns []domain.Turn) []domain.ChatMessage {
	turns = slices.DeleteFunc(slices.Clone(turns), func(t domain.Turn) bool { return t.Blocked })
	out := make([]domain.ChatMessage, 0, len(turns)*2)
	for i, t := range turns {
		if t.Question != "" {
			out = append(out, domain.ChatMessage{Role: domain.RoleUser, Content: t.Question})
		}

		var sb strings.Builder
		if t.Answer != "" {
			sb.WriteString(t.Answer)
		}
		if len(t.ClarificationOptions) > 0 {
			sb.WriteString("\n[варианты уточнения:")
			for j, option := range t.ClarificationOptions {
				fmt.Fprintf(&sb, "\n%d) %s", j+1, f.clean(option, maxCommentRunes))
			}
			sb.WriteString("]")
		}
		switch {
		case len(t.Query) > 0:
			var compact bytes.Buffer
			if json.Compact(&compact, t.Query) == nil {
				fmt.Fprintf(&sb, "\n[параметры запроса: %s]", f.clean(compact.String(), maxArgsBytes))
			} else {
				fmt.Fprintf(&sb, "\n[параметры запроса: %s]", f.clean(string(t.Query), maxArgsBytes))
			}
		case t.SQL != "":
			fmt.Fprintf(&sb, "\n[выполненный SQL: %s]", compactSQL(t.SQL))
		}
		if t.Chart != nil && t.Chart.Type != "" {
			fmt.Fprintf(&sb, "\n[визуализация: %s]", t.Chart.Type)
		}
		if i >= len(turns)-historyResultTurns {
			sb.WriteString(historyRows(f, t.Result))
		}
		if sb.Len() > 0 {
			out = append(out, domain.ChatMessage{Role: domain.RoleAssistant, Content: sb.String()})
		}
	}
	return out
}

func historyRows(f *dataFilter, rs *domain.ResultSet) string {
	if rs == nil || len(rs.Columns) == 0 || len(rs.Rows) == 0 {
		return ""
	}
	var sb strings.Builder
	header := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		header[i] = f.clean(c.Name, maxNameRunes)
	}
	fmt.Fprintf(&sb, "\n[показанный результат: %s", strings.Join(header, " | "))
	for i, row := range rs.Rows {
		if i >= historyResultRows {
			fmt.Fprintf(&sb, "; … ещё %d", len(rs.Rows)-historyResultRows)
			break
		}
		cells := make([]string, len(row))
		for j, v := range row {
			cells[j] = f.clean(formatCell(v), 60)
		}
		fmt.Fprintf(&sb, "; %d) %s", i+1, strings.Join(cells, " | "))
	}
	sb.WriteString("]")
	return sb.String()
}

func compactSQL(sql string) string {
	s := strings.Join(strings.Fields(sql), " ")
	if len(s) > 600 {
		return s[:600] + "…"
	}
	return s
}

func formatResultForModel(f *dataFilter, rs *domain.ResultSet, maxRows int) string {
	if rs == nil || len(rs.Columns) == 0 {
		return "Результат пуст."
	}

	var sb strings.Builder
	header := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		header[i] = f.clean(c.Name, maxNameRunes)
	}
	fmt.Fprintf(&sb, "Колонки: %s\nСтрок получено: %d", strings.Join(header, " | "), rs.RowCount)
	if rs.Truncated {
		sb.WriteString(" (результат усечён)")
	}
	sb.WriteString("\n")

	for i, row := range rs.Rows {
		if i >= maxRows {
			fmt.Fprintf(&sb, "… ещё %d строк(и)\n", len(rs.Rows)-maxRows)
			break
		}
		cells := make([]string, len(row))
		for j, v := range row {
			cells[j] = f.clean(formatCell(v), 60)
		}
		sb.WriteString(strings.Join(cells, " | "))
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatCell(v any) string {
	if v == nil {
		return "—"
	}
	s := fmt.Sprint(v)
	if len([]rune(s)) > 60 {
		return string([]rune(s)[:60]) + "…"
	}
	return s
}

func datasetNames(datasets []domain.Dataset, tables []string) []string {
	byTable := make(map[string]string, len(datasets))
	for _, d := range datasets {
		byTable[strings.ToLower(d.Schema+"."+d.TableName)] = d.Name
	}
	out := make([]string, 0, len(tables))
	for _, t := range tables {
		if name, ok := byTable[strings.ToLower(t)]; ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
