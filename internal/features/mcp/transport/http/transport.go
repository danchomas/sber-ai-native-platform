// Package http — MCP-сервер платформы (Model Context Protocol, транспорт
// Streamable HTTP, ответы JSON). Внешний агент — Claude Desktop, IDE или
// свой — получает те же инструменты, что встроенный: описать данные,
// посчитать агрегаты, найти сезонность, построить прогноз. SQL он не
// пишет, как и встроенная модель.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"

	"github.com/heinousness/aibi/internal/core/domain"
)

const maxRequestBytes = 1 << 20

// Поддерживаемые версии протокола, новая — первой.
var protocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

type Tools interface {
	DataTools() []domain.ToolSpec
	CallTool(ctx context.Context, name string, args []byte) (string, bool, error)
	DataContext(ctx context.Context) (string, error)
}

type Handler struct {
	tools   Tools
	version string
}

func New(tools Tools, version string) *Handler {
	return &Handler{tools: tools, version: version}
}

func (h *Handler) Register(r chi.Router) {
	r.Post("/mcp", h.Serve)
	// GET открывает поток уведомлений сервера; их у платформы нет.
	r.Get("/mcp", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil || len(body) > maxRequestBytes {
		writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &rpcError{Code: codeInvalidRequest, Message: "запрос слишком большой"}})
		return
	}

	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil || req.JSONRPC != "2.0" || req.Method == "" {
		code := codeInvalidRequest
		if err != nil {
			code = codeParse
		}
		writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &rpcError{Code: code, Message: "ожидается один запрос JSON-RPC 2.0"}})
		return
	}

	// Уведомления (без id) ответа не требуют.
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	result, rpcErr := h.handle(r.Context(), req)
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr}
	if rpcErr == nil && result == nil {
		resp.Result = struct{}{}
	}
	writeRPC(w, resp)
}

func (h *Handler) handle(ctx context.Context, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		version := protocolVersions[0]
		if slices.Contains(protocolVersions, params.ProtocolVersion) {
			version = params.ProtocolVersion
		}
		instructions, err := h.tools.DataContext(ctx)
		if err != nil {
			instructions = ""
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "aibi", "title": "AI-Native BI", "version": h.version},
			"instructions": "Инструменты платформы AI-Native BI для анализа данных. SQL не нужен: " +
				"передавай параметры, платформа сама соберёт и проверит запрос.\n" + instructions,
		}, nil

	case "ping":
		return struct{}{}, nil

	case "tools/list":
		specs := h.tools.DataTools()
		tools := make([]map[string]any, 0, len(specs))
		for _, t := range specs {
			tools = append(tools, map[string]any{
				"name": t.Name, "description": t.Description,
				"inputSchema": t.Parameters,
				"annotations": map[string]any{"readOnlyHint": true, "openWorldHint": false},
			})
		}
		return map[string]any{"tools": tools}, nil

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
			return nil, &rpcError{Code: codeInvalidParams, Message: "нужны name и arguments"}
		}
		out, isError, err := h.tools.CallTool(ctx, params.Name, params.Arguments)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, &rpcError{Code: codeInternal, Message: "запрос отменён"}
			}
			return nil, &rpcError{Code: codeInternal, Message: "платформа не смогла выполнить инструмент"}
		}
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": out}},
			"isError": isError,
		}, nil
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: "метод " + req.Method + " не поддерживается"}
}

func writeRPC(w http.ResponseWriter, resp rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
