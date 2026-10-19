package http

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/features/datasets/service"
)

type Service interface {
	Upload(ctx context.Context, req service.Request) (*service.Report, error)
	List(ctx context.Context) ([]domain.Dataset, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.Dataset, error)
	Profile(ctx context.Context, id uuid.UUID) (*domain.Profile, error)
	Preview(ctx context.Context, id uuid.UUID, limit int) (*domain.ResultSet, error)
	Relations(ctx context.Context) ([]domain.Relation, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type Handler struct {
	service     Service
	uploadLimit func(http.Handler) http.Handler
}

func New(svc Service, uploadLimit func(http.Handler) http.Handler) *Handler {
	return &Handler{service: svc, uploadLimit: uploadLimit}
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/datasets", func(ds chi.Router) {
		ds.Get("/", h.List)
		ds.With(h.uploadLimit).Post("/", h.Upload)
		ds.Get("/{id}", h.Get)
		ds.Delete("/{id}", h.Delete)
		ds.Get("/{id}/profile", h.Profile)
		ds.Get("/{id}/preview", h.Preview)
	})
	r.Get("/catalog/relations", h.Relations)
}
