package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/sqlguard"
)

const toolComparePeriods = "compare_periods"

func periodTools() []struct {
	name, desc string
	params     schema
} {
	window := object(schema{
		"from": text("начало включительно: ГГГГ-ММ-ДД или дата со временем в часовом поясе датасета"),
		"to":   text("конец исключительно: для всего сентября 2026 укажи 2026-10-01"),
	}, "from", "to")
	return []struct {
		name, desc string
		params     schema
	}{{
		toolComparePeriods,
		"Сравнить распределение показателя между двумя непересекающимися периодами: размеры выборок, средние, медианы, P90, изменение и интервал Уэлча для разницы средних. " +
			"Для вопросов «стали ли выполнять быстрее», «действительно ли показатель улучшился». group_by удерживает одинаковые команды, типы и размеры задач. " +
			"Считает пропуски, незавершённые, некорректные и недонаблюдённые интервалы; это не причинный анализ и не анализ выживаемости. Не отбирай только завершённые задачи: это скроет смещение.",
		object(schema{
			"dataset":     text("имя датасета"),
			"time_column": text("дата отнесения к периоду: обычно создание или начало работы, а не завершение; выбор когорты должен соответствовать вопросу"),
			"metric": object(schema{
				"column":   text("числовая колонка; не совмещать с expr и duration"),
				"expr":     text("формула числовых колонок; не совмещать с column и duration"),
				"duration": durationSchema(),
			}),
			"baseline":       window,
			"current":        window,
			"group_by":       list(schema{"type": "string"}, "до трёх признаков для сопоставимых групп: команда, тип задачи, плановые трудозатраты; без них состав групп не контролируется"),
			"filters":        filtersSchema(),
			"direction":      enum([]string{"lower", "higher"}, "lower — меньше лучше (длительность), higher — больше лучше"),
			"observed_until": text("фактическая граница наблюдения исключительно, в часовом поясе данных; не придумывай из максимальной даты завершения или сегодняшней даты"),
			"followup_hours": schema{"type": "number", "description": "одинаковый горизонт от duration.start, до 8784 часов; требует observed_until. Слишком свежие интервалы считаются недонаблюдёнными"},
		}, "dataset", "time_column", "metric", "baseline", "current", "direction"),
	}}
}

func (r *agentRun) comparePeriods(ctx context.Context, raw json.RawMessage) (string, int, error) {
	if len(r.results) >= maxResultsPerRun {
		return "", 0, errors.New("достигнут предел запросов на один вопрос: подведи итог через answer")
	}
	var q analytics.PeriodComparisonQuery
	if err := decodeArgs(normalizeArgs(raw, "group_by", "filters"), &q); err != nil {
		return "", 0, err
	}
	compiled, err := analytics.CompilePeriodComparison(q, r.catalog)
	if err != nil {
		return "", 0, err
	}
	scope, checked, err := sqlguard.ResolveScope(compiled.SQL, r.scopes, r.s.catalog.Schema(), r.s.cfg.MaxRows)
	if err != nil {
		return "", 0, err
	}
	rs, err := r.s.exec.Query(ctx, scope.Target, checked.SQL, r.s.cfg.MaxRows, compiled.Args...)
	if err != nil {
		return "", 0, err
	}
	if rs == nil || rs.Truncated || len(rs.Rows) > analytics.MaxComparisonRows {
		return "", 0, errors.New("слишком много групп для полного сравнения: сократи group_by или сузь filters; сравнивать усечённые выборки нельзя")
	}
	out, warnings, err := periodComparisonResult(rs, q)
	if err != nil {
		return "", 0, err
	}
	warnings = append(append(compiled.Warnings, checked.Warnings...), warnings...)
	res := r.register("Сравнение периодов", compiled.SQL, checked.Tables, out, warnings, "period_comparison", &domain.ChartSpec{Type: domain.ChartTypeTable})
	res.comparison = &q
	payload := r.resultOutput(res)
	payload["periods"] = map[string]any{"baseline": q.Baseline, "current": q.Current, "time_column": r.filter.clean(q.TimeColumn, maxNameRunes)}
	payload["method"] = "Разница средних current − baseline; двусторонний интервал Уэлча, семейный уровень 95% с поправкой Бонферрони по показанным группам. Нужны минимум 30 измерений в каждом периоде и независимые наблюдения. Интервал относится к среднему, не медиане или P90."
	return encodeOutput(payload), len(out.Rows), nil
}

type periodSummary struct {
	analytics.DistributionSummary
	total, invalid, unobserved, immature int64
	median, p90                          any
}

