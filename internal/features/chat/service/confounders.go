package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/domain"
)

const (
	toolConfounder     = "check_confounder"
	analysisConfounder = "confounder"

	// maxConfounderDims — сколько признаков показать подробно: больше двух
	// в одной таблице модель уже не пересказывает.
	maxConfounderDims = 2
	// confounderMinRows — меньше записей в группе внутри значения признака —
	// значение не участвует в оценке, его разница — шум.
	confounderMinRows = 20
	// Пороги вывода: какую часть общей разницы сохраняет разница внутри групп.
	keptShare      = 0.7
	partShare      = 0.3
	minGapPct      = 2.0
	minComposition = 0.05
)

func confounderTools() []struct {
	name, desc string
	params     schema
} {
	return []struct {
		name, desc string
		params     schema
	}{{
		toolConfounder,
		"Проверить, не объясняется ли разница между двумя группами записей третьим признаком: считает показатель групп А и Б " +
			"в целом и внутри каждого значения признака, оценивает разницу с поправкой на состав и возвращает готовый вывод " +
			"(сохраняется, частично объясняется, объясняется, разницы нет). Для вопросов «не объясняется ли это X», " +
			"«или это эффект разных категорий», «правда ли, что A влияет на B». Без by сам выбирает признаки, " +
			"по которым состав групп различается сильнее всего и от которых зависит показатель.",
		object(schema{
			"dataset": text("имя датасета"),
			"metric":  metricSchema("обычно avg длительности или количества; частота события — share с filters условия события"),
			"filters": filtersSchema(),
			"group_a": conditionsSchema("условия группы А, например сумма заявки gte 500000 — крупные заявки"),
			"group_b": conditionsSchema("условия группы Б — противоположной: сумма заявки lt 500000"),
			"label_a": text("подпись группы А, например «Крупные заявки»"),
			"label_b": text("подпись группы Б"),
			"by": list(schema{"type": "string"},
				"признаки, внутри которых сравнивать группы, например категория закупки; пусто — подобрать самому"),
			"title": text("заголовок результата"),
		}, "dataset", "metric", "group_a", "group_b"),
	}}
}

type confounderArgs struct {
	Dataset string             `json:"dataset"`
	Metric  *analytics.Metric  `json:"metric"`
	Filters []analytics.Filter `json:"filters"`
	GroupA  []analytics.Filter `json:"group_a"`
	GroupB  []analytics.Filter `json:"group_b"`
	LabelA  string             `json:"label_a"`
	LabelB  string             `json:"label_b"`
	By      []string           `json:"by"`
	Title   string             `json:"title"`
}

// stratum — один признак: показатель групп внутри каждого его значения.
type stratum struct {
	dim    driverDim
	values []driverStats
	// composition — насколько различается состав групп по признаку: половина
	// суммы расхождений долей значений, от 0 (одинаковый) до 1.
	composition float64
	// shift — значение с наибольшим расхождением долей и сами доли.
	shift          string
	shareA, shareB float64
	// spread — насколько показатель зависит от признака сам по себе.
	spread float64
	// finer — вложенные в признак более мелкие с тем же выводом, которые
	// он заменил в списке: «Узел выполнения» внутри «Окружения».
	finer    []string
	adjusted float64
	// adjustedDiff — та же поправка на состав, но разница в единицах показателя (А − Б).
	adjustedDiff float64
	adjOK        bool
	same         []string
	none         []string
	opposite     []string
}

func (s stratum) score() float64 { return s.composition * s.spread }

