package health

import (
	"context"
	"net/http"

	"time"

	"github.com/go-chi/chi/v5"

	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/transport/http/response"
)

type Models interface {
	Current(ctx context.Context) domain.LLM
}

type DBHealth interface {
	Ping(ctx context.Context) error
	PoolStats() map[string]int32
}

type Handler struct {
	db      DBHealth
	models  Models
	version string
	started time.Time
}

func New(db DBHealth, models Models, version string) *Handler {
	return &Handler{db: db, models: models, version: version, started: time.Now()}
}

type componentStatus struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type healthResponse struct {
	Status     string                     `json:"status"`
	Version    string                     `json:"version"`
	UptimeSec  int64                      `json:"uptime_sec"`
	Components map[string]componentStatus `json:"components"`
	Pool       map[string]int32           `json:"pool,omitempty"`
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	resp := healthResponse{
		Status:     "ok",
		Version:    h.version,
		UptimeSec:  int64(time.Since(h.started).Seconds()),
		Components: map[string]componentStatus{},
	}

	status := http.StatusOK
	if err := h.db.Ping(ctx); err != nil {
		resp.Components["postgres"] = componentStatus{Status: "down", Detail: err.Error()}
		resp.Status = "down"
		status = http.StatusServiceUnavailable
	} else {
		resp.Components["postgres"] = componentStatus{Status: "ok"}
		resp.Pool = h.db.PoolStats()
	}

	model := h.models.Current(ctx)
	if err := model.Ping(ctx); err != nil {
		resp.Components["llm"] = componentStatus{Status: "down", Detail: err.Error()}
		if resp.Status == "ok" {
			resp.Status = "degraded"
		}
	} else {
		resp.Components["llm"] = componentStatus{Status: "ok", Detail: model.Model()}
	}

	response.JSON(w, r, status, resp)
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		response.JSON(w, r, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	response.JSON(w, r, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) Register(r chi.Router) {
	r.Get("/health", h.Health)
	r.Get("/ready", h.Ready)
}
