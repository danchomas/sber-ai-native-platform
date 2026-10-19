package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/domain"
)

type SessionRepository interface {
	CreateSession(ctx context.Context, s *domain.Session) error
	GetSession(ctx context.Context, id uuid.UUID) (*domain.Session, error)
	ListSessions(ctx context.Context, limit int) ([]domain.Session, error)
	RenameSession(ctx context.Context, id uuid.UUID, title string) error
	TouchSession(ctx context.Context, id uuid.UUID) error
	DeleteSession(ctx context.Context, id uuid.UUID) error

	AddMessage(ctx context.Context, m *domain.Message) error
	ListMessages(ctx context.Context, sessionID uuid.UUID, limit int) ([]domain.Message, error)
	LastTurns(ctx context.Context, sessionID uuid.UUID, limit int) ([]domain.Turn, error)
}
