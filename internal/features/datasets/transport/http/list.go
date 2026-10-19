package http

import (
	"net/http"

	"github.com/heinousness/aibi/internal/core/transport/http/response"
)

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	datasets, err := h.service.List(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, map[string]any{"datasets": datasets})
}

func (h *Handler) Relations(w http.ResponseWriter, r *http.Request) {
	relations, err := h.service.Relations(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, map[string]any{"relations": relations})
}
