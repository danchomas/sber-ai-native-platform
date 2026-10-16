package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/domain"
	"github.com/heinousness/aibi/internal/logger"
)

type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		logger.From(r.Context()).Error("ответ не сериализован", "error", err)
	}
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := classify(err)

	log := logger.From(r.Context())
	if status >= http.StatusInternalServerError {
		log.Error("ошибка обработки запроса", "error", err)
	} else {
		log.Warn("запрос отклонён", "error", err, "code", code)
	}

	body := errorBody{Error: errorPayload{Code: code, Message: message}}

	var vErr *domain.ValidationError
	if errors.As(err, &vErr) {
		body.Error.Field = vErr.Field
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func classify(err error) (status int, code, message string) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, "not_found", "Объект не найден"
	case errors.Is(err, domain.ErrAlreadyExists):
		return http.StatusConflict, "already_exists", "Объект уже существует"
	case errors.Is(err, domain.ErrValidation):
		return http.StatusBadRequest, "validation", err.Error()
	case errors.Is(err, domain.ErrUnsafeQuery):
		return http.StatusBadRequest, "unsafe_query", err.Error()
	case errors.Is(err, domain.ErrDatasetNotReady):
		return http.StatusConflict, "dataset_not_ready", "Датасет ещё не готов к анализу"
	case errors.Is(err, domain.ErrNoDatasets):
		return http.StatusConflict, "no_datasets", "В каталоге нет загруженных данных"
	case errors.Is(err, domain.ErrQueryTimeout):
		return http.StatusGatewayTimeout, "query_timeout", "Запрос слишком тяжёлый и был остановлен по времени"
	case errors.Is(err, domain.ErrLLMUnavailable):
		return http.StatusServiceUnavailable, "llm_unavailable", "Языковая модель недоступна"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "timeout", "Превышено время обработки запроса"
	case errors.Is(err, context.Canceled):
		return 499, "canceled", "Запрос отменён клиентом"
	default:
		return http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера"
	}
}

func pathUUID(r *http.Request, param string) (uuid.UUID, error) {
	raw := chi.URLParam(r, param)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, domain.NewValidationError(param, "ожидается идентификатор в формате UUID")
	}
	return id, nil
}

func queryInt(r *http.Request, name string, def int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func queryBool(r *http.Request, name string, def bool) bool {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return b
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return domain.NewValidationError("body", "некорректный JSON: "+err.Error())
	}
	return nil
}
