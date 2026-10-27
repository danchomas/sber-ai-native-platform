package service

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/heinousness/aibi/internal/core/config"
)

func TestDataToolsExposeOnlyReading(t *testing.T) {
	svc, _, _ := newTestAgentWith(t, config.Agent{MaxSteps: 4, AllowRawSQL: true},
		newToolLLM(), &fakeExecutor{}, &fakeDashboards{})

	var names []string
	for _, tool := range svc.DataTools() {
		names = append(names, tool.Name)
	}
	for _, forbidden := range []string{toolSQL, toolDashboard, toolAddWidgets, toolRemove, toolAnswer} {
		if slices.Contains(names, forbidden) {
			t.Errorf("по MCP не должен отдаваться %s", forbidden)
		}
	}
	if !slices.Contains(names, toolAggregate) || !slices.Contains(names, toolForecast) {
		t.Fatalf("инструменты чтения: %v", names)
	}
}

func TestCallTool(t *testing.T) {
	exec := &fakeExecutor{result: sampleResult()}
	svc, _, _ := newTestAgent(t, newToolLLM(), exec, &fakeDashboards{})
	ctx := context.Background()

	out, isError, err := svc.CallTool(ctx, toolAggregate, []byte(`{"dataset":"orders","group_by":["status"]}`))
	if err != nil || isError || !strings.Contains(out, `"result_id":"r1"`) {
		t.Fatalf("вызов aggregate: %s (ошибка инструмента %v, %v)", out, isError, err)
	}
	if len(exec.args) != 1 || !strings.Contains(exec.queries[0], `"raw"."ds_orders"`) {
		t.Fatalf("запрос должен собираться компилятором: %v", exec.queries)
	}

	for _, tc := range []struct{ name, args, want string }{
		{toolSQL, `{"sql":"SELECT 1"}`, "нет"},
		{toolDashboard, `{"title":"x","widgets":[]}`, "нет"},
		{toolAggregate, `{"dataset":"orders","group_by":["password"]}`, "нет в датасете"},
	} {
		out, isError, err := svc.CallTool(ctx, tc.name, []byte(tc.args))
		if err != nil || !isError || !strings.Contains(out, tc.want) {
			t.Errorf("%s: ожидался отказ с «%s», получено %q (%v, %v)", tc.name, tc.want, out, isError, err)
		}
	}
	if len(exec.queries) != 1 {
		t.Fatalf("отклонённые вызовы не должны доходить до базы: %v", exec.queries)
	}
}
