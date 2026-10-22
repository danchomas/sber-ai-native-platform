package apperr

import "errors"

var (
	ErrNotFound = errors.New("объект не найден")

	ErrAlreadyExists = errors.New("объект уже существует")

	ErrValidation = errors.New("некорректные данные")

	ErrUnsafeQuery = errors.New("запрос отклонён политикой безопасности")

	ErrDatasetNotReady = errors.New("датасет не готов к анализу")

	ErrNoDatasets = errors.New("в каталоге нет загруженных данных")

	ErrLLMUnavailable = errors.New("языковая модель недоступна")

	ErrQueryTimeout = errors.New("превышено время выполнения запроса")
)

type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Reason
	}
	return e.Field + ": " + e.Reason
}

func (e *ValidationError) Unwrap() error { return ErrValidation }

func NewValidationError(field, reason string) error {
	return &ValidationError{Field: field, Reason: reason}
}
