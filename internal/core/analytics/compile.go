package analytics

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/heinousness/aibi/internal/core/domain"
)

type Catalog struct {
	Datasets      []domain.Dataset
	Relations     []domain.Relation
	DefaultSchema string
}

type Compiled struct {
	SQL      string
	Args     []any
	Datasets []string
	Warnings []string
}

type boundTable struct {
	alias string
	ds    domain.Dataset
}

type output struct {
	expr   string
	label  string
	column string
	isDim  bool
}

type compiler struct {
	cat       Catalog
	tables    []boundTable
	args      []any
	warns     []string
	subtotals bool
}

func Compile(q Query, cat Catalog, maxRows int) (*Compiled, error) {
	c := &compiler{cat: cat, subtotals: q.Subtotals && q.Time == nil && len(q.GroupBy) > 0}
	if err := c.bind(q.Dataset, q.Join); err != nil {
		return nil, err
	}

	if len(q.Metrics) > MaxMetrics {
		return nil, invalid("метрик не больше %d", MaxMetrics)
	}
	if len(q.GroupBy) > MaxGroupBy {
		return nil, invalid("разрезов не больше %d", MaxGroupBy)
	}

	var outs []output
	if q.Time != nil {
		out, err := c.timeOutput(*q.Time)
		if err != nil {
			return nil, err
		}
		outs = append(outs, out)
	}
	for _, ref := range q.GroupBy {
		bt, col, err := c.column(ref)
		if err != nil {
			return nil, err
		}
		outs = append(outs, output{
			expr: qualify(bt.alias, col.Name), label: title(col), column: col.Name, isDim: true,
		})
	}
	dims := len(outs)
	rawDims := make([]string, dims)
	for i := range outs {
		rawDims[i] = outs[i].expr
	}
	if c.subtotals {
		for i := range outs {
			name := SubtotalAll
			if i == 0 {
				name = SubtotalTotal
			}
			outs[i].expr = fmt.Sprintf("CASE WHEN GROUPING(%s) = 1 THEN '%s' ELSE %s::text END", rawDims[i], name, rawDims[i])
		}
	}

	metrics := q.Metrics
	if len(metrics) == 0 {
		if dims == 0 {
			return nil, invalid("укажите хотя бы одну метрику или разрез: metrics с колонкой или формулой expr, group_by или time")
		}
		metrics = []Metric{{Func: FuncCount}}
	}
	for _, m := range metrics {
		out, err := c.metricOutput(m, dims > 0)
		if err != nil {
			return nil, err
		}
		outs = append(outs, out)
	}
	dedupeLabels(outs)

	where, err := c.where(q.Filters)
	if err != nil {
		return nil, err
	}

	var sb strings.Builder
	sb.WriteString("SELECT ")
	for i, o := range outs {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s AS %s", o.expr, ident(o.label))
	}
	c.writeFrom(&sb)
	writeWhere(&sb, where)

	switch {
	case c.subtotals:
		sb.WriteString(" GROUP BY GROUPING SETS (")
		for i := dims; i >= 0; i-- {
			sb.WriteString("(" + strings.Join(rawDims[:i], ", ") + ")")
			if i > 0 {
				sb.WriteString(", ")
			}
		}
		sb.WriteString(")")
	case dims > 0:
		sb.WriteString(" GROUP BY ")
		for i := range dims {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(strconv.Itoa(i + 1))
		}
	}

	order, err := orderBy(q.Sort, outs, q.Time != nil)
	if err != nil {
		return nil, err
	}
	if c.subtotals {
		parts := make([]string, 0, 2*dims)
		for _, d := range rawDims {
			parts = append(parts, "GROUPING("+d+")", d)
		}
		order = strings.Join(parts, ", ")
	}
	if order != "" && dims > 0 {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(order)
	}
	if dims > 0 {
		fmt.Fprintf(&sb, " LIMIT %d", clampLimit(q.Limit, DefaultLimit, maxRows))
	}
	return c.result(sb.String()), nil
}

type RowsQuery struct {
	Dataset string   `json:"dataset"`
	Columns []string `json:"columns,omitempty"`
	Filters []Filter `json:"filters,omitempty"`
	Sort    *Sort    `json:"sort,omitempty"`
	Limit   int      `json:"limit,omitempty"`
}

const (
	MaxRowsColumns  = 12
	DefaultRowLimit = 10
	MaxRowLimit     = 50
)

