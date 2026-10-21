package llm

import (
	"errors"
	"strings"
	"testing"

	"github.com/heinousness/aibi/internal/core/apperr"
)

func TestCheckModelListed(t *testing.T) {
	cases := []struct {
		name    string
		model   string
		body    string
		wantErr bool
		wantIn  string
	}{
		{
			name:  "нужная модель есть",
			model: "qwen2.5-coder:7b",
			body:  `{"object":"list","data":[{"id":"qwen2.5-coder:7b"},{"id":"llama3:8b"}]}`,
		},
		{
			name:    "список пуст",
			model:   "qwen2.5-coder:7b",
			body:    `{"object":"list","data":[]}`,
			wantErr: true,
			wantIn:  "нет ни одной модели",
		},
		{
			name:    "есть другие модели, нужной нет",
			model:   "qwen2.5-coder:7b",
			body:    `{"object":"list","data":[{"id":"llama3:8b"},{"id":"mistral:7b"}]}`,
			wantErr: true,
			wantIn:  "llama3:8b, mistral:7b",
		},
		{
			name:  "ответ не разобрался — проверка не мешает",
			model: "qwen2.5-coder:7b",
			body:  `не json`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := (&Client{model: c.model}).checkModelListed([]byte(c.body))

			if !c.wantErr {
				if err != nil {
					t.Fatalf("ожидалась успешная проверка, получено: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("ожидалась ошибка, проверка прошла")
			}
			if !errors.Is(err, apperr.ErrLLMUnavailable) {
				t.Errorf("ошибка должна оборачивать ErrLLMUnavailable: %v", err)
			}
			if c.wantIn != "" && !strings.Contains(err.Error(), c.wantIn) {
				t.Errorf("в сообщении нет %q: %v", c.wantIn, err)
			}
		})
	}
}
