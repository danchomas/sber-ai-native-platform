package llm

import (
	"context"

	"github.com/heinousness/aibi/internal/domain"
)

type Fallback struct{}

func NewFallback() *Fallback { return &Fallback{} }

var _ domain.LLM = (*Fallback)(nil)

func (f *Fallback) Model() string { return "fallback" }

func (f *Fallback) Complete(context.Context, domain.Completion) (string, error) {
	return `{"intent":"overview","explanation":"Языковая модель недоступна, показываю структуру данных по профилю каталога."}`, nil
}

func (f *Fallback) Ping(context.Context) error { return nil }