func CompileRows(q RowsQuery, cat Catalog) (*Compiled, error) {
	c := &compiler{cat: cat}
	if err := c.bind(q.Dataset, nil); err != nil {
		return nil, err
	}
	base := c.tables[0]

	cols := q.Columns
	if len(cols) == 0 {
		for _, col := range base.ds.Columns {
			cols = append(cols, col.Name)
		}
	}
	if len(cols) > MaxRowsColumns {
		cols = cols[:MaxRowsColumns]
		c.warns = append(c.warns, fmt.Sprintf("Показаны первые %d колонок.", MaxRowsColumns))
	}

	outs := make([]output, 0, len(cols))
	for _, ref := range cols {
		bt, col, err := c.column(ref)
		if err != nil {
			return nil, err
		}
		outs = append(outs, output{expr: qualify(bt.alias, col.Name), label: title(col), column: col.Name})
	}
	dedupeLabels(outs)

	where, err := c.where(q.Filters)
	if err != nil {
		return nil, err
	}

	var sb strings.Builder
	sb.WriteString("SELECT ")
	for i, o := range outs {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s AS %s", o.expr, ident(o.label))
	}
	c.writeFrom(&sb)
	writeWhere(&sb, where)

	if q.Sort != nil {
		bt, col, err := c.column(q.Sort.By)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&sb, " ORDER BY %s%s", qualify(bt.alias, col.Name), direction(q.Sort.Desc))
	}
	fmt.Fprintf(&sb, " LIMIT %d", clampLimit(q.Limit, DefaultRowLimit, MaxRowLimit))
	return c.result(sb.String()), nil
}

func CompileDuplicates(dataset string, columns []string, cat Catalog) (summary, examples *Compiled, err error) {
	c := &compiler{cat: cat}
	if err := c.bind(dataset, nil); err != nil {
		return nil, nil, err
	}
	base := c.tables[0]

	if len(columns) == 0 {
		for _, col := range base.ds.Columns {
			columns = append(columns, col.Name)
		}
	}
	if len(columns) == 0 {
		return nil, nil, invalid("в датасете нет колонок")
	}

	exprs := make([]string, 0, len(columns))
	outs := make([]output, 0, len(columns)+1)
	for _, ref := range columns {
		bt, col, err := c.column(ref)
		if err != nil {
			return nil, nil, err
		}
		expr := qualify(bt.alias, col.Name)
		exprs = append(exprs, expr)
		outs = append(outs, output{expr: expr, label: title(col)})
	}
	outs = append(outs, output{expr: "count(*)", label: "Повторов"})
	dedupeLabels(outs)
	group := strings.Join(exprs, ", ")

	var from strings.Builder
	c.writeFrom(&from)

	summarySQL := "SELECT count(*) AS \"Групп повторов\", coalesce(sum(n), 0) AS \"Строк в повторах\", " +
		"coalesce(sum(n - 1), 0) AS \"Лишних строк\" FROM (SELECT count(*) AS n" + from.String() +
		" GROUP BY " + group + " HAVING count(*) > 1) AS dup"

	var sb strings.Builder
	sb.WriteString("SELECT ")
	for i, o := range outs {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s AS %s", o.expr, ident(o.label))
	}
	sb.WriteString(from.String())
	fmt.Fprintf(&sb, " GROUP BY %s HAVING count(*) > 1 ORDER BY count(*) DESC LIMIT 10", group)

	return c.result(summarySQL), c.result(sb.String()), nil
}

func (c *compiler) result(sql string) *Compiled {
	names := make([]string, 0, len(c.tables))
	for _, t := range c.tables {
		names = append(names, t.ds.Name)
	}
	return &Compiled{SQL: sql, Args: c.args, Datasets: names, Warnings: c.warns}
}

func (c *compiler) bind(dataset string, joins []string) error {
	base, err := c.dataset(dataset)
	if err != nil {
		return err
	}
	c.tables = []boundTable{{alias: "t0", ds: base}}

	if len(joins) > MaxJoins {
		return invalid("соединять можно не больше %d датасетов", MaxJoins)
	}
	for _, name := range joins {
		ds, err := c.dataset(name)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(c.tables, func(t boundTable) bool { return t.ds.ID == ds.ID }) {
			continue
		}
		if !base.Ref().SameSource(ds.Ref()) {
			return invalid("датасеты «%s» и «%s» лежат в разных источниках, соединить их нельзя", base.Name, ds.Name)
		}
		if _, _, ok := c.relation(ds); !ok {
			return invalid("между «%s» и уже выбранными датасетами нет выведенной связи", ds.Name)
		}
		c.tables = append(c.tables, boundTable{alias: "t" + strconv.Itoa(len(c.tables)), ds: ds})
	}
	if len(c.tables) > 1 {
		c.warns = append(c.warns,
			"Датасеты соединены по выведенной связи: если она «один ко многим», строки основного набора повторяются — для подсчёта сущностей берите число уникальных значений.")
	}
	return nil
}

