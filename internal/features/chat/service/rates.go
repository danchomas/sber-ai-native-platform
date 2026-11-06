package service

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/sqlguard"
)

// Проверка частоты события по группам на случайный разброс. На сотнях тысяч
// записей 8,55% и 8,36% у перевозчиков — одно и то же, а модель по таблице
// называла «чаще всего срывает срок» того, кто оказался первым.
const (
	// rateNoiseZ — как в find_drivers: отличие меньше трёх стандартных
	// ошибок считается случайным.
	rateNoiseZ      = 3
	rateMinRows     = 30
	rateShownGroups = 4
)

type rateGroup struct {
	name string
	n, k float64
	z    float64
}

func (g rateGroup) rate() float64 { return g.k / g.n }

// rateSpread пересчитывает к aggregate с частотой события количества по
// группам и говорит модели, какие группы отличаются от средней частоты
// сильнее случайного разброса. Пустая строка — проверка неприменима.
func (r *agentRun) rateSpread(ctx context.Context, q analytics.Query) string {
	compiled, label, ok, err := analytics.CompileRateCounts(q, r.catalog, r.s.cfg.MaxRows)
	if err != nil || !ok {
		return ""
	}
	scope, checked, err := sqlguard.ResolveScope(compiled.SQL, r.scopes, r.s.catalog.Schema(), r.s.cfg.MaxRows)
	if err != nil {
		return ""
	}
	rs, err := r.s.exec.Query(ctx, scope.Target, checked.SQL, r.s.cfg.MaxRows, compiled.Args...)
	if err != nil {
		r.s.log.WarnContext(ctx, "проверка разброса частоты не выполнилась", "error", err)
		return ""
	}
	known, events := rs.ColumnIndex(analytics.RateKnown), rs.ColumnIndex(analytics.RateEvents)
	if known < 1 || events < 1 {
		return ""
	}
	groups := make([]rateGroup, 0, len(rs.Rows))
	for _, row := range rs.Rows {
		n, okN := numberOf(row[known])
		k, okK := numberOf(row[events])
		if !okN || !okK || n < rateMinRows {
			continue
		}
		names := make([]string, 0, known)
		for _, v := range row[:min(known, events)] {
			names = append(names, cellText(r.filter, v))
		}
		groups = append(groups, rateGroup{name: strings.Join(names, " · "), n: n, k: k})
	}
	return rateSpreadHint(r.filter.clean(label, maxNameRunes), groups)
}

// rateSpreadHint — вывод проверки для модели: либо группы не различаются
// вовсе, либо какие из них выше и ниже средней частоты.
func rateSpreadHint(label string, groups []rateGroup) string {
	if len(groups) < 2 {
		return ""
	}
	var n, k float64
	for _, g := range groups {
		n, k = n+g.n, k+g.k
	}
	p := k / n
	if p <= 0 || p >= 1 {
		return ""
	}
	var above, below []rateGroup
	low, high := groups[0].rate(), groups[0].rate()
	for i := range groups {
		g := &groups[i]
		g.z = (g.rate() - p) / math.Sqrt(p*(1-p)/g.n)
		low, high = math.Min(low, g.rate()), math.Max(high, g.rate())
		switch {
		case g.z >= rateNoiseZ:
			above = append(above, *g)
		case g.z <= -rateNoiseZ:
			below = append(below, *g)
		}
	}
	if len(above)+len(below) == 0 {
		return fmt.Sprintf("Частота «%s» по группам — от %s%% до %s%%: это случайный разброс, группы по ней не различаются. "+
			"Не называй группу, где событие случается «чаще всего» или «реже всего», и не ранжируй группы: "+
			"ответь, что они не различаются, и приведи этот диапазон.",
			label, humanNumber(low*100), humanNumber(high*100))
	}
	slices.SortFunc(above, func(a, b rateGroup) int { return cmp.Compare(b.z, a.z) })
	slices.SortFunc(below, func(a, b rateGroup) int { return cmp.Compare(a.z, b.z) })
	parts := []string{fmt.Sprintf("Средняя частота «%s» — %s%%. Сильнее случайного разброса от неё отличаются", label, humanNumber(p*100))}
	if len(above) > 0 {
		parts = append(parts, "выше: "+rateNames(above))
	}
	if len(below) > 0 {
		parts = append(parts, "ниже: "+rateNames(below))
	}
	return strings.Join(parts, "; ") + ". Остальные группы от средней не отличаются: не выделяй их и не делай выводов из разницы между ними."
}

func rateNames(groups []rateGroup) string {
	names := make([]string, 0, rateShownGroups)
	for _, g := range groups[:min(len(groups), rateShownGroups)] {
		names = append(names, fmt.Sprintf("«%s» (%s%%)", g.name, humanNumber(g.rate()*100)))
	}
	if rest := len(groups) - rateShownGroups; rest > 0 {
		names = append(names, fmt.Sprintf("ещё %d", rest))
	}
	return strings.Join(names, ", ")
}