func (r *agentRun) checkConfounder(ctx context.Context, raw []byte) (string, int, error) {
	var args confounderArgs
	if err := decodeArgs(normalizeArgs(raw, "filters", "group_a", "group_b", "by"), &args); err != nil {
		return "", 0, err
	}
	if args.Metric == nil || args.Metric.Func == "" {
		return "", 0, errors.New("укажи metric: что сравнивать, например avg длительности или share с условием события")
	}
	if len(args.GroupA) == 0 || len(args.GroupB) == 0 {
		return "", 0, errors.New("нужны обе группы: group_a и group_b с противоположным условием")
	}
	if len(args.Metric.Filters)+max(len(args.GroupA), len(args.GroupB)) > maxDriverFilters {
		return "", 0, fmt.Errorf("условий метрики и группы вместе не больше %d", maxDriverFilters)
	}
	dataset, ok := r.datasetByName(args.Dataset)
	if !ok {
		return "", 0, fmt.Errorf("датасета %q нет в каталоге", args.Dataset)
	}
	dims, err := confounderDims(dataset, args)
	if err != nil {
		return "", 0, err
	}

	dargs := driversArgs{GroupA: args.GroupA, GroupB: args.GroupB}
	rate := isRate(*args.Metric)
	base := analytics.Query{Dataset: dataset.Name, Filters: args.Filters,
		Metrics: driverMetrics(*args.Metric, dargs), Limit: maxDriverValues + 1}
	overallRows, sql, tables, err := r.driverQuery(ctx, base)
	if err != nil {
		return "", 0, err
	}
	if len(overallRows.Rows) != 1 {
		return "", 0, errors.New("не удалось посчитать показатель по всем записям: проверь metric и filters")
	}
	overall := driverRow(overallRows.Rows[0], 0, true, rate)
	if !overall.defined {
		return "", 0, errors.New("показатель не определён: в одной из групп нет записей или значения пустые, проверь условия групп")
	}

	var strata []stratum
	var proxies []string
	for _, d := range dims {
		rs, _, _, err := r.driverQuery(ctx, d.query(base))
		if err != nil {
			r.s.log.DebugContext(ctx, "признак пропущен", "dimension", d.name, "error", err)
			continue
		}
		st, ok := buildStratum(d, rs, overall, rate)
		if rate && outcomeProxy(st.values) {
			proxies = append(proxies, d.name)
			continue
		}
		if ok {
			strata = append(strata, st)
		}
	}
	if len(strata) == 0 {
		if len(proxies) > 0 {
			return "", 0, proxyError(r.filter, proxies)
		}
		return "", 0, errors.New("ни один признак не дал групп достаточного размера: укажи by с категорией, у которой меньше 40 значений")
	}
	if len(args.By) == 0 {
		slices.SortStableFunc(strata, func(x, y stratum) int { return cmp.Compare(y.score(), x.score()) })
		strata = r.preferCoarse(ctx, base, strata, overall)
	}
	checked := strata[:min(len(strata), maxConfounderDims)]

	labelA := orDefault(args.LabelA, "Группа А")
	labelB := orDefault(args.LabelB, "Группа Б")
	metricLabel := orDefault(args.Metric.Label, "Показатель")
	out := confounderTable(checked, overall, labelA, labelB, metricLabel)
	res := r.register(orDefault(args.Title, "Сохраняется ли разница внутри групп"), sql, tables, out, nil,
		analysisConfounder, &domain.ChartSpec{Type: domain.ChartTypeTable})

	payload := r.resultOutput(res)
	payload["summary"] = confounderSummary(r.filter, checked, strata, overall, labelA, labelB, len(args.By) == 0)
	if len(proxies) > 0 {
		payload["excluded"] = proxyNote(r.filter, proxies)
	}
	payload["how_to_read"] = "conclusion — готовый вывод расчёта: начни ответ с него (да, нет или частично) и подтверди числами " +
		"из overall и одной-двух групп из checked. Разница внутри групп с поправкой на состав (adjusted_gap_pct) отвечает, " +
		"объясняется ли разница признаком; общая разница (overall.gap_pct) — нет. Это связь в данных, а не доказанная причина: " +
		"если вопрос о том, что A влияет на B, назови и обратное объяснение (B влияет на A), если оно возможно. " + differenceHint
	return encodeOutput(payload), len(out.Rows), nil
}