func periodComparisonResult(rs *domain.ResultSet, q analytics.PeriodComparisonQuery) (*domain.ResultSet, []string, error) {
	dims := len(q.GroupBy)
	if rs == nil || len(rs.Columns) != dims+10 {
		return nil, nil, errors.New("неожиданные колонки статистик сравнения периодов")
	}
	type pair struct {
		keys    []any
		periods [2]*periodSummary
	}
	pairs := map[string]*pair{}
	var order []string
	for _, row := range rs.Rows {
		if len(row) != dims+10 {
			return nil, nil, errors.New("неожиданный формат статистик сравнения периодов")
		}
		period, ok := toNumber(row[0])
		if !ok || (period != 0 && period != 1) {
			return nil, nil, errors.New("неизвестный период в статистиках")
		}
		encoded, _ := json.Marshal(row[1 : dims+1])
		key := string(encoded)
		p := pairs[key]
		if p == nil {
			p = &pair{keys: row[1 : dims+1]}
			pairs[key], order = p, append(order, key)
		}
		numbers := make([]float64, 9)
		for i, value := range row[dims+1:] {
			if value == nil {
				continue
			}
			n, ok := toNumber(value)
			if !ok {
				return nil, nil, errors.New("некорректное число в статистиках сравнения")
			}
			numbers[i] = n
		}
		p.periods[int(period)] = &periodSummary{
			DistributionSummary: analytics.DistributionSummary{N: int64(numbers[1]), Mean: numbers[2], Variance: numbers[3]},
			total:               int64(numbers[0]), median: row[dims+5], p90: row[dims+6],
			invalid: int64(numbers[6]), unobserved: int64(numbers[7]), immature: int64(numbers[8]),
		}
	}
	out := &domain.ResultSet{}
	for i := range dims {
		out.Columns = append(out.Columns, rs.Columns[i+1])
	}
	for _, name := range []string{
		"Строк до", "Строк после", "Измерений до", "Измерений после", "Среднее до", "Среднее после",
		"Медиана до", "Медиана после", "P90 до", "P90 после", "Разница средних", "Изменение, %",
		"Без измерения до", "Без измерения после", "Незавершённых в окне до", "Незавершённых в окне после",
		"Некорректных до", "Некорректных после", "Недонаблюдённых до", "Недонаблюдённых после",
		"Нижняя граница разницы", "Верхняя граница разницы", "p-значение",
	} {
		out.Columns = append(out.Columns, domain.ResultColumn{Name: name, Type: domain.ColumnTypeNumeric})
	}
	out.Columns = append(out.Columns, domain.ResultColumn{Name: "Вывод", Type: domain.ColumnTypeText})
	warnings := []string{"Наблюдательные данные не доказывают причинность. Доверительный интервал предполагает независимость задач; кластеризация по исполнителям и временная зависимость отдельно не учтены. Отсутствие статистического различия не доказывает равенство."}
	if len(q.GroupBy) == 0 {
		warnings = append(warnings, "Состав задач не контролируется: общий показатель может измениться из-за долей команд, типов и размеров задач. Используй group_by для сопоставимых групп.")
	}
	incomplete := false
	for _, key := range order {
		p := pairs[key]
		a, b := p.periods[0], p.periods[1]
		if a == nil {
			a = &periodSummary{}
		}
		if b == nil {
			b = &periodSummary{}
		}
		comparison := analytics.CompareMeans(a.DistributionSummary, b.DistributionSummary, len(order))
		var delta, pct, low, high, probability any
		conclusion := "Недостаточно данных для вывода"
		if a.N > 0 && b.N > 0 {
			delta, pct = round2(comparison.Difference), optional(comparison.ChangePct)
		}
		if comparison.Available {
			low, high, probability = comparison.Low, comparison.High, comparison.PValue
			conclusion = "Статистическое различие средних не установлено"
			if comparison.Different {
				better := (q.Direction == "lower" && comparison.Difference < 0) || (q.Direction == "higher" && comparison.Difference > 0)
				conclusion = "Среднее изменилось в худшую сторону"
				if better {
					conclusion = "Среднее изменилось в лучшую сторону"
				}
			}
		}
		missingA, missingB := a.total-a.N, b.total-b.N
		if missingA > 0 || missingB > 0 || (q.Metric.Duration != nil && (q.ObservedUntil == "" || q.FollowupHours == 0)) {
			conclusion = "Только описательное сравнение: неполные или несопоставимые наблюдения"
			incomplete = true
		}
		meanA, meanB := any(nil), any(nil)
		if a.N > 0 {
			meanA = round2(a.Mean)
		}
		if b.N > 0 {
			meanB = round2(b.Mean)
		}
		row := append([]any{}, p.keys...)
		row = append(row, a.total, b.total, a.N, b.N, meanA, meanB, a.median, b.median, a.p90, b.p90,
			delta, pct, missingA, missingB, a.unobserved, b.unobserved, a.invalid, b.invalid, a.immature, b.immature,
			low, high, probability, conclusion)
		out.Rows = append(out.Rows, row)
	}
	if incomplete {
		warnings = append(warnings, "Есть неполные наблюдения или не подтверждён единый горизонт: средние и интервалы описывают только измеренные значения, а не всю когорту. Нельзя объявлять ускорение доказанным.")
	}
	if q.Metric.Duration != nil {
		warnings = append(warnings, "«Незавершённых в окне» включает интервалы, закончившиеся после границы наблюдения или горизонта. Счётчики незавершённых и недонаблюдённых могут пересекаться: не складывай их. Поправка на множественные сравнения действует только внутри этого вызова; последующий поиск лучших групп остаётся исследовательским.")
	}
	out.RowCount = len(out.Rows)
	return out, warnings, nil
}

func (s *Service) comparisonAnswer(ctx context.Context, in agentInput, q *analytics.PeriodComparisonQuery) *domain.Answer {
	scopes, err := s.catalog.Scopes(ctx)
	if err != nil {
		return s.failure(ctx, "Не удалось подготовить сравнение периодов.", err)
	}
	r := s.newRun(in.sessionID, in.question, in.datasets, in.relations, scopes, agentTools(false, toolAccess{}))
	r.filter = in.filter
	raw, _ := json.Marshal(q)
	if _, _, err := r.comparePeriods(ctx, raw); err != nil {
		return &domain.Answer{Intent: domain.IntentClarify, Text: "Не получилось сравнить периоды. " + humanReason(err)}
	}
	answer := r.finish("")
	answer.Text = s.narrate(ctx, in.filter, in.question, "Сравнение периодов", answer.Result, answer.Warnings)
	return answer
}
