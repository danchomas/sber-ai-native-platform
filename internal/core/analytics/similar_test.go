package analytics

import (
	"strings"
	"testing"

	"github.com/heinousness/aibi/internal/core/domain"
)

func TestSimilarColumns(t *testing.T) {
	d := domain.Dataset{Name: "События", Columns: []domain.Column{
		{Name: "error_text", SourceName: "Текст ошибки"},
		{Name: "error_code", SourceName: "Код ошибки"},
		{Name: "platform", SourceName: "Платформа"},
		{Name: "planned_hours", SourceName: "Плановые трудозатраты, ч"},
	}}
	cases := []struct {
		ref  string
		want string
	}{
		{"текст_ошибки", "Текст ошибки"},
		{"плановые трудозатраты часов", "Плановые трудозатраты, ч"},
		{"платформ", "Платформа"},
	}
	for _, tc := range cases {
		got := SimilarColumns(d, tc.ref)
		if len(got) == 0 || got[0] != tc.want {
			t.Errorf("%q: первой должна быть «%s», получено %v", tc.ref, tc.want, got)
		}
	}
	if got := SimilarColumns(d, "выручка"); len(got) != 0 {
		t.Errorf("непохожие колонки не подсказываются: %v", got)
	}
}

func TestCompileSuggestsSimilarColumn(t *testing.T) {
	_, err := Compile(Query{Dataset: "orders", GroupBy: []string{"статус_заказа"}}, testCatalog(), 100)
	if err == nil || !strings.Contains(err.Error(), "похожие: «Статус»") {
		t.Fatalf("ошибка должна подсказать похожую колонку: %v", err)
	}
}

func TestFindColumnLoose(t *testing.T) {
	d := testCatalog().Datasets[0]
	if col, ok := FindColumn(d, "дата заказа"); !ok || col.Name != "created_at" {
		t.Fatalf("колонка находится по заголовку без регистра: %+v", col)
	}
	if _, ok := FindColumn(d, "нет такой"); ok {
		t.Fatal("несуществующая колонка не находится")
	}
}

func TestSimilarColumnsByWordPrefix(t *testing.T) {
	d := domain.Dataset{Name: "Задачи", Columns: []domain.Column{
		{Name: "plan_hours", SourceName: "Плановые трудозатраты, ч"},
		{Name: "fact_hours", SourceName: "Фактические трудозатраты, ч"},
		{Name: "team", SourceName: "Команда"},
	}}
	if got := SimilarColumns(d, "Факт, ч"); len(got) == 0 || got[0] != "Фактические трудозатраты, ч" {
		t.Fatalf("сокращение слова находит полное название: %v", got)
	}
}

func TestCalendarPartIsNotAColumn(t *testing.T) {
	_, err := Compile(Query{Dataset: "orders", GroupBy: []string{"Месяц года («Дата заказа»)"}}, testCatalog(), 100)
	if err == nil || !strings.Contains(err.Error(), "grain month_of_year") {
		t.Fatalf("ошибка подсказывает календарную разбивку: %v", err)
	}
}
