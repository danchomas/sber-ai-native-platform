package service

import (
	"regexp"
	"strings"

	"github.com/heinousness/aibi/internal/core/analytics"
)

// Алгоритм под тип вопроса подставляется в промпт отдельно, а не лежит в нём
// всегда: короткий план для одного вопроса модель выполняет точнее, чем
// раздел из десятка правил, а простые вопросы не платят за него длиной промпта.
const planHeader = "ПЛАН ДЛЯ ЭТОГО ВОПРОСА:"

const planConditions = `Вопрос об условиях («при каких условиях», «от чего зависит»):
1. find_drivers с показателем из вопроса. Если вопрос о группе («крупные задачи», «заказы из рекламы») — group_a с её условием и group_b с противоположным.
2. Сильнейший признак покажи одним aggregate: разрез по нему, для двух групп — две метрики с filters и разными label.
3. Ответ: «Сильнее всего — <признак>: при <значении> — X против Y в среднем», второй признак, если он тоже заметен, и фраза «по остальным признакам разница в пределах случайного разброса».`

const planChange = `Вопрос о перемене во времени («стало ли быстрее», «после X стало», «в последние месяцы»):
1. compare_periods: from и to — проверяемый период из вопроса («конец сентября 2026» — 2026-09-21…2026-09-30, «последние месяцы» — последние три полных месяца данных); для длительностей date_column — дата окончания.
2. Если перемена привязана к событию, а не к датам («после выхода новой версии»), сравни группы до и после через aggregate по признаку события или через check_confounder.
3. Ответ начни с conclusion инструмента — «Нет», «Да» или «Нельзя утверждать», — затем значение периода, обычный диапазон и тот же период год назад.`

const planClaim = `Проверка утверждения («правда ли», «можно ли утверждать», «не объясняется ли»):
1. Посчитай само утверждение: aggregate с двумя группами (метрики с filters) или с разрезом из вопроса.
2. Проверь его check_confounder с теми же group_a и group_b: by — признак, названный в вопросе («категория», «состав аудитории»); если не назван — оставь by пустым, инструмент сам найдёт признак, по которому группы различаются.
3. Вывод бери из conclusion: да, нет или частично. «Частично» — назови группы, где разница есть, и где её нет.
4. Если утверждение о влиянии («A увеличивает B», «из-за A»), закончи фразой: «Это связь в данных; возможно и обратное — <B влияет на A>».`

var (
	conditionsQuestion = regexp.MustCompile(`при как(их|ом) услови|от чего завис|что влияет|как(ие|ой) фактор|в каких случаях|когда .*(больше|дольше|чаще|меньше|дешевле|дороже)`)
	changeQuestion     = regexp.MustCompile(`(стал[аио]?|стали)\s+(ли\s+)?([а-яё]+\s+){0,3}(быстрее|медленнее|дольше|короче|больше|меньше|чаще|реже|лучше|хуже|выше|ниже|дешевле|дороже)|изменил(ся|ась|ось|ись)|после (выхода|запуска|внедрения|изменения)|в последние (месяц|недел|дни)`)
	claimQuestion      = regexp.MustCompile(`правда ли|можно ли утверждать|верно ли|подтверждается ли|действительно ли|не объясняется ли|объясняется ли|или это (эффект|из-за|связано)|за сч[её]т (чего|того|состава)`)
	rateQuestion       = regexp.MustCompile(`чаще|реже|вероятн|частот`)
	// yesNoQuestion — вопрос, главная часть которого ждёт «да» или «нет».
	// «Какие площадки лучше и не объясняется ли…» сюда не относится: ответ
	// на него начинается с перечня.
	yesNoQuestion = regexp.MustCompile(`^(правда ли|можно ли|верно ли|подтверждается ли|действительно ли|стал[аио]? ли|стали ли|изменил\S* ли|есть ли|влияет ли|зависит ли)`)
	verdictStart  = regexp.MustCompile(`^(да|нет|частично|нельзя|однозначно|утверждать нельзя|скорее|не совсем|в целом)[\s,.:;—-]`)
)

// questionPlan подбирает алгоритм под вопрос; пусто — вопрос не из сложных.
func questionPlan(question string) string {
	q := strings.ToLower(question)
	var plans []string
	if conditionsQuestion.MatchString(q) {
		plans = append(plans, planConditions)
	}
	if changeQuestion.MatchString(q) {
		plans = append(plans, planChange)
	}
	if claimQuestion.MatchString(q) && !changeQuestion.MatchString(q) {
		plans = append(plans, planClaim)
	}
	if len(plans) == 0 {
		return ""
	}
	return planHeader + "\n" + strings.Join(plans, "\n")
}

// isClaim — вопрос, на который отвечают «да», «нет» или «частично».
func isClaim(question string) bool {
	return yesNoQuestion.MatchString(strings.ToLower(strings.TrimSpace(question)))
}

// reviewAnswer проверяет ответ до приёма. Ловит то, что видно без модели:
// вопрос «где чаще» отвечен количеством записей, а не частотой, и ответ на
// вопрос-проверку не начинается с вывода. Возвращает просьбу переделать или пусто.
func (r *agentRun) reviewAnswer(text, resultID string) string {
	var notes []string
	q := strings.ToLower(r.question)
	if rateQuestion.MatchString(q) {
		if res := r.result(resultID); res != nil && res.query != nil && countsOnly(*res.query) {
			notes = append(notes, "вопрос о том, где чаще, а результат — число записей: сравни частоту — "+
				"share с filters условия события по тем же группам — и ответь по ней")
		}
	}
	if isClaim(r.question) && !verdictStart.MatchString(strings.ToLower(strings.TrimSpace(text))) {
		notes = append(notes, "это вопрос-проверка: начни текст ответа с вывода — «Да», «Нет», «Частично» или «Нельзя утверждать», "+
			"затем числа, которые его доказывают")
	}
	if len(notes) == 0 {
		return ""
	}
	return "Ответ не принят: " + strings.Join(notes, "; ") + "."
}

// countsOnly — результат в разрезе, где все показатели — количества или суммы.
func countsOnly(q analytics.Query) bool {
	if len(q.GroupBy) == 0 && q.Time == nil {
		return false
	}
	for _, m := range q.Metrics {
		switch analytics.Func(strings.ToLower(string(m.Func))) {
		case analytics.FuncCount, analytics.FuncCountDistinct, analytics.FuncSum:
		default:
			return false
		}
	}
	return true
}

// verdictOf — вывод ответа на вопрос-проверку одним словом, по началу текста.
func verdictOf(question, text string) string {
	if !isClaim(question) {
		return ""
	}
	t := strings.ToLower(strings.TrimSpace(text))
	switch {
	case strings.HasPrefix(t, "частично"), strings.HasPrefix(t, "не совсем"):
		return "частично"
	case strings.HasPrefix(t, "нельзя"), strings.HasPrefix(t, "однозначно"), strings.HasPrefix(t, "утверждать нельзя"):
		return "нельзя утверждать"
	case strings.HasPrefix(t, "да"), strings.HasPrefix(t, "скорее да"):
		return "да"
	case strings.HasPrefix(t, "нет"), strings.HasPrefix(t, "скорее нет"):
		return "нет"
	}
	return ""
}
