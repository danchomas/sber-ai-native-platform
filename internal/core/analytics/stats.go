package analytics

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/heinousness/aibi/internal/core/domain"
)

// Статистика для сценариев с гипотезами: сезонность, лаги, прогноз,
// выбросы. Считается в Go по агрегированному ряду, а не моделью: малая
// модель в арифметике ошибается, а здесь результат воспроизводим.

// Point — значение метрики за период. Period — начало периода, ГГГГ-ММ-ДД.
type Point struct {
	Period string
	Value  float64
}

// Series достаёт ряд из результата вида «период | метрика...»: первая
// колонка — период, column — номер колонки со значением.
func Series(rs *domain.ResultSet, column int) ([]Point, error) {
	if rs == nil || len(rs.Columns) <= column || column < 1 {
		return nil, errors.New("в результате нет ряда «период — значение»")
	}
	out := make([]Point, 0, len(rs.Rows))
	for _, row := range rs.Rows {
		if len(row) <= column || row[0] == nil {
			continue
		}
		v, ok := toFloat(row[column])
		if !ok {
			continue
		}
		out = append(out, Point{Period: fmt.Sprint(row[0]), Value: v})
	}
	return out, nil
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, !math.IsNaN(x) && !math.IsInf(x, 0)
	case float32:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	case int:
		return float64(x), true
	}
	return 0, false
}