func (c *compiler) relation(ds domain.Dataset) (string, boundTable, bool) {
	for _, bt := range c.tables {
		for _, r := range c.cat.Relations {
			var mine, theirs string
			switch {
			case r.LeftDatasetID == bt.ds.ID && r.RightDatasetID == ds.ID:
				mine, theirs = r.LeftColumn, r.RightColumn
			case r.RightDatasetID == bt.ds.ID && r.LeftDatasetID == ds.ID:
				mine, theirs = r.RightColumn, r.LeftColumn
			default:
				continue
			}
			if _, ok := bt.ds.Column(mine); !ok {
				continue
			}
			if _, ok := ds.Column(theirs); !ok {
				continue
			}
			alias := "t" + strconv.Itoa(len(c.tables))
			return qualify(bt.alias, mine) + " = " + qualify(alias, theirs), bt, true
		}
	}
	return "", boundTable{}, false
}

func (c *compiler) dataset(name string) (domain.Dataset, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Dataset{}, invalid("не указан датасет")
	}
	for _, d := range c.cat.Datasets {
		if d.Ready() && (d.Name == name || d.TableName == name) {
			return d, nil
		}
	}
	for _, d := range c.cat.Datasets {
		if d.Ready() && (strings.EqualFold(d.Name, name) || strings.EqualFold(d.TableName, name)) {
			return d, nil
		}
	}
	return domain.Dataset{}, invalid("датасета %q нет в каталоге", name)
}

func (c *compiler) column(ref string) (boundTable, domain.Column, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return boundTable{}, domain.Column{}, invalid("не указана колонка")
	}

	tables := c.tables
	name := ref
	if dot := strings.Index(ref, "."); dot > 0 {
		prefix := ref[:dot]
		for _, bt := range c.tables {
			if strings.EqualFold(bt.ds.Name, prefix) || strings.EqualFold(bt.ds.TableName, prefix) {
				tables, name = []boundTable{bt}, ref[dot+1:]
				break
			}
		}
	}

	matchers := []func(domain.Column) bool{
		func(col domain.Column) bool { return col.Name == name },
		func(col domain.Column) bool { return strings.EqualFold(col.Name, name) },
		func(col domain.Column) bool { return strings.EqualFold(col.SourceName, name) },
	}
	for _, match := range matchers {
		var (
			found   boundTable
			col     domain.Column
			matched int
		)
		for _, bt := range tables {
			for _, candidate := range bt.ds.Columns {
				if match(candidate) {
					if matched == 0 {
						found, col = bt, candidate
					}
					matched++
					break
				}
			}
		}
		if matched == 1 || (matched > 1 && found.alias == "t0") {
			return found, col, nil
		}
		if matched > 1 {
			return boundTable{}, domain.Column{}, invalid(
				"колонка %q есть в нескольких датасетах, укажите её как «датасет.колонка»", ref)
		}
	}
	return boundTable{}, domain.Column{}, invalid("колонки %q нет в датасете «%s»", ref, c.tables[0].ds.Name)
}

func (c *compiler) timeOutput(tg TimeGroup) (output, error) {
	bt, col, err := c.column(tg.Column)
	if err != nil {
		return output{}, err
	}
	if !col.Type.IsTemporal() {
		return output{}, invalid("колонка «%s» не содержит дат", title(col))
	}

	ref := qualify(bt.alias, col.Name)
	var expr, name string
	switch tg.Grain {
	case GrainDay, GrainWeek, GrainMonth, GrainQuarter, GrainYear:
		expr = fmt.Sprintf("date_trunc('%s', %s)::date", tg.Grain, ref)
		name = map[Grain]string{
			GrainDay: "День", GrainWeek: "Неделя", GrainMonth: "Месяц",
			GrainQuarter: "Квартал", GrainYear: "Год",
		}[tg.Grain]
	case GrainMonthOfYear:
		expr, name = fmt.Sprintf("date_part('month', %s)::int", ref), "Месяц года"
	case GrainQuarterOfYear:
		expr, name = fmt.Sprintf("date_part('quarter', %s)::int", ref), "Квартал года"
	case GrainDayOfWeek:
		expr, name = fmt.Sprintf("date_part('isodow', %s)::int", ref), "День недели"
	case GrainHour:
		if col.Type != domain.ColumnTypeTimestamp {
			return output{}, invalid("в колонке «%s» нет времени суток", title(col))
		}
		expr, name = fmt.Sprintf("date_part('hour', %s)::int", ref), "Час"
	default:
		return output{}, invalid("неизвестная гранулярность %q, допустимы: %s", tg.Grain, joinEnum(Grains))
	}
	return output{expr: expr, label: name, column: col.Name, isDim: true}, nil
}

