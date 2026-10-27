package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/heinousness/aibi/internal/core/config"
)

type Server struct {
	server          *http.Server
	shutdownTimeout time.Duration
	log             *slog.Logger
}

func NewServer(cfg config.HTTP, h http.Handler, log *slog.Logger) *Server {
	handler := http.TimeoutHandler(h, cfg.RequestTimeout, `{"error":{"code":"timeout","message":"Превышено время обработки запроса"}}`)

	return &Server{
		server: &http.Server{
			Addr:              cfg.Addr,
			Handler:           handler,
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
