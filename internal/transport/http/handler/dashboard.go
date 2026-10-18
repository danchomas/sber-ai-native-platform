package handler

import (
	"net/http"

	"github.com/heinousness/aibi/internal/core/transport/http/request"
	"github.com/heinousness/aibi/internal/core/transport/http/response"
)

type DashboardHandler struct {
	dashboards Dashboards
}

func NewDashboard(dashboards Dashboards) *DashboardHandler {
	return &DashboardHandler{dashboards: dashboards}
}

func (h *DashboardHandler) List(w http.ResponseWriter, r *http.Request) {
	dashboards, err := h.dashboards.List(r.Context(), request.Int(r, "limit", 50))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, map[string]any{"dashboards": dashboards})
}

func (h *DashboardHandler) Get(w http.ResponseWriter, r *http.Request) {
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

func (h *DashboardHandler) Delete(w http.ResponseWriter, r *http.Request) {
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
