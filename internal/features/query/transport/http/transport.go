package http

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/sqlguard"
	"github.com/heinousness/aibi/internal/core/transport/http/request"
	"github.com/heinousness/aibi/internal/core/transport/http/response"
)

type Handler struct {
	catalog Catalog
	exec    Executor
	cfg     config.Query
}

func New(catalog Catalog, exec Executor, cfg config.Query) *Handler {
	return &Handler{catalog: catalog, exec: exec, cfg: cfg}
}

type queryRequest struct {
	SQL     string `json:"sql"`
	MaxRows int    `json:"max_rows"`
}

type queryResponse struct {
	Result   *domain.ResultSet `json:"result"`
	Source   string            `json:"source,omitempty"`
	Tables   []string          `json:"tables,omitempty"`
	Warnings []string          `json:"warnings,omitempty"`
}

func (h *Handler) Run(w http.ResponseWriter, r *http.Request) {
	var req queryRequest
	if err := request.Decode(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	if strings.TrimSpace(req.SQL) == "" {
		response.Error(w, r, apperr.NewValidationError("sql", "запрос не может быть пустым"))
		return
	}

	scopes, err := h.catalog.Scopes(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}

	maxRows := req.MaxRows
	if maxRows <= 0 || maxRows > h.cfg.MaxRows {
		maxRows = h.cfg.MaxRows
	}

	scope, checked, err := sqlguard.ResolveScope(req.SQL, scopes, h.catalog.Schema(), maxRows)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	result, err := h.exec.Query(r.Context(), scope.Target, checked.SQL, maxRows)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, queryResponse{
		Result:   result,
		Source:   scope.SourceName,
		Tables:   checked.Tables,
		Warnings: checked.Warnings,
	})
}

type Catalog interface {
	Scopes(ctx context.Context) ([]domain.Scope, error)
	Schema() string
}

type Executor interface {
	Query(ctx context.Context, target domain.QueryTarget, sql string, maxRows int) (*domain.ResultSet, error)
}

func (h *Handler) Register(r chi.Router) {
	r.Post("/query", h.Run)
}
