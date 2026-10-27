package service

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/domain"
)

// Инструменты чтения данных, которые платформа отдаёт внешним агентам
// по MCP. Это тот же код, что в цикле агента, с теми же слоями защиты.
// Дашборды и финальные ответы наружу не отдаются: у внешнего клиента
// своя модель и свой диалог, а ссылки result_id между вызовами не живут.
var dataTools = []string{
	toolDescribe, toolAggregate, toolRows, toolDuplicates,
	toolSeasonality, toolCorrelate, toolForecast, toolAnomalies,
}

func (s *Service) DataTools() []domain.ToolSpec {
	all := agentTools(false)
	out := make([]domain.ToolSpec, 0, len(dataTools))
	for _, t := range all {
		if slices.Contains(dataTools, t.Name) {
			out = append(out, t)
		}
	}
	return out
}

// CallTool выполняет один инструмент вне диалога. Ошибка в аргументах —
// не сбой, а ответ для модели клиента: она её прочитает и исправит вызов.
func (s *Service) CallTool(ctx context.Context, name string, args []byte) (output string, isError bool, err error) {
	if !slices.Contains(dataTools, name) {
		return fmt.Sprintf("инструмента %q нет", name), true, nil
	}
	datasets, err := s.catalog.List(ctx)
	if err != nil {
		return "", false, err
	}
	relations, err := s.catalog.Relations(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "связи каталога недоступны", "error", err)
	}
	scopes, err := s.catalog.Scopes(ctx)
	if err != nil {
		return "", false, err
	}

	r := s.newRun(uuid.Nil, "", datasets, relations, scopes, s.DataTools())
	out, _, callErr := r.invoke(ctx, domain.ToolCall{Name: name, Arguments: args})
	if callErr != nil {
		s.log.InfoContext(ctx, "вызов инструмента по MCP отклонён", "tool", name, "error", callErr)
		return humanReason(callErr), true, nil
	}
	s.log.InfoContext(ctx, "вызов инструмента по MCP", "tool", name)
	return out, false, nil
}

// DataContext — каталог для внешнего агента: тот же текст, что видит
// встроенный агент в системном промпте.
func (s *Service) DataContext(ctx context.Context) (string, error) {
	datasets, err := s.catalog.List(ctx)
	if err != nil {
		return "", err
	}
	relations, err := s.catalog.Relations(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "связи каталога недоступны", "error", err)
	}
	return buildToolContext(datasets, relations), nil
}