func values(points []Point) []float64 {
	out := make([]float64, len(points))
	for i, p := range points {
		out[i] = p.Value
	}
	return out
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func stddev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	m := mean(xs)
	var s float64
	for _, x := range xs {
		s += (x - m) * (x - m)
	}
	return math.Sqrt(s / float64(len(xs)-1))
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// linearFit — метод наименьших квадратов по номеру точки.
func linearFit(ys []float64) (slope, intercept float64) {
	n := float64(len(ys))
	if n < 2 {
		return 0, mean(ys)
	}
	var sx, sy, sxy, sxx float64
	for i, y := range ys {
		x := float64(i)
		sx += x
		sy += y
		sxy += x * y
		sxx += x * x
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return 0, sy / n
	}
	slope = (n*sxy - sx*sy) / den
	return slope, (sy - slope*sx) / n
}

// Pearson — коэффициент корреляции; ok=false, если он не определён.
func Pearson(xs, ys []float64) (float64, bool) {
	if len(xs) != len(ys) || len(xs) < 3 {
		return 0, false
	}
	mx, my := mean(xs), mean(ys)
	var sxy, sxx, syy float64
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 || syy == 0 {
		return 0, false
	}
	return sxy / math.Sqrt(sxx*syy), true
}

// Regression — линейная зависимость y от x по парам наблюдений:
// y ≈ Intercept + Slope·x. R2 — доля разброса y, которую объясняет x;
// Elasticity — на сколько процентов меняется y при изменении x на 1%
// в средней точке.
type Regression struct {
	Slope      float64
	Intercept  float64
	R2         float64
	Elasticity float64
	Pairs      int
}

// minRegressionPairs — меньше пар наблюдений оценка зависимости
// превращается в гадание.
const minRegressionPairs = 6

// Fit оценивает зависимость y от x методом наименьших квадратов.
func Fit(xs, ys []float64) (Regression, error) {
	if len(xs) != len(ys) {
		return Regression{}, errors.New("ряды разной длины")
	}
	if len(xs) < minRegressionPairs {
		return Regression{}, fmt.Errorf("для оценки зависимости нужно хотя бы %d периодов, есть %d", minRegressionPairs, len(xs))
	}
	mx, my := mean(xs), mean(ys)
	var sxy, sxx, syy float64
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 {
		return Regression{}, errors.New("показатель-драйвер не менялся: по истории нельзя оценить его влияние")
	}
	reg := Regression{Slope: sxy / sxx, Pairs: len(xs)}
	reg.Intercept = my - reg.Slope*mx
	if syy > 0 {
		reg.R2 = sxy * sxy / (sxx * syy)
	}
	if my != 0 {
		reg.Elasticity = reg.Slope * mx / my
	}
	return reg, nil
}

// LagCorrelation — корреляция x[t] и y[t+lag] для лагов 0..maxLag по
// общим периодам двух рядов: «изменения X предшествуют росту Y на N периодов?».
type LagCorrelation struct {
	Lag   int
	R     float64
	Pairs int
}

func LagCorrelations(x, y []Point, maxLag int) []LagCorrelation {
	byPeriod := make(map[string]float64, len(y))
	for _, p := range y {
		byPeriod[p.Period] = p.Value
	}
	// Ряды выравниваются по общим периодам, лаг — сдвиг в этих периодах.
	var xs, ys []float64
	for _, p := range x {
		if v, ok := byPeriod[p.Period]; ok {
			xs = append(xs, p.Value)
			ys = append(ys, v)
		}
	}

	var out []LagCorrelation
	for lag := 0; lag <= maxLag; lag++ {
		if len(xs)-lag < 4 {
			break
		}
		r, ok := Pearson(xs[:len(xs)-lag], ys[lag:])
		if !ok {
			continue
		}
		out = append(out, LagCorrelation{Lag: lag, R: r, Pairs: len(xs) - lag})
	}
	return out
}

// SeasonalIndex — насколько период сезона (месяц года, квартал, день
// недели) выше или ниже среднего: 1.25 значит на 25% выше обычного.
type SeasonalIndex struct {
	Position int
	Index    float64
	Mean     float64
	Count    int
}

func SeasonalIndices(points []Point, grain Grain) ([]SeasonalIndex, error) {
	position, err := seasonPosition(grain)
	if err != nil {
		return nil, err
	}
	overall := mean(values(points))
	if overall == 0 {
		return nil, errors.New("среднее значение ряда равно нулю, сезонность не определить")
	}

	groups := map[int][]float64{}
	for _, p := range points {
		t, err := parsePeriod(p.Period)
		if err != nil {
			return nil, err
		}
		pos := position(t)
		groups[pos] = append(groups[pos], p.Value)
	}

	out := make([]SeasonalIndex, 0, len(groups))
	for pos, vs := range groups {
		m := mean(vs)
		out = append(out, SeasonalIndex{Position: pos, Index: m / overall, Mean: m, Count: len(vs)})
	}
	slices.SortFunc(out, func(a, b SeasonalIndex) int { return a.Position - b.Position })
	return out, nil
}

func seasonPosition(grain Grain) (func(time.Time) int, error) {
	switch grain {
	case GrainMonth:
		return func(t time.Time) int { return int(t.Month()) }, nil
	case GrainQuarter:
		return func(t time.Time) int { return (int(t.Month())-1)/3 + 1 }, nil
	case GrainDay:
		return func(t time.Time) int { return (int(t.Weekday())+6)%7 + 1 }, nil
	}
	return nil, invalid("сезонность считается по ряду с grain month, quarter или day")
}

// Anomaly — точка, далёкая от типичных значений ряда. Используется
// устойчивая оценка (медиана и MAD): один выброс не сдвигает порог.
type Anomaly struct {
	Point
	Score    float64
	Expected float64
}

const anomalyThreshold = 3.5

func Anomalies(points []Point) []Anomaly {
	if len(points) < 6 {
		return nil
	}
	vs := values(points)
	med := median(vs)
	deviations := make([]float64, len(vs))
	for i, v := range vs {
		deviations[i] = math.Abs(v - med)
	}
	mad := median(deviations)
	if mad == 0 {
		mad = mean(deviations)
	}
	if mad == 0 {
		return nil
	}

	var out []Anomaly
	for _, p := range points {
		score := 0.6745 * (p.Value - med) / mad
		if math.Abs(score) >= anomalyThreshold {
			out = append(out, Anomaly{Point: p, Score: score, Expected: med})
		}
	}
	return out
}

// ForecastPoint — строка прогноза: факт для истории, прогноз с
// интервалом для будущих периодов.
type ForecastPoint struct {
	Period   string
	Actual   *float64
	Forecast *float64
	Lower    *float64
	Upper    *float64
}

type ForecastSummary struct {
	Method     string
	Seasonal   bool
	FitError   float64
	Growth     float64
	LastActual float64
	LastValue  float64
}

// Forecast продлевает ряд на horizon периодов: линейный тренд, а при
// двух и более полных сезонах — тренд плюс аддитивная сезонность.
// Интервал — ±1.96 стандартного отклонения остатков, расширяющийся с горизонтом.
func Forecast(points []Point, grain Grain, horizon int) ([]ForecastPoint, ForecastSummary, error) {
	if len(points) < 4 {
		return nil, ForecastSummary{}, invalid("для прогноза нужно хотя бы 4 периода истории, есть %d", len(points))
	}
	step, err := periodStep(grain)
	if err != nil {
		return nil, ForecastSummary{}, err
	}
	horizon = min(max(horizon, 1), 36)

	ys := values(points)
	n := len(ys)
	slope, intercept := linearFit(ys)
	trend := func(i int) float64 { return intercept + slope*float64(i) }

	season := map[Grain]int{GrainMonth: 12, GrainQuarter: 4, GrainWeek: 52, GrainDay: 7}[grain]
	seasonal := make([]float64, max(season, 1))
	isSeasonal := season > 0 && n >= 2*season
	if isSeasonal {
		sums := make([]float64, season)
		counts := make([]int, season)
		for i, y := range ys {
			sums[i%season] += y - trend(i)
			counts[i%season]++
		}
		for k := range seasonal {
			if counts[k] > 0 {
				seasonal[k] = sums[k] / float64(counts[k])
			}
		}
	}
	model := func(i int) float64 {
		if isSeasonal {
			return trend(i) + seasonal[i%season]
		}
		return trend(i)
	}

	residuals := make([]float64, n)
	var absPct float64
	var pctCount int
	for i, y := range ys {
		residuals[i] = y - model(i)
		if y != 0 {
			absPct += math.Abs(residuals[i] / y)
			pctCount++
		}
	}
	sigma := stddev(residuals)

	last, err := parsePeriod(points[n-1].Period)
	if err != nil {
		return nil, ForecastSummary{}, err
	}

	out := make([]ForecastPoint, 0, n+horizon)
	for _, p := range points {
		v := p.Value
		out = append(out, ForecastPoint{Period: p.Period, Actual: &v})
	}
	// Последняя фактическая точка дублируется в прогнозе, чтобы линии
	// факта и прогноза на графике сходились.
	joint := ys[n-1]
	out[n-1].Forecast = &joint

	var lastValue float64
	for h := 1; h <= horizon; h++ {
		i := n - 1 + h
		f := model(i)
		width := 1.96 * sigma * math.Sqrt(1+float64(h)/float64(n))
		lo, hi := f-width, f+width
		fv := f
		out = append(out, ForecastPoint{
			Period: step(last, h).Format("2006-01-02"), Forecast: &fv, Lower: &lo, Upper: &hi,
		})
		lastValue = f
	}

	summary := ForecastSummary{
		Method: "линейный тренд", Seasonal: isSeasonal,
		LastActual: ys[n-1], LastValue: lastValue,
	}
	if isSeasonal {
		summary.Method = "тренд с сезонностью"
	}
	if pctCount > 0 {
		summary.FitError = absPct / float64(pctCount)
	}
	if ys[n-1] != 0 {
		summary.Growth = (lastValue - ys[n-1]) / math.Abs(ys[n-1])
	}
	return out, summary, nil
}

func periodStep(grain Grain) (func(time.Time, int) time.Time, error) {
	switch grain {
	case GrainDay:
		return func(t time.Time, k int) time.Time { return t.AddDate(0, 0, k) }, nil
	case GrainWeek:
		return func(t time.Time, k int) time.Time { return t.AddDate(0, 0, 7*k) }, nil
	case GrainMonth:
		return func(t time.Time, k int) time.Time { return t.AddDate(0, k, 0) }, nil
	case GrainQuarter:
		return func(t time.Time, k int) time.Time { return t.AddDate(0, 3*k, 0) }, nil
	case GrainYear:
		return func(t time.Time, k int) time.Time { return t.AddDate(k, 0, 0) }, nil
	}
	return nil, invalid("для прогноза нужен ряд с grain day, week, month, quarter или year")
}

func parsePeriod(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("период %q не похож на дату", s)
}
