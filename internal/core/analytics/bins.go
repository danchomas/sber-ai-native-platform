package analytics

import (
	"math"
	"strconv"
	"strings"

	"github.com/heinousness/aibi/internal/core/domain"
)

// Разрез по непрерывной числовой колонке (вес, сумма) даёт тысячи групп по
// одной-две строки. Такую колонку компилятор сам делит на диапазоны с
// круглыми границами 1-2-5: «до 0,2», «0,2–0,5», …, «от 20».
const (
	maxRawNumericGroups = 40
	maxBinEdges         = 9
)

func numericBins(col domain.Column) []float64 {
	p := col.Profile
	if !col.Type.IsNumeric() || p == nil || p.DistinctCount <= maxRawNumericGroups || p.MinValue == nil || p.MaxValue == nil {
		return nil
	}
	lo, errLo := strconv.ParseFloat(*p.MinValue, 64)
	hi, errHi := strconv.ParseFloat(*p.MaxValue, 64)
	if errLo != nil || errHi != nil || !(hi > lo) {
		return nil
	}
	floor := lo
	if floor <= 0 {
		floor = hi / 1000
	}
	for _, steps := range [][]float64{{1, 2, 5}, {1}} {
		if edges := roundEdges(floor, hi, steps); len(edges) >= 2 && len(edges) <= maxBinEdges {
			return edges
		}
	}
	return linearEdges(lo, hi)
}

func roundEdges(lo, hi float64, steps []float64) []float64 {
	var out []float64
	for e := -6; e <= 15; e++ {
		for _, k := range steps {
			if v := k * math.Pow10(e); v > lo && v < hi {
				out = append(out, v)
			}
		}
	}
	return out
}

// linearEdges делит узкий диапазон на равные части с круглым шагом.
func linearEdges(lo, hi float64) []float64 {
	raw := (hi - lo) / 6
	exp := math.Pow10(int(math.Floor(math.Log10(raw))))
	step := exp
	for _, k := range []float64{1, 2, 5, 10} {
		if k*exp >= raw {
			step = k * exp
			break
		}
	}
	scale := math.Pow10(max(0, -int(math.Floor(math.Log10(step)))))
	var out []float64
	for i := math.Ceil(lo / step); i*step < hi && len(out) < maxBinEdges; i++ {
		if v := math.Round(i*step*scale) / scale; v > lo {
			out = append(out, v)
		}
	}
	return out
}

func binLabel(v float64) string {
	return strings.ReplaceAll(strconv.FormatFloat(v, 'f', -1, 64), ".", ",")
}

func binExpr(ref string, edges []float64) string {
	var sb strings.Builder
	sb.WriteString("CASE WHEN " + ref + " IS NULL THEN NULL")
	for i, e := range edges {
		bound := strconv.FormatFloat(e, 'f', -1, 64)
		label := "до " + binLabel(e)
		if i > 0 {
			label = binLabel(edges[i-1]) + "–" + binLabel(e)
		}
		sb.WriteString(" WHEN " + ref + " < " + bound + " THEN '" + label + "'")
	}
	sb.WriteString(" ELSE 'от " + binLabel(edges[len(edges)-1]) + "' END")
	return sb.String()
}
