package llm

import "testing"

func TestIsFreePrice(t *testing.T) {
	cases := map[string]bool{
		"0":         true,
		"0.0":       true,
		"0.000000":  true,
		" 0 ":       true,
		"0.0000015": false,
		"3":         false,
		"-1":        false,
		"":          false,
		"бесплатно": false,
	}
	for price, want := range cases {
		if got := isFreePrice(price); got != want {
			t.Errorf("isFreePrice(%q) = %v, ожидалось %v", price, got, want)
		}
	}
}
