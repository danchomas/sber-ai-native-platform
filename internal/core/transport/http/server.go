package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/heinousness/aibi/internal/core/config"
)

type Server struct {
	server          *http.Server
	shutdownTimeout time.Duration
	log             *slog.Logger
}

func withTimeout(h http.Handler, timeout time.Duration) http.Handler {
	limited := http.TimeoutHandler(h, timeout, `{"error":{"code":"timeout","message":"Превышено время обработки запроса"}}`)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			limited.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func NewServer(cfg config.HTTP, h http.Handler, log *slog.Logger) *Server {
	return &Server{
		server: &http.Server{
			Addr:              cfg.Addr,
			Handler:           withTimeout(h, cfg.RequestTimeout),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
		},
		shutdownTimeout: cfg.ShutdownTimeout,
		log:             log,
	}
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		s.log.Info("HTTP-сервер запущен", "addr", s.server.Addr)
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("HTTP-сервер: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.log.Info("остановка HTTP-сервера")
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.shutdownTimeout)
		defer cancel()

		if err := s.server.Shutdown(shutdownCtx); err != nil {
			_ = s.server.Close()
			return fmt.Errorf("остановка HTTP-сервера: %w", err)
		}
		return nil
	}
}