// differenceHint объясняет модели две меры разницы. Без него она называла
// относительную разницу ROMI (254% от показателя Б) «254 процентными пунктами».
const differenceHint = "gap_pct — разница в процентах от показателя группы Б, difference — в единицах самого показателя (А − Б). " +
	"Если показатель сам в процентах (доля, частота, ROMI, маржа) или у группы Б он отрицательный или близок к нулю, " +
	"называй разницу по difference в процентных пунктах (п.п.), а не по gap_pct."

// preferCoarse заменяет в начале списка признак, вложенный в более крупный
// (каждый узел выполнения относится к одному окружению), этим крупным, если
// вывод по нему тот же. По оценке мелкий признак всегда не хуже крупного,
// в который вложен: состав групп по нему различается не меньше, и показатель
// зависит от него не слабее. Но «объясняется окружением» человеку понятнее,
// чем «объясняется узлом». Вложенность проверяет один запрос на признак:
// число разных значений крупного внутри каждого значения мелкого.
func (r *agentRun) preferCoarse(ctx context.Context, base analytics.Query, strata []stratum, overall driverStats) []stratum {
	for i := 0; i < min(len(strata), maxConfounderDims); i++ {
		fine := strata[i]
		code, _ := fine.verdict(overall)
		var coarse []int
		for j, s := range strata {
			if j == i || len(s.values) >= len(fine.values) {
				continue
			}
			if c, _ := s.verdict(overall); c == code {
				coarse = append(coarse, j)
			}
		}
		if len(coarse) == 0 {
			continue
		}
		q := base
		q.GroupBy = []string{fine.dim.column}
		q.Metrics = make([]analytics.Metric, 0, len(coarse))
		for _, j := range coarse {
			q.Metrics = append(q.Metrics, analytics.Metric{Func: analytics.FuncCountDistinct, Column: strata[j].dim.column})
		}
		rs, _, _, err := r.driverQuery(ctx, q)
		if err != nil {
			r.s.log.DebugContext(ctx, "вложенность признака не проверена", "dimension", fine.dim.name, "error", err)
			continue
		}
		k, ok := nestedIn(rs, len(coarse))
		if !ok {
			continue
		}
		j := coarse[k]
		strata[j].finer = slices.Concat(strata[j].finer, fine.finer, []string{fine.dim.name})
		if j < i {
			// Крупный уже выше в списке: мелкий просто уходит.
			strata = slices.Delete(strata, i, i+1)
		} else {
			parent := strata[j]
			strata = slices.Delete(strata, j, j+1)
			strata[i] = parent
		}
		// На место i встал крупный признак или следующий за мелким:
		// крупный сам может быть вложен в ещё более крупный.
		i--
	}
	return strata
}

// nestedIn — номер первой из n колонок количеств (после колонки значения),
// у которой в каждой строке не больше одного значения: признак строки
// целиком вложен в признак этой колонки.
func nestedIn(rs *domain.ResultSet, n int) (int, bool) {
	if len(rs.Rows) == 0 {
		return 0, false
	}
	for k := range n {
		nested := true
		for _, row := range rs.Rows {
			if len(row) <= k+1 {
				return 0, false
			}
			if v, ok := numberOf(row[k+1]); !ok || v > 1 {
				nested = false
				break
			}
		}
		if nested {
			return k, true
		}
	}
	return 0, false
}

