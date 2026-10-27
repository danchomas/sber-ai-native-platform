package service

import (
	"slices"

	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/promptguard"
)

const maxSuggestions = 3

// suggestQuestions — продолжения разговора по каталогу, без модели:
// пользователь всегда видит, что спросить дальше.
func suggestQuestions(datasets []domain.Dataset) []string {
	var out []string
	add := func(q string) {
		if len(out) < maxSuggestions {
			out = append(out, q)
		}
	}
	for _, d := range datasets {
		if !d.Ready() {
			continue
		}
		if dims := d.ColumnsByRole(domain.RoleDimension); len(dims) > 0 {
			add("Какие значения «" + promptName(dims[0]) + "» встречаются чаще всего?")
		}
		if len(d.ColumnsByRole(domain.RoleTemporal)) > 0 {
			add("Покажи динамику по месяцам")
		}
		add("Есть ли в данных дубликаты?")
		break
	}
	if len(out) == 0 {
		return slices.Clone(defaultQuestions)
	}
	return out
}

// promptName — понятное имя колонки для подсказки пользователю.
// Подсказка станет вопросом от его имени, поэтому заголовок, похожий
// на инструкцию, заменяется техническим именем.
func promptName(c domain.Column) string {
	name, hidden := promptguard.Data(label(c), 40)
	if hidden {
		return c.Name
	}
	return name
}
