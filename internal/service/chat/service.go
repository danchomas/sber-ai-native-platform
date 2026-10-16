package chat

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/domain"
)

const defaultTitle = "Новый диалог"

type Asker interface {
	Ask(ctx context.Context, sessionID uuid.UUID, question string) (*domain.Answer, error)
}

type Service struct {
	sessions domain.SessionRepository
	agent    Asker
	log      *slog.Logger
}

func New(sessions domain.SessionRepository, agent Asker, log *slog.Logger) *Service {
	return &Service{sessions: sessions, agent: agent, log: log}
}

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

func (s *Service) Ask(ctx context.Context, sessionID uuid.UUID, question string) (*domain.Answer, error) {
	session, err := s.sessions.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if session.Title == defaultTitle || session.Title == "" {
		if title := titleFromQuestion(question); title != "" {
			if err := s.sessions.RenameSession(ctx, sessionID, title); err != nil {
				s.log.WarnContext(ctx, "заголовок диалога не обновлён", "session", sessionID, "error", err)
			}
		}
	}
	return s.agent.Ask(ctx, sessionID, question)
}

func (s *Service) EnsureSession(ctx context.Context, id *uuid.UUID) (*domain.Session, error) {
	if id != nil && *id != uuid.Nil {
		return s.sessions.GetSession(ctx, *id)
	}
	return s.Create(ctx, "", nil)
}

func titleFromQuestion(question string) string {
	title := strings.Join(strings.Fields(question), " ")
	runes := []rune(title)
	if len(runes) > 60 {
		title = strings.TrimSpace(string(runes[:60])) + "…"
	}
	return title
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
