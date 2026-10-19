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
	sources Sources
}

func New(sources Sources) *Handler {
	return &Handler{sources: sources}
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/sources", func(src chi.Router) {
		src.Get("/", h.List)
		src.Post("/", h.Attach)
		src.Delete("/{id}", h.Detach)
		src.Get("/{id}/tables", h.Tables)
		src.Post("/{id}/tables", h.AttachTable)
	})
}

type Sources interface {
	Attach(ctx context.Context, name, dsn string) (*domain.Source, error)
	List(ctx context.Context) ([]domain.Source, error)
	Tables(ctx context.Context, id uuid.UUID) ([]domain.ExternalTable, error)
	AttachTable(ctx context.Context, id uuid.UUID, schema, table, name string) (*domain.Dataset, error)
	Detach(ctx context.Context, id uuid.UUID) error
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

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	sources, err := h.sources.List(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if sources == nil {
		sources = []domain.Source{}
	}
	response.JSON(w, r, http.StatusOK, map[string]any{"sources": sources})
}

func (h *Handler) Attach(w http.ResponseWriter, r *http.Request) {
	var req attachSourceRequest
	if err := request.Decode(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	src, err := h.sources.Attach(r.Context(), req.Name, req.DSN)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusCreated, src)
}

func (h *Handler) Tables(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	tables, err := h.sources.Tables(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if tables == nil {
		tables = []domain.ExternalTable{}
	}
	response.JSON(w, r, http.StatusOK, map[string]any{"tables": tables})
}

func (h *Handler) AttachTable(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	var req attachTableRequest
	if err := request.Decode(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	dataset, err := h.sources.AttachTable(r.Context(), id, req.Schema, req.Table, req.Name)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusCreated, dataset)
}

func (h *Handler) Detach(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if err := h.sources.Detach(r.Context(), id); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
