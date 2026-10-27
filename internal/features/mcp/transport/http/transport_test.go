package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/heinousness/aibi/internal/core/domain"
)

type fakeTools struct {
	called string
	args   string
}

func (f *fakeTools) DataTools() []domain.ToolSpec {
	return []domain.ToolSpec{{Name: "aggregate", Description: "агрегат", Parameters: json.RawMessage(`{"type":"object"}`)}}
}

func (f *fakeTools) CallTool(_ context.Context, name string, args []byte) (string, bool, error) {
	f.called, f.args = name, string(args)
	if name != "aggregate" {
		return "инструмента нет", true, nil
	}
	return `{"result_id":"r1"}`, false, nil
}

func (f *fakeTools) DataContext(context.Context) (string, error) {
	return "ДАТАСЕТ «orders»", nil
}

func rpc(t *testing.T, h http.Handler, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body)))
	if rec.Body.Len() == 0 {
		return rec.Code, nil
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("ответ не JSON: %s", rec.Body.String())
	}
	return rec.Code, out
}

func TestServe(t *testing.T) {
	tools := &fakeTools{}
	router := chi.NewRouter()
	New(tools, "test").Register(router)

	t.Run("initialize согласует версию и отдаёт каталог", func(t *testing.T) {
		_, out := rpc(t, router, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`)
		result := out["result"].(map[string]any)
		if result["protocolVersion"] != "2025-03-26" {
			t.Fatalf("версия протокола: %v", result["protocolVersion"])
		}
		if !strings.Contains(result["instructions"].(string), "ДАТАСЕТ «orders»") {
			t.Fatal("в инструкциях нет каталога")
		}
	})

	t.Run("неизвестная версия заменяется новейшей", func(t *testing.T) {
		_, out := rpc(t, router, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
		if out["result"].(map[string]any)["protocolVersion"] != protocolVersions[0] {
			t.Fatalf("версия: %v", out["result"])
		}
	})

	t.Run("уведомление без ответа", func(t *testing.T) {
		code, out := rpc(t, router, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		if code != http.StatusAccepted || out != nil {
			t.Fatalf("код %d, тело %v", code, out)
		}
	})

	t.Run("список инструментов только для чтения", func(t *testing.T) {
		_, out := rpc(t, router, `{"jsonrpc":"2.0","id":"a","method":"tools/list"}`)
		list := out["result"].(map[string]any)["tools"].([]any)
		tool := list[0].(map[string]any)
		if tool["name"] != "aggregate" || tool["inputSchema"].(map[string]any)["type"] != "object" {
			t.Fatalf("инструмент: %v", tool)
		}
		if tool["annotations"].(map[string]any)["readOnlyHint"] != true {
			t.Fatal("инструменты платформы только читают данные")
		}
		if out["id"] != "a" {
			t.Fatalf("id запроса не сохранён: %v", out["id"])
		}
	})

	t.Run("вызов инструмента", func(t *testing.T) {
		_, out := rpc(t, router, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"aggregate","arguments":{"dataset":"orders"}}}`)
		result := out["result"].(map[string]any)
		text := result["content"].([]any)[0].(map[string]any)["text"]
		if text != `{"result_id":"r1"}` || result["isError"] != false || tools.args != `{"dataset":"orders"}` {
			t.Fatalf("результат вызова: %v, аргументы %s", result, tools.args)
		}
	})

	t.Run("ошибка инструмента возвращается модели клиента", func(t *testing.T) {
		_, out := rpc(t, router, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"drop_table","arguments":{}}}`)
		if out["result"].(map[string]any)["isError"] != true {
			t.Fatalf("ожидалась ошибка инструмента: %v", out)
		}
	})

	t.Run("неизвестный метод и мусор", func(t *testing.T) {
		_, out := rpc(t, router, `{"jsonrpc":"2.0","id":4,"method":"resources/list"}`)
		if out["error"].(map[string]any)["code"] != float64(codeMethodNotFound) {
			t.Fatalf("ожидался method not found: %v", out)
		}
		_, out = rpc(t, router, `не json`)
		if out["error"].(map[string]any)["code"] != float64(codeParse) {
			t.Fatalf("ожидалась ошибка разбора: %v", out)
		}
	})
}
