package service

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/heinousness/aibi/internal/core/domain"
)

var errForeignScript = errors.New("в ответе модели посторонняя письменность")

func hasForeignScript(text string) bool {
	return strings.IndexFunc(text, func(r rune) bool {
		return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
	}) >= 0
}

func (s *Service) completeRussian(ctx context.Context, req domain.Completion) (string, error) {
	var text string
	for attempt := 0; attempt < 2; attempt++ {
		var err error
		text, err = s.models.Current(ctx).Complete(ctx, req)
		if err != nil {
			return "", err
		}
		if !hasForeignScript(text) {
			return text, nil
		}
		s.log.DebugContext(ctx, "в ответе модели иероглифы, запрос повторён", "attempt", attempt)
	}
	return "", errForeignScript
}