// confounderDims — признаки-кандидаты: заданные в by или все категории
// датасета, кроме колонок условий групп и события — они сами задают сравнение.
func confounderDims(d domain.Dataset, args confounderArgs) ([]driverDim, error) {
	if len(args.By) > 0 {
		var dims []driverDim
		for _, name := range args.By {
			col, ok := findColumn(d, name)
			if !ok {
				return nil, missingColumn(d, name)
			}
			if col.Type.IsTemporal() {
				return nil, fmt.Errorf("«%s» — дата: внутри дат группы не сравниваются, укажи категорию", label(col))
			}
			dims = append(dims, driverDim{name: label(col), column: col.Name})
		}
		return dims, nil
	}

	used := conditionColumns(d, args.GroupA, args.GroupB, args.Metric.Filters, args.Filters)
	var dims []driverDim
	for _, col := range d.Columns {
		if len(dims) >= maxDriverColumns {
			break
		}
		if used[col.Name] || col.Role != domain.RoleDimension || col.Profile == nil || col.Type.IsTemporal() {
			continue
		}
		if n := col.Profile.DistinctCount; n < 2 || n > maxDriverValues {
			continue
		}
		dims = append(dims, driverDim{name: label(col), column: col.Name})
	}
	if len(dims) == 0 {
		return nil, errors.New("в датасете нет подходящих категорий: укажи признак в by")
	}
	return dims, nil
}

// buildStratum оценивает один признак: состав групп по его значениям,
// зависимость показателя от признака и разницу групп внутри значений.
// Разница с поправкой на состав — среднее разниц внутри значений с весами
// nA·nB/(nA+nB): так весят значения, где обе группы достаточно велики.
func buildStratum(d driverDim, rs *domain.ResultSet, overall driverStats, rate bool) (stratum, bool) {
	st := stratum{dim: d}
	var totalA, totalB float64
	for _, row := range rs.Rows {
		v := driverRow(row, 1, true, rate)
		v.valueName = d.valueName(row[0])
		if row[0] == nil {
			v.valueName = "(пусто)"
		}
		totalA += v.n
		totalB += v.nB
		st.values = append(st.values, v)
	}
	if totalA == 0 || totalB == 0 || len(st.values) < 2 {
		return st, false
	}

	var pooledSum, pooledWeight float64
	for _, v := range st.values {
		pa, pb := v.n/totalA, v.nB/totalB
		st.composition += math.Abs(pa-pb) / 2
		if math.Abs(pa-pb) > math.Abs(st.shareA-st.shareB) {
			st.shift, st.shareA, st.shareB = v.valueName, pa, pb
		}
		if n := v.n + v.nB; n > 0 {
			pooledSum += (v.a*v.n + v.b*v.nB)
			pooledWeight += n
		}
	}
	if pooledWeight > 0 {
		mean := pooledSum / pooledWeight
		var dev float64
		for _, v := range st.values {
			if n := v.n + v.nB; n > 0 {
				pooled := (v.a*v.n + v.b*v.nB) / n
				dev += n * (pooled - mean) * (pooled - mean)
			}
		}
		if mean != 0 {
			st.spread = math.Sqrt(dev/pooledWeight) / math.Abs(mean)
		}
	}

	var sum, sumDiff, weight float64
	for _, v := range st.values {
		if !v.defined || v.n < confounderMinRows || v.nB < confounderMinRows {
			continue
		}
		w := v.n * v.nB / (v.n + v.nB)
		sum += w * v.value
		sumDiff += w * (v.a - v.b)
		weight += w
		switch gapKind(v, overall) {
		case 1:
			st.same = append(st.same, v.valueName)
		case -1:
			st.opposite = append(st.opposite, v.valueName)
		default:
			st.none = append(st.none, v.valueName)
		}
	}
	if weight > 0 {
		st.adjusted, st.adjustedDiff, st.adjOK = sum/weight, sumDiff/weight, true
	}
	return st, true
}

// gapKind: 1 — разница внутри значения того же знака, что общая, и не шум;
// -1 — обратного знака; 0 — в пределах случайного разброса или меньше 2%.
func gapKind(v, overall driverStats) int {
	if math.Abs(v.value*100) < minGapPct {
		return 0
	}
	if v.hasSE && v.se > 0 && math.Abs(v.value)/v.se < 2 {
		return 0
	}
	if (v.value > 0) == (overall.value > 0) {
		return 1
	}
	return -1
}

