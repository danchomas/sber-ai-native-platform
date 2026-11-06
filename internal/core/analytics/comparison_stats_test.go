package analytics

import (
	"math"
	"testing"
)

func TestStudentDistributionReferenceValues(t *testing.T) {
	for _, tc := range []struct{ df, quantile float64 }{{1, 12.7062047364}, {10, 2.2281388520}, {30, 2.0422724563}, {1000, 1.9623390808}} {
		if got := studentQuantile(0.975, tc.df); math.Abs(got-tc.quantile) > 1e-7 {
			t.Errorf("df=%v: %v, ожидалось %v", tc.df, got, tc.quantile)
		}
	}
	if math.Abs(studentCDF(1, 1)-0.75) > 1e-12 {
		t.Fatal("неверная функция распределения Стьюдента")
	}
}

func TestCompareMeans(t *testing.T) {
	a := DistributionSummary{N: 100, Mean: 10, Variance: 4}
	b := DistributionSummary{N: 100, Mean: 8, Variance: 4}
	got := CompareMeans(a, b, 1)
	if !got.Available || !got.Different || got.Difference != -2 || got.ChangePct == nil || *got.ChangePct != -20 || got.High >= 0 || got.Low >= got.High {
		t.Fatalf("ожидалось снижение с интервалом ниже нуля: %+v", got)
	}
	adjusted := CompareMeans(a, b, 20)
	if adjusted.Low >= got.Low || adjusted.High <= got.High || adjusted.Alpha != 0.0025 {
		t.Fatalf("множественные сравнения должны расширять интервал: %+v / %+v", got, adjusted)
	}
	noDifference := CompareMeans(a, a, 1)
	if !noDifference.Available || noDifference.Different || noDifference.PValue != 1 {
		t.Fatalf("одинаковые средние: %+v", noDifference)
	}
}

func TestCompareMeansInsufficientAndInvalidData(t *testing.T) {
	valid := DistributionSummary{N: 100, Mean: 10, Variance: 4}
	for _, invalid := range []DistributionSummary{
		{N: 1, Mean: 2, Variance: 1}, {N: 29, Mean: 2, Variance: 1},
		{N: 100, Mean: math.NaN(), Variance: 1}, {N: 100, Mean: 2, Variance: -1}, {N: 100, Mean: 2, Variance: math.Inf(1)},
	} {
		if CompareMeans(valid, invalid, 1).Available {
			t.Errorf("нельзя делать вывод: %+v", invalid)
		}
	}
	zero := DistributionSummary{N: 100, Mean: 0, Variance: 0}
	if got := CompareMeans(zero, zero, 1); got.Available || got.ChangePct != nil {
		t.Fatalf("нулевой разброс и база: %+v", got)
	}
}
