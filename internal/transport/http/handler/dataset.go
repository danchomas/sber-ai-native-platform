package handler

import (
	"net/http"
	"strings"

	"github.com/heinousness/aibi/internal/domain"
	"github.com/heinousness/aibi/internal/service/ingest"
)

type Dataset struct {
	ingest  Ingester
	catalog Catalog
}

func NewDataset(ingester Ingester, catalog Catalog) *Dataset {
	return &Dataset{ingest: ingester, catalog: catalog}
}

func (h *Dataset) List(w http.ResponseWriter, r *http.Request) {
	datasets, err := h.catalog.List(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"datasets": datasets})
}

func (h *Dataset) Get(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	dataset, err := h.catalog.Get(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, dataset)
}

func (h *Dataset) Profile(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	profile, err := h.catalog.Profile(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, profile)
}

func (h *Dataset) Preview(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	result, err := h.catalog.Preview(r.Context(), id, queryInt(r, "limit", 0))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}

func (h *Dataset) Upload(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, domain.NewValidationError("file", "ожидается файл в поле file"))
		return
	}
	defer func() { _ = file.Close() }()

	filename := header.Filename
	if ext := strings.ToLower(filename); !strings.HasSuffix(ext, ".csv") &&
		!strings.HasSuffix(ext, ".tsv") && !strings.HasSuffix(ext, ".txt") {
		writeError(w, r, domain.NewValidationError("file", "поддерживаются файлы CSV/TSV"))
		return
	}

	report, err := h.ingest.Upload(r.Context(), ingest.Request{
		Name:     r.FormValue("name"),
		Filename: filename,
		Reader:   file,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, report)
}

func (h *Dataset) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.ingest.Delete(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusNoContent, nil)
}

func (h *Dataset) Relations(w http.ResponseWriter, r *http.Request) {
	relations, err := h.catalog.Relations(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"relations": relations})
}
