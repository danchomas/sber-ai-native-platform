package http

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/go-chi/chi/v5"

	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/transport/http/request"
	"github.com/heinousness/aibi/internal/core/transport/http/response"
)

type Handler struct {
	dashboards Dashboards
}

func New(dashboards Dashboards) *Handler {
	return &Handler{dashboards: dashboards}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	dashboards, err := h.dashboards.List(r.Context(), request.Int(r, "limit", 50))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, map[string]any{"dashboards": dashboards})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	dashboard, err := h.dashboards.Get(r.Context(), id, request.Bool(r, "refresh", true))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, dashboard)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if err := h.dashboards.Delete(r.Context(), id); err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusNoContent, nil)
}

type Dashboards interface {
	List(ctx context.Context, limit int) ([]domain.Dashboard, error)
	Get(ctx context.Context, id uuid.UUID, refresh bool) (*domain.Dashboard, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/dashboards", func(d chi.Router) {
		d.Get("/", h.List)
		d.Get("/{id}", h.Get)
		d.Delete("/{id}", h.Delete)
	})
}
