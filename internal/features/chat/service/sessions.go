package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/domain"
)

const defaultTitle = "Новый диалог"

func (s *Service) Create(ctx context.Context, title string, datasetIDs []uuid.UUID) (*domain.Session, error) {
	session := &domain.Session{
		ID:         uuid.New(),
		Title:      orDefault(strings.TrimSpace(title), defaultTitle),
		DatasetIDs: datasetIDs,
	}
	if err := s.sessions.CreateSession(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *Service) List(ctx context.Context, limit int) ([]domain.Session, error) {
	return s.sessions.ListSessions(ctx, limit)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Session, error) {
	return s.sessions.GetSession(ctx, id)
}

func (s *Service) Messages(ctx context.Context, id uuid.UUID, limit int) ([]domain.Message, error) {
	if _, err := s.sessions.GetSession(ctx, id); err != nil {
		return nil, err
	}
	return s.sessions.ListMessages(ctx, id, limit)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.sessions.DeleteSession(ctx, id)
}

func (s *Service) EnsureSession(ctx context.Context, id *uuid.UUID) (*domain.Session, error) {
	if id != nil && *id != uuid.Nil {
		return s.sessions.GetSession(ctx, *id)
	}
	return s.Create(ctx, "", nil)
}

func (s *Service) ensureTitle(ctx context.Context, session *domain.Session, question string) {
	if session.Title != defaultTitle && session.Title != "" {
		return
	}
	title := titleFromQuestion(question)
	if title == "" {
		return
	}
	if err := s.sessions.RenameSession(ctx, session.ID, title); err != nil {
		s.log.WarnContext(ctx, "заголовок диалога не обновлён", "session", session.ID, "error", err)
	}
}

func titleFromQuestion(question string) string {
	title := strings.Join(strings.Fields(question), " ")
	runes := []rune(title)
	if len(runes) > 60 {
		title = strings.TrimSpace(string(runes[:60])) + "…"
	}
	return title
}
