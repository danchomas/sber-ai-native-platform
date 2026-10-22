package http

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/llm"
	"github.com/heinousness/aibi/internal/core/transport/http/request"
	"github.com/heinousness/aibi/internal/core/transport/http/response"
)

type Providers interface {
	Attach(ctx context.Context, p domain.Provider) (*domain.Provider, error)
	List(ctx context.Context) ([]domain.Provider, error)
	Models(ctx context.Context, id uuid.UUID) ([]llm.CatalogEntry, error)
	Activate(ctx context.Context, id uuid.UUID, model string) (*domain.Provider, error)
	Deactivate(ctx context.Context) error
	Delete(ctx context.Context, id uuid.UUID) error
	Fallback() string
}

type Handler struct {
	providers Providers
}

func New(p Providers) *Handler { return &Handler{providers: p} }

func (h *Handler) Register(r chi.Router) {
	r.Route("/providers", func(p chi.Router) {
		p.Get("/", h.List)
		p.Post("/", h.Attach)
		p.Delete("/active", h.Deactivate)
		p.Get("/{id}/models", h.Models)
		p.Post("/{id}/activate", h.Activate)
		p.Delete("/{id}", h.Delete)
	})
}

type attachRequest struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

type activateRequest struct {
	Model string `json:"model"`
}

type providerView struct {
	domain.Provider
	HasKey bool `json:"has_key"`
}

func view(p *domain.Provider) providerView {
	return providerView{Provider: *p, HasKey: p.HasKey()}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.providers.List(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}

	out := make([]providerView, 0, len(items))
	for i := range items {
		out = append(out, view(&items[i]))
	}
	response.JSON(w, r, http.StatusOK, map[string]any{
		"providers":      out,
		"fallback_model": h.providers.Fallback(),
	})
}

func (h *Handler) Attach(w http.ResponseWriter, r *http.Request) {
	var req attachRequest
	if err := request.Decode(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	p, err := h.providers.Attach(r.Context(), domain.Provider{
		Name:    req.Name,
		Kind:    domain.ProviderKind(req.Kind),
		BaseURL: req.BaseURL,
		APIKey:  req.APIKey,
		Model:   req.Model,
	})
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusCreated, view(p))
}

func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	models, err := h.providers.Models(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if models == nil {
		models = []llm.CatalogEntry{}
	}
	response.JSON(w, r, http.StatusOK, map[string]any{"models": models})
}

func (h *Handler) Activate(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}

	var req activateRequest
	if r.ContentLength > 0 {
		if err := request.Decode(r, &req); err != nil {
			response.Error(w, r, err)
			return
		}
	}

	p, err := h.providers.Activate(r.Context(), id, req.Model)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, view(p))
}

func (h *Handler) Deactivate(w http.ResponseWriter, r *http.Request) {
	if err := h.providers.Deactivate(r.Context()); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if err := h.providers.Delete(r.Context(), id); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
