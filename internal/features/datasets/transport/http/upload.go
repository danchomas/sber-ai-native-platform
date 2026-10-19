package http

import (
	"net/http"
	"strings"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/transport/http/response"
	"github.com/heinousness/aibi/internal/features/datasets/service"
)

func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")
	if err != nil {
		response.Error(w, r, apperr.NewValidationError("file", "ожидается файл в поле file"))
		return
	}
	defer func() { _ = file.Close() }()

	filename := header.Filename
	if ext := strings.ToLower(filename); !strings.HasSuffix(ext, ".csv") &&
		!strings.HasSuffix(ext, ".tsv") && !strings.HasSuffix(ext, ".txt") {
		response.Error(w, r, apperr.NewValidationError("file", "поддерживаются файлы CSV/TSV"))
		return
	}

	report, err := h.service.Upload(r.Context(), service.Request{
		Name:     r.FormValue("name"),
		Filename: filename,
		Reader:   file,
	})
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusCreated, report)
}
