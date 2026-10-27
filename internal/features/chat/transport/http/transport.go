package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/logger"
	"github.com/heinousness/aibi/internal/core/transport/http/request"
	"github.com/heinousness/aibi/internal/core/transport/http/response"
)

type Handler struct {
	chat Chat
}

func New(chat Chat) *Handler {
	return &Handler{chat: chat}
}

type createSessionRequest struct {
	Title      string      `json:"title"`
	DatasetIDs []uuid.UUID `json:"dataset_ids"`
}

type askRequest struct {
	Question string `json:"question"`
}

type askResponse struct {
	SessionID uuid.UUID      `json:"session_id"`
	Answer    *domain.Answer `json:"answer"`
}

func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if r.ContentLength > 0 {
		if err := request.Decode(r, &req); err != nil {
			response.Error(w, r, err)
			return
		}
	}

	session, err := h.chat.Create(r.Context(), req.Title, req.DatasetIDs)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusCreated, session)
}

func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.chat.List(r.Context(), request.Int(r, "limit", 50))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, map[string]any{"sessions": sessions})
}

func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	session, err := h.chat.Get(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, session)
}

func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	messages, err := h.chat.Messages(r.Context(), id, request.Int(r, "limit", 200))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, map[string]any{"messages": messages})
}

func (h *Handler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if err := h.chat.Delete(r.Context(), id); err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusNoContent, nil)
}

func (h *Handler) Ask(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}

	var req askRequest
	if err := request.Decode(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	if strings.TrimSpace(req.Question) == "" {
		response.Error(w, r, apperr.NewValidationError("question", "вопрос не может быть пустым"))
		return
	}

	answer, err := h.chat.Ask(r.Context(), id, req.Question)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.JSON(w, r, http.StatusOK, askResponse{SessionID: id, Answer: answer})
}

// AskStream отвечает на вопрос потоком событий (text/event-stream):
// «step» — очередной шаг агента, «answer» — готовый ответ, «error» — сбой.
// Пользователь видит ход работы, пока модель думает, вместо пустого
// ожидания длиной в минуту.
func (h *Handler) AskStream(w http.ResponseWriter, r *http.Request) {
	id, err := request.UUID(r, "id")
	if err != nil {
		response.Error(w, r, err)
		return
	}
	var req askRequest
	if err := request.Decode(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	if strings.TrimSpace(req.Question) == "" {
		response.Error(w, r, apperr.NewValidationError("question", "вопрос не может быть пустым"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		response.Error(w, r, errors.New("потоковая передача не поддерживается"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// nginx иначе буферизует поток и отдаст его одним куском в конце.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	send := func(event string, payload any) {
		data, err := json.Marshal(payload)
		if err != nil {
			logger.From(r.Context()).Error("событие не сериализовано", "event", event, "error", err)
			return
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		flusher.Flush()
	}

	answer, err := h.chat.AskWithProgress(r.Context(), id, req.Question, func(step domain.AgentStep) {
		send("step", step)
	})
	if err != nil {
		_, code, message := response.Classify(err)
		logger.From(r.Context()).Warn("вопрос не обработан", "error", err, "code", code)
		send("error", map[string]string{"code": code, "message": message})
		return
	}
	send("answer", askResponse{SessionID: id, Answer: answer})
}

type Chat interface {
	Create(ctx context.Context, title string, datasetIDs []uuid.UUID) (*domain.Session, error)
	List(ctx context.Context, limit int) ([]domain.Session, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.Session, error)
	Messages(ctx context.Context, id uuid.UUID, limit int) ([]domain.Message, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Ask(ctx context.Context, sessionID uuid.UUID, question string) (*domain.Answer, error)
	AskWithProgress(ctx context.Context, sessionID uuid.UUID, question string, progress func(domain.AgentStep)) (*domain.Answer, error)
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/sessions", func(s chi.Router) {
		s.Get("/", h.ListSessions)
		s.Post("/", h.CreateSession)
		s.Get("/{id}", h.GetSession)
		s.Delete("/{id}", h.DeleteSession)
		s.Get("/{id}/messages", h.Messages)
		s.Post("/{id}/messages", h.Ask)
		s.Post("/{id}/messages/stream", h.AskStream)
	})
}
