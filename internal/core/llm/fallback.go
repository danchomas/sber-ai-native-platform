package llm

import (
	"context"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
)

type Fallback struct{}

func NewFallback() *Fallback { return &Fallback{} }

var _ domain.LLM = (*Fallback)(nil)

func (f *Fallback) Model() string { return "fallback" }

func (f *Fallback) Complete(_ context.Context, req domain.Completion) (string, error) {
	if !req.JSONOnly {
		return "", apperr.ErrLLMUnavailable
	}
	return `{"intent":"overview","explanation":"Языковая модель недоступна, показываю структуру данных по профилю каталога."}`, nil
}

func (f *Fallback) Ping(context.Context) error { return nil }
