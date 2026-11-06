package service

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/heinousness/aibi/internal/core/analytics"
	"github.com/heinousness/aibi/internal/core/domain"
)

const (
	toolDescribe   = "describe_dataset"
	toolAggregate  = "aggregate"
	toolRows       = "sample_rows"
	toolDuplicates = "find_duplicates"
	toolSQL        = "run_sql"
	toolDashboard  = "create_dashboard"
	toolDashboards = "list_dashboards"
	toolAddWidgets = "add_to_dashboard"
	toolRemove     = "remove_widget"
	toolAnswer     = "answer"
	toolClarify    = "clarify"
	toolDecline    = "decline"
)

type schema = map[string]any

func object(props schema, required ...string) schema {
	s := schema{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func text(desc string) schema { return schema{"type": "string", "description": desc} }

func list(items schema, desc string) schema {
	return schema{"type": "array", "items": items, "description": desc}
}

func enum[T ~string](values []T, desc string) schema {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return schema{"type": "string", "enum": out, "description": desc}
}

var chartTypes = []domain.ChartType{
	domain.ChartTypeKPI, domain.ChartTypeLine, domain.ChartTypeArea, domain.ChartTypeBar,
	domain.ChartTypeHBar, domain.ChartTypePie, domain.ChartTypeDonut, domain.ChartTypeScatter,
	domain.ChartTypeHistogram, domain.ChartTypeHeatmap, domain.ChartTypeTable,
}

func chartSchema() schema {
	return object(schema{
		"type":   enum(chartTypes, "kpi — одно число, line — динамика, bar — сравнение категорий, hbar — длинные подписи, pie/donut — доля целого при 2-7 категориях"),
		"title":  text("заголовок графика"),
		"x":      text("колонка результата для оси X: категория или время"),
		"y":      list(schema{"type": "string"}, "колонки результата с метриками"),
		"series": text("колонка результата для разбивки на серии, если нужна"),
	}, "type")
}

func metricSchema(fnDesc string) schema {
	return object(schema{
		"fn":     enum(analytics.Funcs, fnDesc),
		"column": text("колонка датасета; для count можно не указывать"),
		"expr": text("формула вместо column, если показателя нет отдельной колонкой: числовые колонки, числа, + - * / и скобки, " +
			"например количество * цена * (1 - скидка / 100)"),
		"per":   text("колонка-знаменатель: показатель на одно её уникальное значение, например сумма на один заказ"),
		"label": text("подпись показателя по-русски"),
	}, "fn")
}

func filtersSchema() schema {
	return list(object(schema{
		"column": text("колонка датасета"),
		"op":     enum(analytics.Ops, "оператор сравнения"),
		"value":  schema{"description": "значение: число, строка или дата ГГГГ-ММ-ДД"},
		"values": schema{"type": "array", "description": "список значений для in, not_in и [начало, конец] для between"},
	}, "column", "op"), "условия отбора строк, объединяются через И")
}

func sortSchema() schema {
	return object(schema{
		"by":   text("колонка результата, по которой сортировать"),
		"desc": schema{"type": "boolean", "description": "по убыванию"},
	}, "by")
}

func widgetsSchema() schema {
	return list(object(schema{
		"result_id": text("result_id из aggregate"),
		"title":     text("заголовок виджета"),
		"chart":     chartSchema(),
		"width":     schema{"type": "integer", "description": "ширина из 12 колонок: 3 — число, 6 — половина, 12 — во всю ширину"},
	}, "result_id"), "виджеты")
}

type toolAccess struct {
	createDashboards bool
	editDashboards   bool
	removeWidgets    bool
}

var (
	dashboardIntent = regexp.MustCompile(`дашборд|панел|виджет|отч[её]т|сводк|закреп|dashboard|widget|report`)
	editIntent      = regexp.MustCompile(`дашборд|панел|виджет|закреп|dashboard|widget`)
	removeIntent    = regexp.MustCompile(`убер|убра|удал|сними|снять|снеси|замени|убрать|очисти|remove|delete|replace`)
)

func accessFor(question string) toolAccess {
	q := strings.ToLower(question)
	edit := editIntent.MatchString(q)
	return toolAccess{
		createDashboards: dashboardIntent.MatchString(q),
		editDashboards:   edit,
		removeWidgets:    edit && removeIntent.MatchString(q),
	}
}

func mutating(name string) bool {
	switch name {
	case toolDashboard, toolAddWidgets, toolRemove:
		return true
	}
	return false
}

func (a toolAccess) allows(name string) bool {
	switch name {
	case toolDashboard:
		return a.createDashboards
	case toolAddWidgets:
		return a.editDashboards
	case toolRemove:
		return a.removeWidgets
	}
	return true
}

var fullAccess = toolAccess{createDashboards: true, editDashboards: true, removeWidgets: true}

func agentTools(allowSQL bool, access toolAccess) []domain.ToolSpec {
	tools := []struct {
		name, desc string
		params     schema
	}{
		{
			toolDescribe,
			"Подробное описание датасета: колонки, типы, роли, частые значения, диапазоны, пропуски, связи с другими датасетами. " +
				"Вызывай, когда спрашивают, что это за данные, за какой период, что означает поле.",
			object(schema{"dataset": text("имя датасета; пусто — описать все")}),
		},
		{
			toolAggregate,
			"Посчитать показатели: метрики в разрезах, по времени, с фильтрами. Платформа сама собирает и проверяет запрос. " +
				"Возвращает result_id и строки результата.",
			object(schema{
				"dataset":  text("имя датасета из списка"),
				"metrics":  list(metricSchema("share — доля в процентах: с expr или числовой column доля суммы, иначе доля строк группы"), "что посчитать; без метрик считается количество строк"),
				"group_by": list(schema{"type": "string"}, "колонки-разрезы, не больше 3"),
				"time": object(schema{
					"column": text("колонка с датой"),
					"grain":  enum(analytics.Grains, "month_of_year, quarter_of_year, day_of_week — для сезонности"),
				}, "column", "grain"),
				"filters": filtersSchema(),
				"join":    list(schema{"type": "string"}, "связанные датасеты, только по связям из описания"),
				"sort":    sortSchema(),
				"limit":   schema{"type": "integer", "description": "сколько строк вернуть"},
				"title":   text("короткий заголовок результата по-русски"),
			}, "dataset", "metrics"),
		},
		{
			toolRows,
			"Показать отдельные записи (до 50): как выглядят данные, конкретные строки по условию.",
			object(schema{
				"dataset": text("имя датасета"),
				"columns": list(schema{"type": "string"}, "какие колонки показать; пусто — все"),
				"filters": filtersSchema(),
				"sort":    sortSchema(),
				"limit":   schema{"type": "integer", "description": "сколько записей, до 50"},
			}, "dataset"),
		},
		{
			toolDuplicates,
			"Проверить повторяющиеся записи: одинаковые строки целиком или повторы по набору колонок.",
			object(schema{
				"dataset": text("имя датасета"),
				"columns": list(schema{"type": "string"}, "колонки, по которым искать повторы; пусто — строка целиком"),
			}, "dataset"),
		},
		{
			toolDashboard,
			"Сохранить дашборд из уже посчитанных результатов. Сначала посчитай каждый показатель через aggregate, потом передай их result_id.",
			object(schema{
				"title":       text("название дашборда"),
				"description": text("о чём дашборд"),
				"widgets":     widgetsSchema(),
			}, "title", "widgets"),
		},
		{
			toolDashboards,
			"Список сохранённых дашбордов с их виджетами. Вызывай, когда просят изменить существующую панель.",
			object(schema{}),
		},
		{
			toolAddWidgets,
			"Добавить на существующий дашборд виджеты из уже посчитанных результатов.",
			object(schema{
				"dashboard_id": text("id дашборда из list_dashboards"),
				"widgets":      widgetsSchema(),
			}, "dashboard_id", "widgets"),
		},
		{
			toolRemove,
			"Убрать виджет с дашборда.",
			object(schema{
				"dashboard_id": text("id дашборда из list_dashboards"),
				"widget_id":    text("id виджета из list_dashboards"),
			}, "dashboard_id", "widget_id"),
		},
		{
			toolAnswer,
			"Закончить работу и ответить пользователю. Вызывай последним.",
			object(schema{
				"text":      text("ответ бизнес-пользователю по-русски, 2-5 предложений с числами из результата"),
				"result_id": text("какой результат показать; по умолчанию последний"),
				"chart":     chartSchema(),
				"followups": list(schema{"type": "string"}, "два-три уместных следующих вопроса"),
			}, "text"),
		},
		{
			toolClarify,
			"Задать уточняющий вопрос, если неясен датасет, метрика, период или разрез. " +
				"Если подходят несколько датасетов и выбор неясен из диалога, вызови до расчётов; предложи реальные названия наборов и при необходимости источники.",
			object(schema{
				"message": text("один конкретный уточняющий вопрос"),
				"options": list(schema{"type": "string"}, "варианты выбора: реальные названия подходящих датасетов или 2-3 трактовки вопроса"),
			}, "message"),
		},
		{
			toolDecline,
			"Отказать, если ответа нет в данных или вопрос не о данных. Объясни причину и предложи, что спросить вместо.",
			object(schema{
				"message":     text("объяснение простыми словами"),
				"suggestions": list(schema{"type": "string"}, "что можно спросить вместо"),
			}, "message"),
		},
	}
	tools = append(tools, insightTools()...)
	tools = append(tools, segmentTools()...)
	if allowSQL {
		tools = append(tools, struct {
			name, desc string
			params     schema
		}{
			toolSQL,
			"Выполнить один SELECT для PostgreSQL, если вопрос не укладывается в aggregate. Запрос проверяется политикой безопасности: " +
				"только чтение и только таблицы каталога.",
			object(schema{"sql": text("запрос SELECT или WITH"), "title": text("заголовок результата")}, "sql"),
		})
	}

	out := make([]domain.ToolSpec, 0, len(tools))
	for _, t := range tools {
		if !access.allows(t.name) {
			continue
		}
		params, err := json.Marshal(t.params)
		if err != nil {
			panic("схема инструмента " + t.name + ": " + err.Error())
		}
		out = append(out, domain.ToolSpec{Name: t.name, Description: t.desc, Parameters: params})
	}
	return out
}

func normalizeArgs(raw json.RawMessage, arrays ...string) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	for _, key := range arrays {
		switch v := m[key].(type) {
		case string:
			if v == "" {
				delete(m, key)
			} else {
				m[key] = []any{v}
			}
		case map[string]any:
			m[key] = []any{v}
		}
	}
	if metrics, ok := m["metrics"].([]any); ok {
		for i, x := range metrics {
			if s, ok := x.(string); ok {
				metrics[i] = map[string]any{"fn": s}
			}
		}
	}
	for _, key := range []string{"limit", "width"} {
		if s, ok := m[key].(string); ok {
			if n, err := strconv.Atoi(s); err == nil {
				m[key] = n
			} else {
				delete(m, key)
			}
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}
