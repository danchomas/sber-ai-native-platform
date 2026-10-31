package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

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
	period := domain.DateRange{
		From: strings.TrimSpace(r.URL.Query().Get("from")),
		To:   strings.TrimSpace(r.URL.Query().Get("to")),
	}
	dashboard, err := h.dashboards.Get(r.Context(), id, request.Bool(r, "refresh", true), period)
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

func (h *Handler) Trash(w http.ResponseWriter, r *http.Request) {
	items, err := h.dashboards.Trash(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if err := h.dashboards.RestoreDashboard(r.Context(), id); err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusNoContent, nil)
}

func (h *Handler) RestoreWidget(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	widgetID, err := request.UUID(r, "widget")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if err := h.dashboards.RestoreWidget(r.Context(), id, widgetID); err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusNoContent, nil)
}

type widgetRequest struct {
	Title    string           `json:"title"`
	Question string           `json:"question"`
	SQL      string           `json:"sql"`
	Query    json.RawMessage  `json:"query"`
	Chart    domain.ChartSpec `json:"chart"`
	Width    int              `json:"width"`
}

func (w widgetRequest) widget() domain.Widget {
	return domain.Widget{
		Title: w.Title, Question: w.Question, SQL: w.SQL,
		Query: w.Query, Chart: w.Chart, Width: w.Width,
	}
}

type createRequest struct {
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Widgets     []widgetRequest `json:"widgets"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := request.Decode(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	widgets := make([]domain.Widget, 0, len(req.Widgets))
	for _, wr := range req.Widgets {
		widgets = append(widgets, wr.widget())
	}
	dashboard, err := h.dashboards.Create(r.Context(), req.Title, req.Description, widgets)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusCreated, dashboard)
}

func (h *Handler) Rename(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	if err := request.Decode(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	if err := h.dashboards.Rename(r.Context(), id, req.Title); err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusNoContent, nil)
}

func (h *Handler) AddWidget(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	var req widgetRequest
	if err := request.Decode(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	widget, err := h.dashboards.AddWidget(r.Context(), id, req.widget())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusCreated, widget)
}

func (h *Handler) RemoveWidget(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	widgetID, err := request.UUID(r, "widget")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if err := h.dashboards.RemoveWidget(r.Context(), id, widgetID); err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusNoContent, nil)
}

type Dashboards interface {
	List(ctx context.Context, limit int) ([]domain.Dashboard, error)
	Get(ctx context.Context, id uuid.UUID, refresh bool, period domain.DateRange) (*domain.Dashboard, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Create(ctx context.Context, title, description string, widgets []domain.Widget) (*domain.Dashboard, error)
	Rename(ctx context.Context, id uuid.UUID, title string) error
	AddWidget(ctx context.Context, dashboardID uuid.UUID, w domain.Widget) (*domain.Widget, error)
	RemoveWidget(ctx context.Context, dashboardID, widgetID uuid.UUID) error
	Trash(ctx context.Context) ([]domain.TrashItem, error)
	RestoreDashboard(ctx context.Context, id uuid.UUID) error
	RestoreWidget(ctx context.Context, dashboardID, widgetID uuid.UUID) error
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/dashboards", func(d chi.Router) {
		d.Get("/", h.List)
		d.Post("/", h.Create)
		d.Get("/trash", h.Trash)
		d.Get("/{id}", h.Get)
		d.Get("/{id}/export", h.Export)
		d.Patch("/{id}", h.Rename)
		d.Delete("/{id}", h.Delete)
		d.Post("/{id}/widgets", h.AddWidget)
		d.Delete("/{id}/widgets/{widget}", h.RemoveWidget)
		d.Post("/{id}/restore", h.Restore)
		d.Post("/{id}/widgets/{widget}/restore", h.RestoreWidget)
	})
}