func overallSignificant(overall driverStats) bool {
	if math.Abs(overall.value*100) < minGapPct {
		return false
	}
	return !overall.hasSE || overall.se <= 0 || math.Abs(overall.value)/overall.se >= 2
}

// verdict — вывод по одному признаку словами, которые модель перескажет.
// Разница в тексте — и в единицах показателя, и в процентах от группы Б:
// одни проценты модель пересказывала как процентные пункты.
func (s stratum) verdict(overall driverStats) (code, text string) {
	name := "«" + s.dim.name + "»"
	if !s.adjOK {
		return "unknown", "по " + name + " не хватило записей в группах для сравнения"
	}
	adj := gapText(s.adjustedDiff, s.adjusted)
	all := gapText(overall.a-overall.b, overall.value)
	if !overallSignificant(overall) {
		if math.Abs(s.adjusted*100) >= minGapPct && len(s.same)+len(s.opposite) > 0 {
			return "hidden", fmt.Sprintf("в целом разницы нет, но внутри значений %s она есть: %s с поправкой на состав", name, adj)
		}
		return "none", "разницы нет ни в целом, ни внутри значений " + name
	}
	ratio := s.adjusted / overall.value
	switch {
	case ratio < -partShare:
		return "reversed", fmt.Sprintf("объясняется %s: внутри значений разница обратная (%s против %s в целом)", name, adj, all)
	case ratio < partShare:
		return "explained", fmt.Sprintf("объясняется %s: внутри значений разница почти исчезает (%s против %s в целом)", name, adj, all)
	case ratio < keptShare:
		return "partly", fmt.Sprintf("частично объясняется %s: внутри значений разница меньше (%s против %s в целом)", name, adj, all)
	}
	if len(s.none)+len(s.opposite) > 0 && len(s.same) > 0 {
		return "uneven", fmt.Sprintf("не объясняется %s (%s с поправкой на состав против %s в целом), "+
			"но разница есть не везде", name, adj, all)
	}
	return "kept", fmt.Sprintf("не объясняется %s: внутри значений разница сохраняется (%s против %s в целом)", name, adj, all)
}

// gapText — разница А − Б в единицах показателя и в процентах от Б: «+122 (+254,4%)».
func gapText(diff, rel float64) string {
	sign := "+"
	if diff < 0 {
		sign = "−"
	}
	return fmt.Sprintf("%s%s (%+.1f%%)", sign, humanNumber(math.Abs(diff)), round2(rel*100))
}

func confounderSummary(f *dataFilter, checked, all []stratum, overall driverStats, labelA, labelB string, auto bool) map[string]any {
	items := make([]map[string]any, 0, len(checked))
	codes := make([]string, 0, len(checked))
	texts := make([]string, 0, len(checked))
	for _, s := range checked {
		code, text := s.verdict(overall)
		codes = append(codes, code)
		texts = append(texts, text)
		item := map[string]any{
			"dimension":       f.clean(s.dim.name, maxNameRunes),
			"verdict":         text,
			"composition_gap": round2(s.composition * 100),
		}
		if s.adjOK {
			item["adjusted_gap_pct"] = round2(s.adjusted * 100)
			item["adjusted_difference"] = round2(s.adjustedDiff)
		}
		if s.composition >= minComposition && s.shift != "" {
			item["composition"] = fmt.Sprintf("значение «%s» — %.0f%% группы «%s» и %.0f%% группы «%s»",
				f.clean(s.shift, maxCellRunes), s.shareA*100, f.clean(labelA, maxNameRunes), s.shareB*100, f.clean(labelB, maxNameRunes))
		} else {
			item["composition"] = "состав групп по этому признаку почти одинаков"
		}
		if len(s.finer) > 0 {
			item["includes"] = fmt.Sprintf("вложенные признаки с тем же выводом: %s — каждое их значение относится к одному значению «%s»",
				strings.Join(cleanAll(f, s.finer), ", "), f.clean(s.dim.name, maxNameRunes))
		}
		if len(s.same) > 0 {
			item["gap_in"] = cleanAll(f, s.same)
		}
		if len(s.none) > 0 {
			item["no_gap_in"] = cleanAll(f, s.none)
		}
		if len(s.opposite) > 0 {
			item["opposite_in"] = cleanAll(f, s.opposite)
		}
		items = append(items, item)
	}

	out := map[string]any{
		"overall": map[string]any{
			labelA: round2(overall.a), labelB: round2(overall.b), "gap_pct": round2(overall.value * 100),
			"difference":  round2(overall.a - overall.b),
			"significant": overallSignificant(overall),
		},
		"checked":    items,
		"conclusion": confounderConclusion(codes, texts, overall),
	}
	if auto && len(all) > len(checked) {
		var rest []string
		for _, s := range all[len(checked):] {
			rest = append(rest, f.clean(s.dim.name, maxNameRunes))
		}
		out["also_checked"] = "по остальным признакам состав групп различается меньше: " + strings.Join(rest, ", ")
	}
	return out
}

