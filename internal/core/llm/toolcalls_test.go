package llm

import "testing"

func TestToolCallsFromText(t *testing.T) {
	known := func(name string) bool { return name == "aggregate" || name == "answer" }

	cases := []struct {
		name     string
		in       string
		wantName []string
		wantArgs string
	}{
		{
			name:     "тег tool_call",
			in:       "<tool_call>\n{\"name\": \"aggregate\", \"arguments\": {\"dataset\": \"orders\"}}\n</tool_call>",
			wantName: []string{"aggregate"},
			wantArgs: `{"dataset": "orders"}`,
		},
		{
			name:     "два вызова подряд",
			in:       `<tool_call>{"name":"aggregate","arguments":{}}</tool_call><tool_call>{"name":"answer","arguments":{"text":"x"}}</tool_call>`,
			wantName: []string{"aggregate", "answer"},
		},
		{
			name:     "голый json с parameters",
			in:       `Вызываю: {"name":"aggregate","parameters":{"dataset":"orders"}}`,
			wantName: []string{"aggregate"},
			wantArgs: `{"dataset":"orders"}`,
		},
		{
			name:     "аргументы строкой",
			in:       `{"name":"aggregate","arguments":"{\"dataset\":\"orders\"}"}`,
			wantName: []string{"aggregate"},
			wantArgs: `{"dataset":"orders"}`,
		},
		{
			name: "чужой инструмент не принимается",
			in:   `{"name":"drop_table","arguments":{}}`,
		},
		{
			name: "обычный текст",
			in:   "В данных 1000 заказов.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ToolCallsFromText(tc.in, known)
			if len(got) != len(tc.wantName) {
				t.Fatalf("вызовов %d, ожидалось %d: %+v", len(got), len(tc.wantName), got)
			}
			for i, name := range tc.wantName {
				if got[i].Name != name {
					t.Fatalf("вызов %d: %q, ожидался %q", i, got[i].Name, name)
				}
			}
			if tc.wantArgs != "" && string(got[0].Arguments) != tc.wantArgs {
				t.Fatalf("аргументы %s, ожидались %s", got[0].Arguments, tc.wantArgs)
			}
		})
	}
}
