package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/datasets", nil))

	tests := []struct {
		header   string
		expected string
	}{
		{header: "X-Content-Type-Options", expected: "nosniff"},
		{header: "X-Frame-Options", expected: "DENY"},
		{header: "Referrer-Policy", expected: "no-referrer"},
		{header: "Content-Security-Policy", expected: "default-src 'none'; frame-ancestors 'none'"},
	}
	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			if got := rec.Header().Get(tt.header); got != tt.expected {
				t.Fatalf("%s = %q, ожидалось %q", tt.header, got, tt.expected)
			}
		})
	}
}
