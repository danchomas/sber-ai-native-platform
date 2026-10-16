package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heinousness/aibi/internal/domain"
)

type SessionRepository struct {
	pool *pgxpool.Pool
}

func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

var _ domain.SessionRepository = (*SessionRepository)(nil)

func (r *SessionRepository) CreateSession(ctx context.Context, s *domain.Session) error {
	const q = `
		INSERT INTO chat_sessions (id, title, dataset_ids)
		VALUES ($1, $2, $3)
		RETURNING created_at, updated_at`
	ids := s.DatasetIDs
	if ids == nil {
		ids = []uuid.UUID{}
	}
	if err := r.pool.QueryRow(ctx, q, s.ID, s.Title, ids).Scan(&s.CreatedAt, &s.UpdatedAt); err != nil {
		return fmt.Errorf("создание диалога: %w", mapError(err))
	}
	return nil
}

func (r *SessionRepository) GetSession(ctx context.Context, id uuid.UUID) (*domain.Session, error) {
	const q = `SELECT id, title, dataset_ids, created_at, updated_at FROM chat_sessions WHERE id = $1`
	var s domain.Session
	err := r.pool.QueryRow(ctx, q, id).Scan(&s.ID, &s.Title, &s.DatasetIDs, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &s, nil
}

func (r *SessionRepository) ListSessions(ctx context.Context, limit int) ([]domain.Session, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
		SELECT id, title, dataset_ids, created_at, updated_at
		  FROM chat_sessions ORDER BY updated_at DESC LIMIT $1`
	rows, err := r.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("список диалогов: %w", mapError(err))
	}
	defer rows.Close()

	var out []domain.Session
	for rows.Next() {
		var s domain.Session
		if err := rows.Scan(&s.ID, &s.Title, &s.DatasetIDs, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, mapError(err)
		}
		out = append(out, s)
	}
	return out, mapError(rows.Err())
}

func (r *SessionRepository) RenameSession(ctx context.Context, id uuid.UUID, title string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE chat_sessions SET title = $2, updated_at = now() WHERE id = $1`, id, title)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *SessionRepository) TouchSession(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE chat_sessions SET updated_at = now() WHERE id = $1`, id)
	return mapError(err)
}

func (r *SessionRepository) DeleteSession(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM chat_sessions WHERE id = $1`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *SessionRepository) AddMessage(ctx context.Context, m *domain.Message) error {
	var payload []byte
	if m.Answer != nil {
		b, err := json.Marshal(m.Answer)
		if err != nil {
			return fmt.Errorf("сериализация ответа: %w", err)
		}
		payload = b
	}

	return withTx(ctx, r.pool, func(tx pgx.Tx) error {
		const q = `
			INSERT INTO chat_messages (id, session_id, role, content, answer)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING created_at`
		if err := tx.QueryRow(ctx, q, m.ID, m.SessionID, m.Role, m.Content, payload).
			Scan(&m.CreatedAt); err != nil {
			return fmt.Errorf("добавление сообщения: %w", mapError(err))
		}
		_, err := tx.Exec(ctx, `UPDATE chat_sessions SET updated_at = now() WHERE id = $1`, m.SessionID)
		return mapError(err)
	})
}

func (r *SessionRepository) ListMessages(ctx context.Context, sessionID uuid.UUID, limit int) ([]domain.Message, error) {
	if limit <= 0 {
		limit = 200
	}
	const q = `
		SELECT id, session_id, role, content, answer, created_at
		  FROM (SELECT * FROM chat_messages WHERE session_id = $1
		         ORDER BY created_at DESC LIMIT $2) m
		 ORDER BY created_at`
	rows, err := r.pool.Query(ctx, q, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("история диалога: %w", mapError(err))
	}
	defer rows.Close()

	var out []domain.Message
	for rows.Next() {
		var (
			m       domain.Message
			payload []byte
		)
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &payload, &m.CreatedAt); err != nil {
			return nil, mapError(err)
		}
		if len(payload) > 0 {
			var a domain.Answer
			if err := json.Unmarshal(payload, &a); err == nil {
				m.Answer = &a
			}
		}
		out = append(out, m)
	}
	return out, mapError(rows.Err())
}

func (r *SessionRepository) LastTurns(ctx context.Context, sessionID uuid.UUID, limit int) ([]domain.Turn, error) {
	if limit <= 0 {
		limit = 8
	}
	msgs, err := r.ListMessages(ctx, sessionID, limit*2)
	if err != nil {
		return nil, err
	}

	var (
		turns   []domain.Turn
		pending *domain.Turn
	)
	for _, m := range msgs {
		switch m.Role {
		case domain.RoleUser:
			if pending != nil {
				turns = append(turns, *pending)
			}
			pending = &domain.Turn{Question: m.Content}
		case domain.RoleAssistant:
			if pending == nil {
				pending = &domain.Turn{}
			}
			pending.Answer = m.Content
			if m.Answer != nil {
				pending.SQL = m.Answer.SQL
				pending.Chart = m.Answer.Chart
			}
			turns = append(turns, *pending)
			pending = nil
		}
	}
	if pending != nil {
		turns = append(turns, *pending)
	}
	if len(turns) > limit {
		turns = turns[len(turns)-limit:]
	}
	return turns, nil
}
