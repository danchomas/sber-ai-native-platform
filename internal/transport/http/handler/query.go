package handler

import (
	"net/http"
	"strings"

	"github.com/heinousness/aibi/internal/config"
	"github.com/heinousness/aibi/internal/domain"
	"github.com/heinousness/aibi/internal/sqlguard"
)

type QueryHandler struct {
	catalog Catalog
	exec    Executor
	cfg     config.Query
}

func NewQuery(catalog Catalog, exec Executor, cfg config.Query) *QueryHandler {
	return &QueryHandler{catalog: catalog, exec: exec, cfg: cfg}
}

type queryRequest struct {
	SQL     string `json:"sql"`
	MaxRows int    `json:"max_rows"`
}

type queryResponse struct {
	Result   *domain.ResultSet `json:"result"`
	Tables   []string          `json:"tables,omitempty"`
	Warnings []string          `json:"warnings,omitempty"`
}

func (h *QueryHandler) Run(w http.ResponseWriter, r *http.Request) {
	var req queryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if strings.TrimSpace(req.SQL) == "" {
		writeError(w, r, domain.NewValidationError("sql", "запрос не может быть пустым"))
		return
	}

	tables, err := h.catalog.Tables(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}

	maxRows := req.MaxRows
	if maxRows <= 0 || maxRows > h.cfg.MaxRows {
		maxRows = h.cfg.MaxRows
	}

	checked, err := sqlguard.Validate(req.SQL, sqlguard.NewPolicy(h.catalog.Schema(), tables, maxRows))
	if err != nil {
		writeError(w, r, err)
		return
	}

	result, err := h.exec.Query(r.Context(), checked.SQL, maxRows)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, queryResponse{
		Result:   result,
		Tables:   checked.Tables,
		Warnings: checked.Warnings,
	})
}
