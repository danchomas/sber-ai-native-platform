package http

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/transport/http/middleware"
	"github.com/heinousness/aibi/internal/core/web"
	"github.com/heinousness/aibi/internal/transport/http/handler"
)

type Deps struct {
	Config     *config.Config
	Logger     *slog.Logger
	Version    string
	Ingest     handler.Ingester
	Catalog    handler.Catalog
	Sources    handler.Sources
	Chat       handler.Chat
	Dashboards handler.Dashboards
	Executor   handler.Executor
	DB         handler.DBHealth
	Model      domain.LLM
}

func NewRouter(d Deps) http.Handler {
	datasets := handler.NewDataset(d.Ingest, d.Catalog)
	sources := handler.NewSource(d.Sources)
	chat := handler.NewChat(d.Chat)
	dashboards := handler.NewDashboard(d.Dashboards)
	queries := handler.NewQuery(d.Catalog, d.Executor, d.Config.Query)
	health := handler.NewHealth(d.DB, d.Model, d.Version)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(middleware.WithLogger(d.Logger)))
	r.Use(middleware.Recoverer)
	r.Use(middleware.CORS(d.Config.HTTP.CORSOrigins))

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(middleware.NoCache)

		api.Get("/health", health.Health)
		api.Get("/ready", health.Ready)

		api.Route("/datasets", func(ds chi.Router) {
			ds.Get("/", datasets.List)
			ds.With(middleware.MaxBody(d.Config.Ingest.MaxUploadBytes+1<<20)).
				Post("/", datasets.Upload)
			ds.Get("/{id}", datasets.Get)
			ds.Delete("/{id}", datasets.Delete)
			ds.Get("/{id}/profile", datasets.Profile)
			ds.Get("/{id}/preview", datasets.Preview)
		})

		api.Get("/catalog/relations", datasets.Relations)

		api.Route("/sources", func(src chi.Router) {
			src.Use(middleware.MaxBody(1 << 20))
			src.Get("/", sources.List)
			src.Post("/", sources.Attach)
			src.Delete("/{id}", sources.Detach)
			src.Get("/{id}/tables", sources.Tables)
			src.Post("/{id}/tables", sources.AttachTable)
		})

		api.Route("/sessions", func(s chi.Router) {
			s.Use(middleware.MaxBody(1 << 20))
			s.Get("/", chat.ListSessions)
			s.Post("/", chat.CreateSession)
			s.Get("/{id}", chat.GetSession)
			s.Delete("/{id}", chat.DeleteSession)
			s.Get("/{id}/messages", chat.Messages)
			s.Post("/{id}/messages", chat.Ask)
		})

		api.Route("/dashboards", func(dash chi.Router) {
			dash.Get("/", dashboards.List)
			dash.Get("/{id}", dashboards.Get)
			dash.Delete("/{id}", dashboards.Delete)
		})

		api.With(middleware.MaxBody(1<<20)).Post("/query", queries.Run)
	})

	r.Handle("/*", web.Handler())
	return r
}
