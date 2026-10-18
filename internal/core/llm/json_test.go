package llm

import "testing"

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"чистый json", `{"a":1}`, `{"a":1}`},
		{"в блоке кода", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"блок без языка", "```\n{\"a\":1}\n```", `{"a":1}`},
		{"с рассуждением", "Вот план:\n{\"a\":1}\nготово", `{"a":1}`},
		{"вложенные объекты", `{"a":{"b":[1,2]}}`, `{"a":{"b":[1,2]}}`},
		{"скобка в строке", `{"sql":"select '}' from t"}`, `{"sql":"select '}' from t"}`},
		{"экранированная кавычка", `{"s":"a\"}b"}`, `{"s":"a\"}b"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractJSON(tc.in)
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if got != tc.want {
				t.Fatalf("получено %q, ожидалось %q", got, tc.want)
			}
		})
	}
}

func TestExtractJSONErrors(t *testing.T) {
	for _, in := range []string{"", "нет json", `{"a":1`} {
		if _, err := ExtractJSON(in); err == nil {
			t.Fatalf("ожидалась ошибка для %q", in)
		}
	}
}
