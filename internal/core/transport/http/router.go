package http

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/transport/http/middleware"
)

type Registrar interface {
	Register(r chi.Router)
}

type RouterOptions struct {
	Config   *config.Config
	Logger   *slog.Logger
	Static   http.Handler
	Features []Registrar
}

func NewRouter(opts RouterOptions) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(middleware.WithLogger(opts.Logger)))
	r.Use(middleware.Recoverer)
	r.Use(middleware.CORS(opts.Config.HTTP.CORSOrigins))

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(middleware.NoCache)
		for _, f := range opts.Features {
			f.Register(api)
		}
	})

	if opts.Static != nil {
		r.Handle("/*", opts.Static)
	}
	return r
}
