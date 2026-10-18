package response

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/logger"
)

type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func JSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		logger.From(r.Context()).Error("ответ не сериализован", "error", err)
	}
}

func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func Error(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := classify(err)

	log := logger.From(r.Context())
	if status >= http.StatusInternalServerError {
		log.Error("ошибка обработки запроса", "error", err)
	} else {
		log.Warn("запрос отклонён", "error", err, "code", code)
	}

	body := errorBody{Error: errorPayload{Code: code, Message: message}}

	var vErr *apperr.ValidationError
	if errors.As(err, &vErr) {
		body.Error.Field = vErr.Field
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func classify(err error) (status int, code, message string) {
	switch {
	case errors.Is(err, apperr.ErrNotFound):
		return http.StatusNotFound, "not_found", "Объект не найден"
	case errors.Is(err, apperr.ErrAlreadyExists):
		return http.StatusConflict, "already_exists", "Объект уже существует"
	case errors.Is(err, apperr.ErrValidation):
		return http.StatusBadRequest, "validation", err.Error()
	case errors.Is(err, apperr.ErrUnsafeQuery):
		return http.StatusBadRequest, "unsafe_query", err.Error()
	case errors.Is(err, apperr.ErrDatasetNotReady):
		return http.StatusConflict, "dataset_not_ready", "Датасет ещё не готов к анализу"
	case errors.Is(err, apperr.ErrNoDatasets):
		return http.StatusConflict, "no_datasets", "В каталоге нет загруженных данных"
	case errors.Is(err, apperr.ErrQueryTimeout):
		return http.StatusGatewayTimeout, "query_timeout", "Запрос слишком тяжёлый и был остановлен по времени"
	case errors.Is(err, apperr.ErrLLMUnavailable):
		return http.StatusServiceUnavailable, "llm_unavailable", "Языковая модель недоступна"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "timeout", "Превышено время обработки запроса"
	case errors.Is(err, context.Canceled):
		return 499, "canceled", "Запрос отменён клиентом"
	default:
		return http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера"
	}
}