func (c *compiler) metricOutput(m Metric, grouped bool) (output, error) {
	out, err := c.baseMetric(m, grouped)
	if err != nil || strings.TrimSpace(m.Per) == "" {
		return out, err
	}
	fn := Func(strings.ToLower(strings.TrimSpace(string(m.Func))))
	if fn != FuncSum && fn != FuncCount && fn != FuncCountDistinct {
		return output{}, invalid("per применяется только к sum, count и count_distinct")
	}
	bt, col, err := c.column(m.Per)
	if err != nil {
		return output{}, err
	}
	out.expr = "round((" + out.expr + ")::numeric / NULLIF(count(DISTINCT " + qualify(bt.alias, col.Name) + "), 0), 2)"
	out.column = ""
	return out, nil
}

func (c *compiler) baseMetric(m Metric, grouped bool) (output, error) {
	fn := Func(strings.ToLower(strings.TrimSpace(string(m.Func))))

	if m.Duration != nil {
		if m.Column != "" || m.Expr != "" || m.Per != "" || m.Scale != nil {
			return output{}, invalid("duration нельзя совмещать с column, expr, per или scale")
		}
		value, err := c.durationExpr(*m.Duration)
		if err != nil {
			return output{}, err
		}
		return c.valueMetric(m, fn, grouped, value)
	}
	if strings.TrimSpace(m.Expr) != "" {
		return c.exprMetric(m, fn, grouped)
	}
	if fn == FuncCount && m.Column == "" {
		return output{expr: "count(*)", label: orLabel(m.Label, "Количество")}, nil
	}
	if fn == FuncShare {
		if !grouped {
			return output{}, invalid("доля считается только в разрезе: добавьте group_by или time")
		}
		if m.Column != "" {
			bt, col, err := c.column(m.Column)
			if err != nil {
				return output{}, err
			}
			if col.Type.IsNumeric() {
				return output{expr: c.shareOf(qualify(bt.alias, col.Name) + "::numeric"), label: orLabel(m.Label, "Доля «"+title(col)+"», %")}, nil
			}
		}
		return output{
			expr:  "round(100.0 * count(*) / NULLIF(" + c.windowTotal("count(*)") + " OVER (), 0), 2)",
			label: orLabel(m.Label, "Доля, %"),
		}, nil
	}

	if m.Column == "" {
		return output{}, invalid("для метрики %s нужна колонка или формула expr", fn)
	}
	bt, col, err := c.column(m.Column)
	if err != nil {
		return output{}, err
	}
	ref := qualify(bt.alias, col.Name)
	name := title(col)

	switch fn {
	case FuncCount:
		return output{expr: "count(" + ref + ")", label: orLabel(m.Label, "Заполнено «"+name+"»"), column: col.Name}, nil
	case FuncCountDistinct:
		return output{expr: "count(DISTINCT " + ref + ")", label: orLabel(m.Label, "Уникальных «"+name+"»"), column: col.Name}, nil
	case FuncAvg:
		switch {
		case col.Type == domain.ColumnTypeBoolean:
			return output{expr: "round(100.0 * avg(" + ref + "::int), 2)", label: orLabel(m.Label, "Доля «"+name+"», %"), column: col.Name}, nil
		case col.Type.IsNumeric():
			return output{expr: "round(avg(" + ref + ")::numeric, 2)", label: orLabel(m.Label, "Среднее «"+name+"»"), column: col.Name}, nil
		}
	case FuncSum:
		if col.Type.IsNumeric() {
			return output{expr: "sum(" + ref + ")", label: orLabel(m.Label, "Сумма «"+name+"»"), column: col.Name}, nil
		}
	case FuncMedian, FuncP90:
		if col.Type.IsNumeric() {
			percentile, word := "0.5", "Медиана"
			if fn == FuncP90 {
				percentile, word = "0.9", "P90"
			}
			return output{
				expr:  "percentile_cont(" + percentile + ") WITHIN GROUP (ORDER BY " + ref + ")",
				label: orLabel(m.Label, word+" «"+name+"»"), column: col.Name,
			}, nil
		}
	case FuncMin, FuncMax:
		if col.Type != domain.ColumnTypeBoolean {
			word := map[Func]string{FuncMin: "Минимум", FuncMax: "Максимум"}[fn]
			return output{expr: string(fn) + "(" + ref + ")", label: orLabel(m.Label, word+" «"+name+"»"), column: col.Name}, nil
		}
	default:
		return output{}, invalid("неизвестная функция %q, допустимы: %s", m.Func, joinEnum(Funcs))
	}
	return output{}, invalid("функцию %s нельзя применить к колонке «%s» типа %s", fn, name, col.Type)
}

