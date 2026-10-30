package service

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/heinousness/aibi/internal/core/domain"
)

// errForeignScript — модель вставила в русский ответ иероглифы. Qwen
// изредка так делает («…покупках.紧随其后 идут скидки…»), и одной
// инструкции в промпте против этого мало.
var errForeignScript = errors.New("в ответе модели посторонняя письменность")

// hasForeignScript находит письменности, которых в русском ответе
// о данных быть не может: китайскую, японскую, корейскую.
func hasForeignScript(text string) bool {
	return strings.IndexFunc(text, func(r rune) bool {
		return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
	}) >= 0
}

// completeRussian запрашивает текст для пользователя и один раз
// переспрашивает, если в нём оказались иероглифы. Не помогло — ошибка:
// вызывающий отдаст детерминированный текст вместо испорченного.
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
