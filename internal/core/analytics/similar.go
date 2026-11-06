package analytics

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/heinousness/aibi/internal/core/domain"
)

const (
	maxSimilarColumns = 3
	// similarThreshold — предел «непохожести» от 0 до 1: дальше этого колонка
	// в подсказку не попадает, чтобы не перечислять весь датасет.
	similarThreshold = 0.6
)

// SimilarColumns подбирает до трёх колонок датасета, похожих на неверно
// названную: модель пишет «текст_ошибки» вместо «Текст ошибки» или путает
// слово в названии. Подсказка в ошибке экономит модели шаг на угадывание.
func SimilarColumns(d domain.Dataset, ref string) []string {
	want := looseName(ref)
	words := stems(ref)
	if want == "" {
		return nil
	}
	type scored struct {
		name  string
		score float64
	}
	var found []scored
	for _, col := range d.Columns {
		name := title(col)
		score := min(nameDistance(want, looseName(name)), nameDistance(want, looseName(col.Name)))
		if overlap := wordOverlap(words, stems(name)); overlap > 0 {
			score = min(score, 1-overlap)
		}
		if score < similarThreshold {
			found = append(found, scored{name, score})
		}
	}
	slices.SortStableFunc(found, func(a, b scored) int { return cmp.Compare(a.score, b.score) })
	out := make([]string, 0, maxSimilarColumns)
	for _, f := range found {
		if len(out) == maxSimilarColumns {
			break
		}
		out = append(out, f.name)
	}
	return out
}

// FindColumn ищет колонку датасета так же, как компилятор: по техническому
// имени, по заголовку из файла и по названию без регистра и пунктуации.
func FindColumn(d domain.Dataset, ref string) (domain.Column, bool) {
	ref = strings.TrimSpace(ref)
	loose := looseName(ref)
	matchers := []func(domain.Column) bool{
		func(col domain.Column) bool { return strings.EqualFold(col.Name, ref) },
		func(col domain.Column) bool { return strings.EqualFold(col.SourceName, ref) },
		func(col domain.Column) bool {
			return loose != "" && (looseName(col.SourceName) == loose || looseName(col.Name) == loose)
		},
	}
	for _, match := range matchers {
		for _, col := range d.Columns {
			if match(col) {
				return col, true
			}
		}
	}
	return domain.Column{}, false
}

// MissingColumn — ошибка о неизвестной колонке с подсказкой похожих.
func MissingColumn(d domain.Dataset, ref string) error {
	return invalid("колонки %q нет в датасете «%s»%s", ref, d.Name, similarHint(d, ref))
}

// similarHint дописывает к ошибке похожие колонки, если они есть.
func similarHint(d domain.Dataset, ref string) string {
	key := looseName(ref)
	if i := strings.IndexAny(ref, "(«"); i > 0 {
		key = looseName(ref[:i])
	}
	if grain, ok := calendarPart[key]; ok {
		return fmt.Sprintf("; это не колонка, а часть даты: используй time с колонкой даты и grain %s", grain)
	}
	names := SimilarColumns(d, ref)
	if len(names) == 0 {
		return ""
	}
	return "; похожие: «" + strings.Join(names, "», «") + "»"
}

// nameDistance — расстояние Левенштейна, нормированное на длину большего
// названия; вхождение одного названия в другое считается почти совпадением.
func nameDistance(a, b string) float64 {
	if a == "" || b == "" {
		return 1
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return 0.1
	}
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return float64(prev[len(rb)]) / float64(max(len(ra), len(rb)))
}

// stems — основы слов названия: первые пять букв слова длиннее трёх,
// чтобы «ошибки» и «ошибка» совпадали.
func stems(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		r := []rune(strings.ReplaceAll(w, "ё", "е"))
		if len(r) < 4 {
			continue
		}
		out = append(out, string(r[:min(len(r), 5)]))
	}
	return out
}

// wordOverlap — доля слов, совпавших по основе: короткое слово модели
// («факт») совпадает с началом слова из названия («фактические»).
func wordOverlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	common := 0
	for _, w := range a {
		if slices.ContainsFunc(b, func(x string) bool { return strings.HasPrefix(x, w) || strings.HasPrefix(w, x) }) {
			common++
		}
	}
	return float64(common) / float64(max(len(a), len(b)))
}

// calendarPart — календарная часть даты, которую модель принимает за колонку:
// в выводе find_drivers она называется «Месяц года», «День недели».
var calendarPart = map[string]Grain{
	"месяцгода": GrainMonthOfYear, "месяц": GrainMonthOfYear,
	"кварталгода": GrainQuarterOfYear, "квартал": GrainQuarter,
	"деньнедели": GrainDayOfWeek, "час": GrainHour, "часдня": GrainHour,
	"неделя": GrainWeek, "год": GrainYear, "день": GrainDay,
}