// confounderConclusion сводит выводы по признакам в один: решает самый
// сильный — если хоть один признак объясняет разницу, разница не «сама по себе».
func confounderConclusion(codes, texts []string, overall driverStats) string {
	rank := map[string]int{"reversed": 0, "explained": 1, "partly": 2, "hidden": 3, "uneven": 4, "kept": 5, "none": 6, "unknown": 7}
	best := 0
	for i, c := range codes {
		if rank[c] < rank[codes[best]] {
			best = i
		}
	}
	text := texts[best]
	switch codes[best] {
	case "reversed", "explained":
		return "Нет: разница между группами объясняется составом — " + text + "."
	case "partly":
		return "Частично: " + text + "."
	case "uneven":
		return "Частично: разница сохраняется, но не во всех группах — " + text + "."
	case "kept":
		return "Да, разница сохраняется: " + text + "."
	case "hidden":
		return "В целом разницы нет, но " + text + "."
	case "none":
		if !overallSignificant(overall) {
			return "Нет: " + text + "."
		}
	}
	return text + "."
}

func cleanAll(f *dataFilter, names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = f.clean(n, maxCellRunes)
	}
	return out
}

func confounderTable(strata []stratum, overall driverStats, labelA, labelB, metric string) *domain.ResultSet {
	rs := &domain.ResultSet{Columns: []domain.ResultColumn{
		{Name: "Признак", Type: domain.ColumnTypeText},
		{Name: "Значение", Type: domain.ColumnTypeText},
		{Name: "Записей: " + labelA, Type: domain.ColumnTypeInteger},
		{Name: "Записей: " + labelB, Type: domain.ColumnTypeInteger},
		{Name: metric + ": " + labelA, Type: domain.ColumnTypeNumeric},
		{Name: metric + ": " + labelB, Type: domain.ColumnTypeNumeric},
		{Name: "Разница, %", Type: domain.ColumnTypeNumeric},
	}}
	add := func(dim, value string, v driverStats) {
		gap := any(nil)
		if v.defined {
			gap = round2(v.value * 100)
		}
		rs.Rows = append(rs.Rows, []any{dim, value, int64(v.n), int64(v.nB), round2(v.a), round2(v.b), gap})
	}
	add(driverOverallName, driverOverallValue, overall)
	for _, s := range strata {
		// Значения, где одной из групп нет, ничего не сравнивают: в таблице
		// они выглядели бы как «0 против 56 017».
		values := slices.DeleteFunc(slices.Clone(s.values), func(v driverStats) bool { return v.n == 0 || v.nB == 0 })
		slices.SortStableFunc(values, func(x, y driverStats) int { return cmp.Compare(y.n+y.nB, x.n+x.nB) })
		if len(values) > driverDetailRows {
			values = values[:driverDetailRows]
		}
		for _, v := range values {
			add(s.dim.name, v.valueName, v)
		}
	}
	rs.RowCount = len(rs.Rows)
	return rs
}
