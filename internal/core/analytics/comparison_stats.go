package analytics

import "math"

// DistributionSummary содержит статистики индивидуальных наблюдений, не средних по дням.
type DistributionSummary struct {
	N        int64
	Mean     float64
	Variance float64
}

type MeanComparison struct {
	Difference float64
	ChangePct  *float64
	Low        float64
	High       float64
	PValue     float64
	Alpha      float64
	Available  bool
	Different  bool
}

const MinComparisonSample = 30

// CompareMeans считает двусторонний интервал Уэлча для current-baseline.
// Поправка Бонферрони учитывает число показанных сравнений; независимость наблюдений — допущение.
func CompareMeans(baseline, current DistributionSummary, comparisons int) MeanComparison {
	out := MeanComparison{Difference: current.Mean - baseline.Mean, Alpha: 0.05 / float64(max(comparisons, 1))}
	if baseline.Mean != 0 {
		pct := out.Difference / math.Abs(baseline.Mean) * 100
		if finite(pct) {
			out.ChangePct = &pct
		}
	}
	if baseline.N < MinComparisonSample || current.N < MinComparisonSample ||
		baseline.Variance < 0 || current.Variance < 0 || !finite(baseline.Mean) || !finite(current.Mean) ||
		!finite(baseline.Variance) || !finite(current.Variance) || !finite(out.Difference) {
		return out
	}
	a := baseline.Variance / float64(baseline.N)
	b := current.Variance / float64(current.N)
	se2 := a + b
	// При нулевом разбросе оценить неопределённость по выборке нельзя.
	if se2 <= 0 || !finite(se2) {
		return out
	}
	df := se2 * se2 / (a*a/float64(baseline.N-1) + b*b/float64(current.N-1))
	critical := studentQuantile(1-out.Alpha/2, df)
	half := critical * math.Sqrt(se2)
	out.Low, out.High = out.Difference-half, out.Difference+half
	out.PValue = math.Min(1, 2*(1-studentCDF(math.Abs(out.Difference)/math.Sqrt(se2), df)))
	out.Available = finite(out.Low) && finite(out.High) && finite(out.PValue)
	out.Different = out.Available && (out.Low > 0 || out.High < 0)
	return out
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func studentCDF(t, df float64) float64 {
	if t == 0 {
		return 0.5
	}
	x := df / (df + t*t)
	p := regularizedBeta(x, df/2, 0.5) / 2
	if t > 0 {
		return 1 - p
	}
	return p
}

func studentQuantile(p, df float64) float64 {
	low, high := 0.0, 2.0
	for studentCDF(high, df) < p && high < 1e6 {
		high *= 2
	}
	for range 80 {
		mid := (low + high) / 2
		if studentCDF(mid, df) < p {
			low = mid
		} else {
			high = mid
		}
	}
	return (low + high) / 2
}

func regularizedBeta(x, a, b float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	lgab, _ := math.Lgamma(a + b)
	lga, _ := math.Lgamma(a)
	lgb, _ := math.Lgamma(b)
	front := math.Exp(lgab - lga - lgb + a*math.Log(x) + b*math.Log1p(-x))
	if x < (a+1)/(a+b+2) {
		return front * betaFraction(x, a, b) / a
	}
	return 1 - front*betaFraction(1-x, b, a)/b
}

func betaFraction(x, a, b float64) float64 {
	const tiny = 1e-300
	protect := func(v float64) float64 {
		if math.Abs(v) < tiny {
			return math.Copysign(tiny, v)
		}
		return v
	}
	c, d := 1.0, 1/protect(1-(a+b)*x/(a+1))
	h := d
	for m := 1; m <= 300; m++ {
		mf := float64(m)
		aa := mf * (b - mf) * x / ((a + 2*mf - 1) * (a + 2*mf))
		d = 1 / protect(1+aa*d)
		c = protect(1 + aa/c)
		h *= d * c
		aa = -(a + mf) * (a + b + mf) * x / ((a + 2*mf) * (a + 2*mf + 1))
		d = 1 / protect(1+aa*d)
		c = protect(1 + aa/c)
		delta := d * c
		h *= delta
		if math.Abs(delta-1) < 3e-14 {
			break
		}
	}
	return h
}
