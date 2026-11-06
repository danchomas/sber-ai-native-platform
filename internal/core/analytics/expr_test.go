package analytics

import (
	"errors"
	"strings"
	"testing"

	"github.com/heinousness/aibi/internal/core/apperr"
)

func TestCompileExprMetrics(t *testing.T) {
	const amount = `t0."amount"::numeric`
	cases := []struct {
		name   string
		metric Metric
		want   string
	}{
		{"произведение со скидкой",
			Metric{Func: FuncSum, Expr: "amount * order_id * (1 - customer_id / 100)", Label: "Выручка"},
			`round(sum(((` + amount + ` * t0."order_id"::numeric) * (1 - (t0."customer_id"::numeric / NULLIF(100, 0))))), 2) AS "Выручка"`},
		{"заголовок из файла в кавычках",
			Metric{Func: FuncAvg, Expr: `"Сумма" * 2`, Label: "Двойная"}, `round(avg((` + amount + ` * 2)), 2) AS "Двойная"`},
		{"заголовок в ёлочках", Metric{Func: FuncMax, Expr: "«Сумма»", Label: "М"}, `round(max(` + amount + `), 2) AS "М"`},
		{"дробное число с запятой", Metric{Func: FuncSum, Expr: "amount * 0,5", Label: "П"}, amount + ` * 0.5`},
		{"унарный минус", Metric{Func: FuncSum, Expr: "-amount - -1", Label: "М"}, `((-` + amount + `) - (-1))`},
		{"доля суммы", Metric{Func: FuncShare, Expr: "amount", Label: "Доля"},
			`round(100.0 * sum(` + amount + `) / NULLIF(sum(sum(` + amount + `)) OVER (), 0), 2) AS "Доля"`},
		{"доля числовой колонки", Metric{Func: FuncShare, Column: "amount"}, `sum(sum(` + amount + `)) OVER ()`},
		{"доля строк без колонки", Metric{Func: FuncShare}, `sum(count(*)) OVER ()`},
		{"сумма на заказ", Metric{Func: FuncSum, Expr: "amount", Per: "order_id", Label: "Чек"},
			`round((round(sum(` + amount + `), 2))::numeric / NULLIF(count(DISTINCT t0."order_id"), 0), 2) AS "Чек"`},
		{"заказов на клиента", Metric{Func: FuncCountDistinct, Column: "order_id", Per: "customer_id", Label: "З"},
			`NULLIF(count(DISTINCT t0."customer_id"), 0)`},
		{"масштаб колонки", Metric{Func: FuncSum, Expr: "amount * 2", Label: "С", Scale: &Scale{Column: "Сумма", Factor: 1.1}},
			`(` + amount + ` * 1.1)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat := testCatalog()
			got, err := Compile(Query{Dataset: "orders", GroupBy: []string{"status"}, Metrics: []Metric{tc.metric}}, cat, 1000)
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if !strings.Contains(got.SQL, tc.want) {
				t.Fatalf("в запросе нет %q:\n%s", tc.want, got.SQL)
			}
			mustPassGuard(t, got, cat)
		})
	}
}

func TestCompileExprRejects(t *testing.T) {
	cases := []struct {
		name   string
		metric Metric
		wantIn string
	}{
		{"вызов функции", Metric{Func: FuncSum, Expr: "pg_sleep(10)"}, "нет в датасете"},
		{"подзапрос", Metric{Func: FuncSum, Expr: "(SELECT 1 FROM pg_authid)"}, "нет в датасете"},
		{"точка с запятой", Metric{Func: FuncSum, Expr: "amount; DROP TABLE x"}, "допустимы числовые колонки"},
		{"комментарий", Metric{Func: FuncSum, Expr: "amount /* x */"}, ""},
		{"строка", Metric{Func: FuncSum, Expr: "amount || 'x'"}, "допустимы числовые колонки"},
		{"кавычка в кавычках", Metric{Func: FuncSum, Expr: `"amount"" FROM pg_authid --"`}, "лишнее в конце"},
		{"приведение типа", Metric{Func: FuncSum, Expr: "amount::text"}, "допустимы числовые колонки"},
		{"степень", Metric{Func: FuncSum, Expr: "amount ^ 2"}, "допустимы числовые колонки"},
		{"неизвестная колонка", Metric{Func: FuncSum, Expr: "price * 2"}, "нет в датасете"},
		{"текстовая колонка", Metric{Func: FuncSum, Expr: "status * 2"}, "не числовая"},
		{"дата плюс число", Metric{Func: FuncSum, Expr: "created_at + 1"}, "только разность двух колонок"},
		{"дата как показатель", Metric{Func: FuncAvg, Expr: "created_at"}, "дата не может быть показателем"},
		{"сумма дат", Metric{Func: FuncAvg, Expr: "created_at + shipped_at"}, "только разность двух колонок"},
		{"дата умножена", Metric{Func: FuncAvg, Expr: "(shipped_at - created_at) * created_at"}, "только разность двух колонок"},
		{"минус дата", Metric{Func: FuncAvg, Expr: "-created_at"}, "только разность двух колонок"},
		{"лишнее в конце", Metric{Func: FuncSum, Expr: "amount amount"}, "лишнее в конце"},
		{"незакрытая скобка", Metric{Func: FuncSum, Expr: "(amount"}, "не закрыта скобка"},
		{"незакрытая кавычка", Metric{Func: FuncSum, Expr: `"amount`}, "не закрыта кавычка"},
		{"оборванная формула", Metric{Func: FuncSum, Expr: "amount *"}, "обрывается"},
		{"слишком длинная", Metric{Func: FuncSum, Expr: strings.Repeat("amount + ", 40) + "1"}, "длиннее"},
		{"глубокие скобки", Metric{Func: FuncSum, Expr: strings.Repeat("(", 12) + "1" + strings.Repeat(")", 12)}, "уровней скобок"},
		{"неподходящая функция", Metric{Func: FuncCountDistinct, Expr: "amount"}, "к формуле применимы"},
		{"масштаб не той колонки", Metric{Func: FuncSum, Expr: "amount", Scale: &Scale{Column: "order_id", Factor: 2}}, "не зависит от колонки"},
		{"per к среднему", Metric{Func: FuncAvg, Column: "amount", Per: "order_id"}, "per применяется"},
		{"per неизвестная колонка", Metric{Func: FuncSum, Column: "amount", Per: "pg_authid"}, "нет в датасете"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(Query{Dataset: "orders", GroupBy: []string{"status"}, Metrics: []Metric{tc.metric}}, lifecycleCatalog(), 1000)
			if !errors.Is(err, apperr.ErrValidation) {
				t.Fatalf("ожидалась ошибка проверки, получено %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("ошибка %q не содержит %q", err, tc.wantIn)
			}
		})
	}
}

func TestShareNeedsGroupWithExpr(t *testing.T) {
	_, err := Compile(Query{Dataset: "orders", Metrics: []Metric{{Func: FuncShare, Expr: "amount"}}}, testCatalog(), 1000)
	if !errors.Is(err, apperr.ErrValidation) {
		t.Fatalf("ожидалась ошибка проверки, получено %v", err)
	}
}

func TestCompileSubtotals(t *testing.T) {
	cat := testCatalog()
	got, err := Compile(Query{
		Dataset: "orders", GroupBy: []string{"status", "is_paid"}, Subtotals: true,
		Metrics: []Metric{{Func: FuncCount, Label: "Записей"}, {Func: FuncShare, Label: "Доля"}, {Func: FuncShare, Expr: "amount", Label: "Доля суммы"}},
	}, cat, 1000)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	for _, want := range []string{
		`GROUP BY GROUPING SETS ((t0."status", t0."is_paid"), (t0."status"), ())`,
		`CASE WHEN GROUPING(t0."status") = 1 THEN 'Итого' ELSE t0."status"::text END`,
		`CASE WHEN GROUPING(t0."is_paid") = 1 THEN 'Все' ELSE t0."is_paid"::text END`,
		`ORDER BY GROUPING(t0."status"), t0."status", GROUPING(t0."is_paid"), t0."is_paid"`,
		`max(count(*)) OVER ()`,
		`max(sum(t0."amount"::numeric)) OVER ()`,
	} {
		if !strings.Contains(got.SQL, want) {
			t.Errorf("в запросе нет %q:\n%s", want, got.SQL)
		}
	}
	mustPassGuard(t, got, cat)
}
