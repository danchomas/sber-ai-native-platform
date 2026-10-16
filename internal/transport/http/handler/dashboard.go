package handler

import "net/http"

type DashboardHandler struct {
	dashboards Dashboards
}

func NewDashboard(dashboards Dashboards) *DashboardHandler {
	return &DashboardHandler{dashboards: dashboards}
}

func (h *DashboardHandler) List(w http.ResponseWriter, r *http.Request) {
	dashboards, err := h.dashboards.List(r.Context(), queryInt(r, "limit", 50))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"dashboards": dashboards})
}

func (h *DashboardHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	dashboard, err := h.dashboards.Get(r.Context(), id, queryBool(r, "refresh", true))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, dashboard)
}

func (h *DashboardHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.dashboards.Delete(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusNoContent, nil)
}
