package handler

import (
	"net/http"

	"github.com/heinousness/aibi/internal/domain"
)

type SourceHandler struct {
	sources Sources
}

func NewSource(sources Sources) *SourceHandler {
	return &SourceHandler{sources: sources}
}

type attachSourceRequest struct {
	Name string `json:"name"`
	DSN  string `json:"dsn"`
}

type attachTableRequest struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	Name   string `json:"name"`
}

func (h *SourceHandler) List(w http.ResponseWriter, r *http.Request) {
	sources, err := h.sources.List(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	if sources == nil {
		sources = []domain.Source{}
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"sources": sources})
}

func (h *SourceHandler) Attach(w http.ResponseWriter, r *http.Request) {
	var req attachSourceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	src, err := h.sources.Attach(r.Context(), req.Name, req.DSN)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, src)
}

func (h *SourceHandler) Tables(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	tables, err := h.sources.Tables(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if tables == nil {
		tables = []domain.ExternalTable{}
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"tables": tables})
}

func (h *SourceHandler) AttachTable(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var req attachTableRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	dataset, err := h.sources.AttachTable(r.Context(), id, req.Schema, req.Table, req.Name)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, dataset)
}

func (h *SourceHandler) Detach(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.sources.Detach(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
