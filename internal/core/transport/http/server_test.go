package http

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/heinousness/aibi/internal/core/config"
)

type streamFeature struct {
	flushable chan bool
	deadline  chan bool
}

func (f streamFeature) Register(r chi.Router) {
	r.Post("/stream", func(w http.ResponseWriter, r *http.Request) {
		_, ok := w.(http.Flusher)
		f.flushable <- ok
		_, hasDeadline := r.Context().Deadline()
		f.deadline <- hasDeadline
	})
}

func TestStreamRequestKeepsFlusherThroughRouter(t *testing.T) {
	cases := []struct {
		name      string
		accept    string
		flushable bool
	}{
		{"поток", "text/event-stream", true},
		{"обычный запрос", "application/json", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			feature := streamFeature{flushable: make(chan bool, 1), deadline: make(chan bool, 1)}
			router := NewRouter(RouterOptions{
				Config:   &config.Config{HTTP: config.HTTP{CORSOrigins: []string{"*"}}},
				Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
				Features: []Registrar{feature},
			})
			srv := httptest.NewServer(withTimeout(router, time.Minute))
			defer srv.Close()

			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/api/v1/stream", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Accept", tc.accept)
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if got := <-feature.flushable; got != tc.flushable {
				t.Errorf("http.Flusher = %v, ожидалось %v", got, tc.flushable)
			}
			if !<-feature.deadline {
				t.Error("у запроса нет предела времени")
			}
		})
	}
}