func (c *compiler) exprMetric(m Metric, fn Func, grouped bool) (output, error) {
	value, err := c.compileExpr(m.Expr, m.Scale)
	if err != nil {
		return output{}, err
	}
	return c.valueMetric(m, fn, grouped, value)
}

func (c *compiler) valueMetric(m Metric, fn Func, grouped bool, value string) (output, error) {
	name := orLabel(m.Label, "Показатель")
	switch fn {
	case FuncCount:
		if m.Duration != nil {
			return output{expr: "count(" + value + ")", label: orLabel(m.Label, "Корректных интервалов")}, nil
		}
	case FuncSum:
		return output{expr: "round(sum(" + value + "), 2)", label: name}, nil
	case FuncAvg:
		return output{expr: "round(avg(" + value + "), 2)", label: name}, nil
	case FuncMin, FuncMax:
		return output{expr: "round(" + string(fn) + "(" + value + "), 2)", label: name}, nil
	case FuncMedian, FuncP90:
		percentile := "0.5"
		if fn == FuncP90 {
			percentile = "0.9"
		}
		return output{expr: "percentile_cont(" + percentile + ") WITHIN GROUP (ORDER BY " + value + ")", label: name}, nil
	case FuncShare:
		if !grouped {
			return output{}, invalid("доля считается только в разрезе: добавьте group_by или time")
		}
		return output{expr: c.shareOf(value), label: orLabel(m.Label, "Доля, %")}, nil
	}
	return output{}, invalid("к формуле применимы sum, avg, min, max, median, p90, share и count для duration, а не %q", m.Func)
}

func (c *compiler) shareOf(value string) string {
	total := "sum(" + value + ")"
	return "round(100.0 * " + total + " / NULLIF(" + c.windowTotal(total) + " OVER (), 0), 2)"
}

func (c *compiler) windowTotal(aggregate string) string {
	if c.subtotals {
		return "max(" + aggregate + ")"
	}
	return "sum(" + aggregate + ")"
}

func (c *compiler) where(filters []Filter) ([]string, error) {
	if len(filters) > MaxFilters {
		return nil, invalid("фильтров не больше %d", MaxFilters)
	}
	out := make([]string, 0, len(filters))
	for _, f := range filters {
		cond, err := c.condition(f)
		if err != nil {
			return nil, err
		}
		out = append(out, cond)
	}
	return out, nil
}

