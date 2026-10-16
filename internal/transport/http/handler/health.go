package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/heinousness/aibi/internal/domain"
)

type DBHealth interface {
	Ping(ctx context.Context) error
	PoolStats() map[string]int32
}

type HealthHandler struct {
	db      DBHealth
	model   domain.LLM
	version string
	started time.Time
}

func NewHealth(db DBHealth, model domain.LLM, version string) *HealthHandler {
	return &HealthHandler{db: db, model: model, version: version, started: time.Now()}
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

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
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

	if err := h.model.Ping(ctx); err != nil {
		resp.Components["llm"] = componentStatus{Status: "down", Detail: err.Error()}
		if resp.Status == "ok" {
			resp.Status = "degraded"
		}
	} else {
		resp.Components["llm"] = componentStatus{Status: "ok", Detail: h.model.Model()}
	}

	writeJSON(w, r, status, resp)
}

func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		writeJSON(w, r, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]string{"status": "ready"})
}
