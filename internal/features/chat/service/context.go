package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/heinousness/aibi/internal/core/domain"
)

const maxTopValuesInContext = 6

var roleNames = map[domain.SemanticRole]string{
	domain.RoleIdentifier: "идентификатор",
	domain.RoleTemporal:   "дата/время",
	domain.RoleMeasure:    "метрика",
	domain.RoleDimension:  "разрез",
	domain.RoleFreeText:   "свободный текст",
	domain.RoleUnknown:    "тип не определён",
}

func buildSchemaContext(schema string, datasets []domain.Dataset, relations []domain.Relation) string {
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
			fmt.Fprintf(&sb, "\nИСТОЧНИК «%s» (подключённая база):\n", source)
		}

		for _, d := range grouped[source] {
			tableSchema := d.Schema
			if tableSchema == "" {
				tableSchema = schema
			}
			fmt.Fprintf(&sb, "\nТАБЛИЦА %s.%s — датасет «%s», строк: %d, колонок: %d\n",
				tableSchema, d.TableName, d.Name, d.RowCount, len(d.Columns))

			for _, c := range d.Columns {
				sb.WriteString(describeColumn(c))
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

func describeColumn(c domain.Column) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "  - %s %s [%s]", c.Name, c.Type, roleNames[c.Role])
	if c.SourceName != "" && c.SourceName != c.Name {
		fmt.Fprintf(&sb, " заголовок в файле: «%s»", c.SourceName)
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
		fmt.Fprintf(&sb, "; частые значения: %s", formatTopValues(p.TopValues))
	}

	sb.WriteString("\n")
	return sb.String()
}

func formatTopValues(values []domain.ValueFrequency) string {
	if len(values) > maxTopValuesInContext {
		values = values[:maxTopValuesInContext]
	}
	parts := make([]string, 0, len(values))
	for _, v := range values {
		value := v.Value
		if len([]rune(value)) > 40 {
			value = string([]rune(value)[:40]) + "…"
		}
		parts = append(parts, fmt.Sprintf("«%s» (%.0f%%)", value, v.Share*100))
	}
	return strings.Join(parts, ", ")
}

func formatHistory(turns []domain.Turn) []domain.ChatMessage {
	out := make([]domain.ChatMessage, 0, len(turns)*2)
	for _, t := range turns {
		if t.Question != "" {
			out = append(out, domain.ChatMessage{Role: domain.RoleUser, Content: t.Question})
		}

		var sb strings.Builder
		if t.Answer != "" {
			sb.WriteString(t.Answer)
		}
		if t.SQL != "" {
			fmt.Fprintf(&sb, "\n[выполненный SQL: %s]", compactSQL(t.SQL))
		}
		if t.Chart != nil && t.Chart.Type != "" {
			fmt.Fprintf(&sb, "\n[визуализация: %s]", t.Chart.Type)
		}
		if sb.Len() > 0 {
			out = append(out, domain.ChatMessage{Role: domain.RoleAssistant, Content: sb.String()})
		}
	}
	return out
}

func compactSQL(sql string) string {
	s := strings.Join(strings.Fields(sql), " ")
	if len(s) > 600 {
		return s[:600] + "…"
	}
	return s
}

func formatResultForModel(rs *domain.ResultSet, maxRows int) string {
	if rs == nil || len(rs.Columns) == 0 {
		return "Результат пуст."
	}

	var sb strings.Builder
	header := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		header[i] = c.Name
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
			cells[j] = formatCell(v)
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
