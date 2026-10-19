package http

import (
	"net/http"

	"github.com/heinousness/aibi/internal/core/transport/http/request"
	"github.com/heinousness/aibi/internal/core/transport/http/response"
)

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	dataset, err := h.service.Get(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, dataset)
}

func (h *Handler) Profile(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	profile, err := h.service.Profile(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, profile)
}

func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	result, err := h.service.Preview(r.Context(), id, request.Int(r, "limit", 0))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, result)
}