func (c *compiler) condition(f Filter) (string, error) {
	bt, col, err := c.column(f.Column)
	if err != nil {
		return "", err
	}
	ref := qualify(bt.alias, col.Name)
	op := Op(strings.ToLower(strings.TrimSpace(string(f.Op))))

	switch op {
	case OpIsNull:
		return ref + " IS NULL", nil
	case OpNotNull:
		return ref + " IS NOT NULL", nil

	case OpEq, OpNe, OpGt, OpGte, OpLt, OpLte:
		if col.Type == domain.ColumnTypeBoolean && op != OpEq && op != OpNe {
			return "", invalid("логическую колонку «%s» можно сравнивать только на равенство", title(col))
		}
		v, err := coerce(f.Value, col)
		if err != nil {
			return "", err
		}
		sym := map[Op]string{OpEq: "=", OpNe: "<>", OpGt: ">", OpGte: ">=", OpLt: "<", OpLte: "<="}[op]
		return ref + " " + sym + " " + c.param(v), nil

	case OpIn, OpNotIn:
		values := f.Values
		if len(values) == 0 && f.Value != nil {
			values = []any{f.Value}
		}
		if len(values) == 0 {
			return "", invalid("для %s нужен список значений", op)
		}
		if len(values) > MaxInValues {
			return "", invalid("в списке значений не больше %d элементов", MaxInValues)
		}
		list, err := coerceList(values, col)
		if err != nil {
			return "", err
		}
		if op == OpIn {
			return ref + " = ANY(" + c.param(list) + ")", nil
		}
		return ref + " <> ALL(" + c.param(list) + ")", nil

	case OpBetween:
		if len(f.Values) != 2 {
			return "", invalid("для between нужны ровно два значения: начало и конец")
		}
		from, err := coerce(f.Values[0], col)
		if err != nil {
			return "", err
		}
		to, err := coerce(f.Values[1], col)
		if err != nil {
			return "", err
		}
		return ref + " BETWEEN " + c.param(from) + " AND " + c.param(to), nil

	case OpContains:
		if col.Type != domain.ColumnTypeText {
			return "", invalid("поиск подстроки возможен только в текстовой колонке, «%s» — %s", title(col), col.Type)
		}
		v, err := coerce(f.Value, col)
		if err != nil {
			return "", err
		}
		return ref + " ILIKE " + c.param("%"+escapeLike(v.(string))+"%") + " ESCAPE '!'", nil
	}
	return "", invalid("неизвестный оператор %q, допустимы: %s", f.Op, joinEnum(Ops))
}

func (c *compiler) param(v any) string {
	c.args = append(c.args, v)
	return "$" + strconv.Itoa(len(c.args))
}

func (c *compiler) writeFrom(sb *strings.Builder) {
	base := c.tables[0]
	fmt.Fprintf(sb, " FROM %s AS %s", c.table(base.ds), base.alias)

	joined := c.tables
	c.tables = c.tables[:1]
	for _, bt := range joined[1:] {
		on, _, _ := c.relation(bt.ds)
		fmt.Fprintf(sb, " LEFT JOIN %s AS %s ON %s", c.table(bt.ds), bt.alias, on)
		c.tables = append(c.tables, bt)
	}
}

func (c *compiler) table(d domain.Dataset) string {
	schema := d.Schema
	if schema == "" {
		schema = c.cat.DefaultSchema
	}
	return ident(schema) + "." + ident(d.TableName)
}

func writeWhere(sb *strings.Builder, conds []string) {
	if len(conds) == 0 {
		return
	}
	sb.WriteString(" WHERE ")
	sb.WriteString(strings.Join(conds, " AND "))
}

func orderBy(s *Sort, outs []output, hasTime bool) (string, error) {
	if s == nil || strings.TrimSpace(s.By) == "" {
		switch {
		case hasTime:
			return "1", nil
		case len(outs) > 0:
			for i, o := range outs {
				if !o.isDim {
					return strconv.Itoa(i+1) + " DESC NULLS LAST", nil
				}
			}
		}
		return "", nil
	}

	by := strings.TrimSpace(s.By)
	for i, o := range outs {
		if strings.EqualFold(o.label, by) || (o.column != "" && strings.EqualFold(o.column, by)) {
			return strconv.Itoa(i+1) + direction(s.Desc), nil
		}
	}
	return "", invalid("сортировать можно только по колонкам результата, %q среди них нет", by)
}

func direction(desc bool) string {
	if desc {
		return " DESC NULLS LAST"
	}
	return " ASC NULLS LAST"
}

func clampLimit(limit, def, maxRows int) int {
	if limit <= 0 {
		limit = def
	}
	if maxRows > 0 && limit > maxRows {
		limit = maxRows
	}
	return limit
}

func dedupeLabels(outs []output) {
	seen := make(map[string]int, len(outs))
	for i := range outs {
		outs[i].label = label(outs[i].label)
		key := strings.ToLower(outs[i].label)
		seen[key]++
		if n := seen[key]; n > 1 {
			outs[i].label = label(fmt.Sprintf("%s (%d)", outs[i].label, n))
		}
	}
}

func title(col domain.Column) string {
	if s := strings.TrimSpace(col.SourceName); s != "" {
		return s
	}
	return col.Name
}

func orLabel(custom, def string) string {
	if s := strings.TrimSpace(custom); s != "" {
		return s
	}
	return def
}

func qualify(alias, column string) string {
	return alias + "." + ident(column)
}

func ident(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func escapeLike(s string) string {
	r := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	return r.Replace(s)
}

func joinEnum[T ~string](values []T) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = string(v)
	}
	return strings.Join(parts, ", ")
}
