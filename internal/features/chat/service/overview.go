package service

import (
	"fmt"
	"strings"

	"github.com/heinousness/aibi/internal/core/domain"
)

func buildOverviewFacts(f *dataFilter, schema string, datasets []domain.Dataset, relations []domain.Relation) string {
	var sb strings.Builder

	var totalRows int64
	ready := 0
	for _, d := range datasets {
		if d.Ready() {
			ready++
			totalRows += d.RowCount
		}
	}
	fmt.Fprintf(&sb, "Загружено датасетов: %d, суммарно строк: %s.\n", ready, formatInt(totalRows))
	sb.WriteString(buildSchemaContext(f, schema, datasets, relations))
	return sb.String()
}

func fallbackOverviewText(datasets []domain.Dataset, relations []domain.Relation) string {
	ready := make([]domain.Dataset, 0, len(datasets))
	for _, d := range datasets {
		if d.Ready() {
			ready = append(ready, d)
		}
	}
	if len(ready) == 0 {
		return "Данные пока не загружены. Загрузите CSV-файл, и я расскажу, что в нём есть."
	}

	var (
		sb        strings.Builder
		totalRows int64
	)
	for _, d := range ready {
		totalRows += d.RowCount
	}

	fmt.Fprintf(&sb, "Передо мной %s: суммарно %s строк.\n",
		pluralDatasets(len(ready)), formatInt(totalRows))

	for _, d := range ready {
		fmt.Fprintf(&sb, "\n«%s» — %s строк, %d колонок.", d.Name, formatInt(d.RowCount), len(d.Columns))

		if ids := columnLabels(d, domain.RoleIdentifier); len(ids) > 0 {
			fmt.Fprintf(&sb, " Идентификаторы: %s.", strings.Join(ids, ", "))
		}
		if temporal := d.ColumnsByRole(domain.RoleTemporal); len(temporal) > 0 {
			for _, c := range temporal {
				if c.Profile != nil && c.Profile.MinValue != nil && c.Profile.MaxValue != nil {
					fmt.Fprintf(&sb, " Период по полю «%s»: с %s по %s.",
						label(c), *c.Profile.MinValue, *c.Profile.MaxValue)
					break
				}
			}
		}
		if dims := columnLabels(d, domain.RoleDimension); len(dims) > 0 {
			fmt.Fprintf(&sb, " Разрезы для группировок: %s.", strings.Join(limitList(dims, 6), ", "))
		}
		if measures := columnLabels(d, domain.RoleMeasure); len(measures) > 0 {
			fmt.Fprintf(&sb, " Числовые показатели: %s.", strings.Join(limitList(measures, 6), ", "))
		}
		if gaps := gapsNote(d); gaps != "" {
			sb.WriteString(" " + gaps)
		}
	}

	if len(relations) > 0 {
		sb.WriteString("\n\nТаблицы связываются по совпадающим идентификаторам: ")
		parts := make([]string, 0, len(relations))
		for _, r := range limitRelations(relations, 5) {
			parts = append(parts, fmt.Sprintf("«%s».%s ↔ «%s».%s (совпадает %.0f%% значений)",
				r.LeftDataset, r.LeftColumn, r.RightDataset, r.RightColumn, r.Overlap*100))
		}
		sb.WriteString(strings.Join(parts, "; "))
		sb.WriteString(".")
	}

	sb.WriteString("\n\nМожно спросить, например: «какие значения чаще всего встречаются в этих данных», " +
		"«покажи динамику по месяцам», «собери дашборд с ключевыми показателями».")
	return sb.String()
}

func overviewResultSet(datasets []domain.Dataset) *domain.ResultSet {
	rs := &domain.ResultSet{
		Columns: []domain.ResultColumn{
			{Name: "Датасет", Type: domain.ColumnTypeText},
			{Name: "Строк", Type: domain.ColumnTypeInteger},
			{Name: "Колонок", Type: domain.ColumnTypeInteger},
			{Name: "Период", Type: domain.ColumnTypeText},
			{Name: "Источник", Type: domain.ColumnTypeText},
		},
	}
	for _, d := range datasets {
		if !d.Ready() {
			continue
		}
		rs.Rows = append(rs.Rows, []any{d.Name, d.RowCount, len(d.Columns), datasetPeriod(d), d.SourceFile})
	}
	rs.RowCount = len(rs.Rows)
	return rs
}

func datasetPeriod(d domain.Dataset) string {
	for _, c := range d.ColumnsByRole(domain.RoleTemporal) {
		if c.Profile != nil && c.Profile.MinValue != nil && c.Profile.MaxValue != nil {
			return fmt.Sprintf("%s … %s", *c.Profile.MinValue, *c.Profile.MaxValue)
		}
	}
	return "—"
}

func columnLabels(d domain.Dataset, role domain.SemanticRole) []string {
	cols := d.ColumnsByRole(role)
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, label(c))
	}
	return out
}

func label(c domain.Column) string {
	if c.SourceName != "" {
		return c.SourceName
	}
	return c.Name
}

func gapsNote(d domain.Dataset) string {
	var noisy []string
	for _, c := range d.Columns {
		if c.Profile != nil && c.Profile.NullShare() > 0.3 {
			noisy = append(noisy, fmt.Sprintf("%s (%.0f%% пропусков)", label(c), c.Profile.NullShare()*100))
		}
	}
	if len(noisy) == 0 {
		return ""
	}
	return "Заполнены не полностью: " + strings.Join(limitList(noisy, 4), ", ") + "."
}

func limitList(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	out := append([]string{}, items[:n]...)
	return append(out, fmt.Sprintf("и ещё %d", len(items)-n))
}

func limitRelations(items []domain.Relation, n int) []domain.Relation {
	if len(items) <= n {
		return items
	}
	return items[:n]
}

func pluralDatasets(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return fmt.Sprintf("%d датасет", n)
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
		return fmt.Sprintf("%d датасета", n)
	default:
		return fmt.Sprintf("%d датасетов", n)
	}
}

func formatInt(n int64) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		s = s[1:]
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	out := strings.Join(parts, " ")
	if n < 0 {
		return "-" + out
	}
	return out
}
