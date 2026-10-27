package service

import (
	"context"
	"slices"
	"strings"

	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/promptguard"
)

// Детерминированный слой защиты вокруг модели. Главная гарантия — у модели
// есть только инструменты чтения, — от него не зависит; этот слой делает
// поведение предсказуемым там, где малая модель могла бы растеряться:
// явная атака на инструкции и просьба изменить данные отклоняются до
// обращения к модели, а ответ проверяется на утечку промпта и чистится
// от ссылок и разметки.

var kindNames = map[promptguard.Kind]string{
	promptguard.KindOverride:    "попытка отменить правила ассистента",
	promptguard.KindPromptLeak:  "запрос служебных инструкций",
	promptguard.KindRoleplay:    "попытка сменить роль ассистента",
	promptguard.KindTokens:      "служебная разметка языковой модели",
	promptguard.KindDestructive: "просьба изменить данные",
}

// screen отвечает без модели на вопросы, которые платформа выполнять
// не должна. nil — вопрос можно передавать агенту.
func (s *Service) screen(ctx context.Context, question string) *domain.Answer {
	if findings := promptguard.Inspect(question); len(findings) > 0 {
		f := findings[0]
		s.log.WarnContext(ctx, "вопрос отклонён фильтром инъекций", "kind", f.Kind, "fragment", f.Fragment)
		return &domain.Answer{
			Intent: domain.IntentReject,
			Text: "Я отвечаю на вопросы по вашим данным и работаю по фиксированным правилам платформы: " +
				"изменить их, сменить роль или раскрыть служебные инструкции нельзя. " +
				"Спросите о данных — например, о показателях, динамике или распределении.",
			Followups: slices.Clone(defaultQuestions),
			Security: []string{"Вопрос похож на попытку управлять ассистентом (" + kindNames[f.Kind] +
				"), поэтому он не передавался языковой модели."},
			Blocked: true,
		}
	}
	if f, ok := promptguard.Destructive(question); ok {
		s.log.WarnContext(ctx, "вопрос отклонён: просьба изменить данные", "fragment", f.Fragment)
		return &domain.Answer{
			Intent: domain.IntentReject,
			Text: "Платформа только читает данные: изменить, удалить или выгрузить их из чата нельзя. " +
				"У ассистента нет таких инструментов, а база открыта ему только на чтение. " +
				"Загруженный набор целиком можно удалить на вкладке «Данные». " +
				"Могу посчитать, сколько таких записей, или показать их.",
			Followups: slices.Clone(defaultQuestions),
			Security:  []string{"Просьба изменить данные отклонена до обращения к языковой модели."},
			Blocked:   true,
		}
	}
	return nil
}

var defaultQuestions = []string{
	"Что есть в этих данных?",
	"Какие значения встречаются чаще всего?",
	"Есть ли в данных дубликаты?",
}

// secure проверяет ответ перед показом: утечка системного промпта
// заменяет ответ отказом, ссылки и разметка вырезаются.
func (s *Service) secure(ctx context.Context, a *domain.Answer, canary promptguard.Canary) {
	// Ответ, который дал сам слой защиты, написан платформой, а не моделью:
	// его фразы о правилах совпадают с промптом, но утечкой не являются.
	if a.Blocked {
		return
	}
	texts := append([]string{a.Text}, a.Followups...)
	if a.Chart != nil {
		texts = append(texts, a.Chart.Title)
	}
	for _, t := range texts {
		if promptguard.Leaks(t, canary, promptMarkers...) || promptEcho.Repeats(t) {
			s.log.WarnContext(ctx, "ответ модели раскрывал системный промпт и заменён")
			*a = domain.Answer{
				Intent: domain.IntentReject,
				Text: "Не могу показать этот ответ: он раскрывал служебные инструкции ассистента. " +
					"Задайте вопрос о данных.",
				Followups: slices.Clone(defaultQuestions),
				Steps:     a.Steps,
				Security:  append(a.Security, "Ответ модели содержал служебные инструкции и был заменён."),
				Blocked:   true,
			}
			return
		}
	}

	cleaned := promptguard.CleanOutput(a.Text)
	changed := cleaned != strings.TrimSpace(a.Text)
	a.Text = cleaned

	// Новый срез, а не a.Followups[:0]: продолжения могут ссылаться на общий
	// defaultQuestions, и фильтр на месте испортил бы его для всех вопросов.
	followups := make([]string, 0, len(a.Followups))
	for _, f := range a.Followups {
		f = promptguard.CleanOutput(promptguard.Neutralize(f, 120))
		// Продолжение — это кнопка, которая отправит вопрос от имени
		// пользователя. Похожее на инъекцию продолжение не показываем.
		if f == "" || promptguard.IsInjection(f) {
			changed = true
			continue
		}
		followups = append(followups, f)
	}
	a.Followups = followups
	if a.Chart != nil {
		a.Chart.Title = promptguard.CleanOutput(a.Chart.Title)
	}
	if changed {
		s.log.InfoContext(ctx, "из ответа убраны ссылки или разметка")
		a.Security = append(a.Security, "Из ответа убраны ссылки или разметка: платформа не показывает их в ответах о данных.")
	}
}
