package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestResultSetDurationInMilliseconds(t *testing.T) {
	data, err := json.Marshal(ResultSet{Duration: Millis(346 * time.Millisecond)})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if got := raw["duration_ms"]; got != float64(346) {
		t.Fatalf("duration_ms = %v, ожидалось 346", got)
	}

	var back ResultSet
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if time.Duration(back.Duration) != 346*time.Millisecond {
		t.Fatalf("после разбора %v, ожидалось 346ms", time.Duration(back.Duration))
	}
}
