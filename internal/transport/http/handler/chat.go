package handler

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/heinousness/aibi/internal/core/apperr"
	"github.com/heinousness/aibi/internal/core/domain"
)

type ChatHandler struct {
	chat Chat
}

func NewChat(chat Chat) *ChatHandler {
	return &ChatHandler{chat: chat}
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

func (h *ChatHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, r, err)
			return
		}
	}

	session, err := h.chat.Create(r.Context(), req.Title, req.DatasetIDs)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, session)
}

func (h *ChatHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.chat.List(r.Context(), queryInt(r, "limit", 50))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"sessions": sessions})
}

func (h *ChatHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	session, err := h.chat.Get(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, session)
}

func (h *ChatHandler) Messages(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	messages, err := h.chat.Messages(r.Context(), id, queryInt(r, "limit", 200))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"messages": messages})
}

func (h *ChatHandler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.chat.Delete(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusNoContent, nil)
}

func (h *ChatHandler) Ask(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}

	var req askRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if strings.TrimSpace(req.Question) == "" {
		writeError(w, r, apperr.NewValidationError("question", "вопрос не может быть пустым"))
		return
	}

	answer, err := h.chat.Ask(r.Context(), id, req.Question)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, askResponse{SessionID: id, Answer: answer})
}
