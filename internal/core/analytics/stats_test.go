package analytics

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/heinousness/aibi/internal/core/domain"
)

func monthly(values ...float64) []Point {
	start := time.Date(2022, time.January, 1, 0, 0, 0, 0, time.UTC)
	out := make([]Point, len(values))
	for i, v := range values {
		out[i] = Point{Period: start.AddDate(0, i, 0).Format("2006-01-02"), Value: v}
	}
	return out
}

func near(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

func TestSeries(t *testing.T) {
	rs := &domain.ResultSet{
		Columns: []domain.ResultColumn{{Name: "Месяц"}, {Name: "Сумма"}},
		Rows:    [][]any{{"2024-01-01", 10.5}, {"2024-02-01", int64(7)}, {"2024-03-01", nil}, {nil, 1.0}},
	}
	got, err := Series(rs, 1)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(got) != 2 || got[1].Value != 7 {
		t.Fatalf("ряд без пропусков: %+v", got)
	}
	if _, err := Series(rs, 0); err == nil {
		t.Fatal("период не может быть значением")
	}
}

func TestPearson(t *testing.T) {
	cases := []struct {
		name   string
		x, y   []float64
		want   float64
		wantOK bool
	}{
		{"прямая связь", []float64{1, 2, 3, 4}, []float64{2, 4, 6, 8}, 1, true},
		{"обратная связь", []float64{1, 2, 3, 4}, []float64{8, 6, 4, 2}, -1, true},
		{"постоянный ряд", []float64{1, 2, 3, 4}, []float64{5, 5, 5, 5}, 0, false},
		{"мало точек", []float64{1, 2}, []float64{1, 2}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Pearson(tc.x, tc.y)
			if ok != tc.wantOK || (ok && !near(got, tc.want, 1e-9)) {
				t.Fatalf("получено %v (%v), ожидалось %v (%v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestLagCorrelationsFindsLeadingSeries(t *testing.T) {
	x := monthly(1, 5, 2, 8, 3, 9, 4, 7, 2, 6, 1, 5)
	yv := []float64{0, 0}
	for _, p := range x[:len(x)-2] {
		yv = append(yv, p.Value*10)
	}
	y := monthly(yv...)

	got := LagCorrelations(x, y, 4)
	best := got[0]
	for _, c := range got {
		if c.R > best.R {
			best = c
		}
	}
	if best.Lag != 2 || !near(best.R, 1, 1e-9) {
		t.Fatalf("лучший лаг %d (r=%.3f), ожидался 2: %+v", best.Lag, best.R, got)
	}
}

func TestSeasonalIndices(t *testing.T) {
	var vs []float64
	for range 2 {
		for m := 1; m <= 12; m++ {
			v := 100.0
			if m == 7 {
				v = 200
			}
			vs = append(vs, v)
		}
	}
	got, err := SeasonalIndices(monthly(vs...), GrainMonth)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(got) != 12 || got[6].Position != 7 || got[6].Index <= 1.8 || got[0].Index >= 1 {
		t.Fatalf("индексы сезонности: %+v", got)
	}
	if _, err := SeasonalIndices(monthly(vs...), GrainYear); err == nil {
		t.Fatal("годовой ряд не даёт сезонности внутри года")
	}
}

func TestAnomalies(t *testing.T) {
	got := Anomalies(monthly(100, 102, 98, 101, 99, 400, 100, 103))
	if len(got) != 1 || got[0].Value != 400 || got[0].Score <= 0 {
		t.Fatalf("ожидался один выброс вверх: %+v", got)
	}
	if got := Anomalies(monthly(5, 5, 5, 5, 5, 5)); len(got) != 0 {
		t.Fatalf("в постоянном ряду выбросов нет: %+v", got)
	}
}

func TestForecastLinear(t *testing.T) {
	points, summary, err := Forecast(monthly(10, 20, 30, 40, 50), GrainMonth, 3)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(points) != 8 || summary.Seasonal {
		t.Fatalf("история и прогноз: %d точек, сезонность %v", len(points), summary.Seasonal)
	}
	last := points[len(points)-1]
	if last.Period != "2022-08-01" || !near(*last.Forecast, 80, 1e-9) || last.Actual != nil {
		t.Fatalf("последняя точка прогноза: %s %v", last.Period, *last.Forecast)
	}
	if points[4].Forecast == nil || *points[4].Forecast != 50 {
		t.Fatal("последний факт должен продолжаться в линии прогноза")
	}
	if !near(summary.Growth, 0.6, 1e-9) {
		t.Fatalf("рост за горизонт %.3f, ожидалось 0.6", summary.Growth)
	}
}

func TestForecastSeasonal(t *testing.T) {
	var vs []float64
	for y := range 3 {
		for m := 1; m <= 12; m++ {
			v := 100 + float64(y*12+m)
			if m == 12 {
				v += 50
			}
			vs = append(vs, v)
		}
	}
	points, summary, err := Forecast(monthly(vs...), GrainMonth, 12)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if !summary.Seasonal || summary.FitError > 0.05 {
		t.Fatalf("ожидалась сезонная модель с малой ошибкой: %+v", summary)
	}
	december := points[len(points)-1]
	november := points[len(points)-2]
	if *december.Forecast-*november.Forecast < 30 {
		t.Fatalf("декабрьский пик не перенесён в прогноз: ноябрь %.1f, декабрь %.1f",
			*november.Forecast, *december.Forecast)
	}
	if *december.Lower >= *december.Forecast || *december.Upper <= *december.Forecast {
		t.Fatal("интервал должен окружать прогноз")
	}
}

func TestForecastRejectsShortSeries(t *testing.T) {
	if _, _, err := Forecast(monthly(1, 2, 3), GrainMonth, 3); err == nil {
		t.Fatal("по трём точкам прогноз не строится")
	}
	if _, _, err := Forecast(monthly(1, 2, 3, 4), GrainMonthOfYear, 3); err == nil {
		t.Fatal("по месяцу года прогноз не строится")
	}
}

func ExampleForecast() {
	_, summary, _ := Forecast(monthly(10, 20, 30, 40), GrainQuarter, 2)
	fmt.Printf("%s, рост %.0f%%\n", summary.Method, summary.Growth*100)
}

func TestFit(t *testing.T) {
	tests := []struct {
		name      string
		xs, ys    []float64
		slope     float64
		r2        float64
		expectErr bool
	}{
		{name: "exact line", xs: []float64{1, 2, 3, 4, 5, 6}, ys: []float64{12, 14, 16, 18, 20, 22}, slope: 2, r2: 1},
		{name: "falling demand", xs: []float64{10, 11, 12, 13, 14, 15}, ys: []float64{100, 95, 90, 85, 80, 75}, slope: -5, r2: 1},
		{name: "too short", xs: []float64{1, 2, 3}, ys: []float64{1, 2, 3}, expectErr: true},
		{name: "constant driver", xs: []float64{5, 5, 5, 5, 5, 5}, ys: []float64{1, 2, 3, 4, 5, 6}, expectErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg, err := Fit(tt.xs, tt.ys)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("ожидалась ошибка, получено %+v", reg)
				}
				return
			}
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if math.Abs(reg.Slope-tt.slope) > 1e-9 || math.Abs(reg.R2-tt.r2) > 1e-9 {
				t.Fatalf("наклон %v, R² %v; ожидалось %v, %v", reg.Slope, reg.R2, tt.slope, tt.r2)
			}
		})
	}
}
