package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	cont "hschat/internal/continue"
	"hschat/internal/deepseek"
	"hschat/internal/engine"
	"hschat/internal/mcp"
	"hschat/internal/model"
	"hschat/internal/storage"
)

type Server struct {
	mu            sync.Mutex
	versionCookie string
	mode          string
	mux           *http.ServeMux
	staticFS      embed.FS
	config        *model.MCPConfig
	mcpMgr        *mcp.Manager
	engine        *engine.StreamEngine
}

func New(staticFS embed.FS) *Server {
	cfg, err := storage.LoadConfig()
	if err != nil {
		cfg = &model.MCPConfig{}
	}

	mcpMgr := mcp.NewManager()
	if err := mcpMgr.LoadAndConnect(); err != nil {
		fmt.Println("MCP init warning:", err)
	}

	dsClient := deepseek.NewClient(cfg.APIKey)

	s := &Server{
		mode:     "readonly",
		mux:      http.NewServeMux(),
		staticFS: staticFS,
		config:   cfg,
		mcpMgr:   mcpMgr,
		engine:   engine.Init(dsClient, mcpMgr),
	}
	s.registerRoutes()
	return s
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/favicon.ico" {
			cookie, err := r.Cookie("Version")
			s.mu.Lock()
			expected := s.versionCookie
			s.mu.Unlock()
			if err != nil || (expected != "" && cookie.Value != expected) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(498)
				json.NewEncoder(w).Encode(map[string]string{"error": "Invalid version cookie"})
				return
			}
		}
		s.mux.ServeHTTP(w, r)
	})
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /api/mode", s.handleGetMode)
	s.mux.HandleFunc("PUT /api/mode", s.handleSetMode)
	s.mux.HandleFunc("GET /api/chats", s.handleListChats)
	s.mux.HandleFunc("POST /api/chats", s.handleCreateChat)
	s.mux.HandleFunc("GET /api/chats/{title}", s.handleGetChat)
	s.mux.HandleFunc("DELETE /api/chats/{title}", s.handleDeleteChat)
	s.mux.HandleFunc("POST /api/chats/{title}/dupe", s.handleDupeChat)
	s.mux.HandleFunc("PUT /api/chats/{title}/rename", s.handleRenameChat)
	s.mux.HandleFunc("GET /api/validate/{title}", s.handleValidate)
	s.mux.HandleFunc("POST /api/chat/continue", s.handleContinue)
	s.mux.HandleFunc("POST /api/chat/interrupt", s.handleInterrupt)
	s.mux.HandleFunc("POST /api/chat/stop", s.handleStop)
	s.mux.HandleFunc("GET /api/mcp/tools", s.handleMCPTools)
	s.mux.HandleFunc("PUT /api/mcp/tools", s.handleMCPToolsUpdate)
	s.mux.HandleFunc("POST /api/mcp/reload", s.handleMCPReload)
	s.mux.HandleFunc("DELETE /api/chat/{title}/message/{index}", s.handleDeleteMessage)
	s.mux.HandleFunc("PUT /api/chat/{title}/message/{index}/approve", s.handleApproveToggle)
	s.mux.HandleFunc("PUT /api/chat/{title}/message/{index}", s.handleEditMessage)
	s.mux.HandleFunc("POST /api/chat/{title}/message/{index}", s.handleInsertMessage)
	s.mux.HandleFunc("GET /", s.handleStatic)
	s.mux.HandleFunc("GET /favicon.ico", s.handleFavicon)
	s.mux.HandleFunc("GET /web/", s.handleStatic)
	s.mux.HandleFunc("GET /assets/", s.handleStatic)
}

func (s *Server) writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (s *Server) writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (s *Server) handleGetMode(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, map[string]any{"mode": s.mode, "inferencing": s.engine.IsInferencing()})
}

func (s *Server) handleSetMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Mode != "readonly" && req.Mode != "writable" && req.Mode != "sudo" {
		s.writeError(w, "invalid mode", http.StatusBadRequest)
		return
	}
	s.mode = req.Mode
	s.engine.SetMode(req.Mode)
	s.writeJSON(w, map[string]string{"mode": s.mode})
}

func (s *Server) handleListChats(w http.ResponseWriter, r *http.Request) {
	chats, err := storage.ListChats()
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	summaries := make([]model.ChatSummary, 0, len(chats))
	for _, c := range chats {
		summaries = append(summaries, model.ChatSummary{Title: c.Title})
	}
	s.writeJSON(w, summaries)
}

func (s *Server) handleCreateChat(w http.ResponseWriter, r *http.Request) {
	chat := &model.Chat{
		Title:    time.Now().Format("2006-01-02 150405"),
		Messages: []model.Message{},
	}
	if err := storage.SaveChat(chat); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, chat)
}

func (s *Server) handleGetChat(w http.ResponseWriter, r *http.Request) {
	title := r.PathValue("title")
	chat, err := storage.GetChat(title)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusNotFound)
		return
	}
	s.writeJSON(w, chat)
}

func (s *Server) handleDeleteChat(w http.ResponseWriter, r *http.Request) {
	title := r.PathValue("title")
	if s.engine.IsInferencingWith(title) {
		s.writeError(w, "chat is currently being processed", http.StatusConflict)
		return
	}
	if err := storage.DeleteChat(title); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleDupeChat(w http.ResponseWriter, r *http.Request) {
	title := r.PathValue("title")
	chat, err := storage.DupeChat(title)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, chat)
}

func (s *Server) handleRenameChat(w http.ResponseWriter, r *http.Request) {
	oldTitle := r.PathValue("title")
	if s.engine.IsInferencingWith(oldTitle) {
		s.writeError(w, "chat is currently being processed", http.StatusConflict)
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" {
		s.writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if err := storage.RenameChat(oldTitle, req.Title); err != nil {
		code := http.StatusInternalServerError
		if strings.Contains(err.Error(), "already exists") {
			code = http.StatusConflict
		}
		s.writeError(w, err.Error(), code)
		return
	}
	s.writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	title := r.PathValue("title")
	chat, err := storage.GetChat(title)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusNotFound)
		return
	}
	lookup := func(name string) bool {
		return s.mcpMgr.ToolExists(name)
	}
	errors := cont.ValidateChat(chat, lookup)
	s.writeJSON(w, map[string]any{
		"valid":  len(errors) == 0,
		"errors": errors,
	})
}

func (s *Server) handleContinue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title        string `json:"title"`
		Input        string `json:"input"`
		AutoContinue bool   `json:"auto_continue"`
		Reconnect    bool   `json:"reconnect"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "invalid body", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	if s.engine.IsInferencingWith(req.Title) {
		if req.Reconnect {
			s.engine.RequestInterrupt()
			s.engine.WaitForIdle()
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", `{"error":"no active stream for reconnect"}`)
			flusher.Flush()
			return
		}
		reader := s.engine.Subscribe(req.Title)
		if reader == nil {
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", `{"error":"internal error"}`)
			flusher.Flush()
			return
		}
		s.streamSSE(w, r, flusher, reader)
		return
	}

	if req.Reconnect {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", `{"error":"no active stream for reconnect"}`)
		flusher.Flush()
		return
	}

	if s.engine.IsInferencing() {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", `{"error":"another chat is being processed"}`)
		flusher.Flush()
		return
	}

	if err := s.engine.StartInference(req.Title, req.Input, req.AutoContinue); err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", `{"error":"`+err.Error()+`"}`)
		flusher.Flush()
		return
	}

	reader := s.engine.Subscribe(req.Title)
	if reader == nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", `{"error":"internal error"}`)
		flusher.Flush()
		return
	}
	s.streamSSE(w, r, flusher, reader)
}

func (s *Server) streamSSE(w http.ResponseWriter, r *http.Request, flusher http.Flusher, reader *engine.EventReader) {
	ctx := r.Context()
	for {
		events, done := reader.Wait(ctx)
		for _, evt := range events {
			data, _ := json.Marshal(evt)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, string(data))
			flusher.Flush()
		}
		if done {
			return
		}
	}
}

func (s *Server) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	s.engine.RequestInterrupt()
	s.engine.WaitForIdle()
	s.writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if s.engine.ActiveTitle() == req.Title {
		s.engine.RequestInterrupt()
	}
	s.writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleMCPTools(w http.ResponseWriter, r *http.Request) {
	tools := s.mcpMgr.GetTools()
	if tools == nil {
		tools = []mcp.ToolStatus{}
	}
	s.writeJSON(w, tools)
}

func (s *Server) handleMCPToolsUpdate(w http.ResponseWriter, r *http.Request) {
	var updates map[string]string
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		s.writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	for fullName, status := range updates {
		mcpName, toolName := splitFullName(fullName)
		if err := s.mcpMgr.SetToolStatus(mcpName, toolName, status); err != nil {
			s.writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	s.writeJSON(w, map[string]bool{"ok": true})
}

func splitFullName(fullName string) (string, string) {
	for i := 0; i < len(fullName)-1; i++ {
		if fullName[i] == ':' && fullName[i+1] == ':' {
			return fullName[:i], fullName[i+2:]
		}
	}
	return fullName, ""
}

func (s *Server) handleMCPReload(w http.ResponseWriter, r *http.Request) {
	if err := s.mcpMgr.Reload(); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	title := r.PathValue("title")
	idx, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		s.writeError(w, "invalid index", http.StatusBadRequest)
		return
	}

	if s.engine.IsInferencingWith(title) {
		s.writeError(w, "chat is currently being processed", http.StatusConflict)
		return
	}

	chat, err := storage.GetChat(title)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusNotFound)
		return
	}

	if s.mode == "readonly" && idx != len(chat.Messages)-1 {
		s.writeError(w, "readonly mode", http.StatusForbidden)
		return
	}

	delMode := s.mode
	if delMode == "readonly" {
		delMode = "writable"
	}

	errors, err := cont.DeleteMessage(chat, idx, delMode)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := storage.SaveChat(chat); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if errors != nil {
		s.writeJSON(w, map[string]any{"ok": true, "errors": errors})
	} else {
		s.writeJSON(w, map[string]bool{"ok": true})
	}
}

func (s *Server) handleApproveToggle(w http.ResponseWriter, r *http.Request) {
	title := r.PathValue("title")
	idx, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		s.writeError(w, "invalid index", http.StatusBadRequest)
		return
	}

	if s.engine.IsInferencingWith(title) {
		s.writeError(w, "chat is currently being processed", http.StatusConflict)
		return
	}

	chat, err := storage.GetChat(title)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusNotFound)
		return
	}

	if idx < 0 || idx >= len(chat.Messages) {
		s.writeError(w, "index out of range", http.StatusBadRequest)
		return
	}

	if chat.Messages[idx].Role != "assistant" {
		s.writeError(w, "only assistant messages can be approved", http.StatusBadRequest)
		return
	}

	chat.Messages[idx].Approved = !chat.Messages[idx].Approved

	if err := storage.SaveChat(chat); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]any{"ok": true, "approved": chat.Messages[idx].Approved})
}

func (s *Server) handleEditMessage(w http.ResponseWriter, r *http.Request) {
	if s.mode == "readonly" {
		s.writeError(w, "readonly mode", http.StatusForbidden)
		return
	}
	title := r.PathValue("title")
	idx, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		s.writeError(w, "invalid index", http.StatusBadRequest)
		return
	}

	if s.engine.IsInferencingWith(title) {
		s.writeError(w, "chat is currently being processed", http.StatusConflict)
		return
	}

	var newMsg model.Message
	if err := json.NewDecoder(r.Body).Decode(&newMsg); err != nil {
		s.writeError(w, "invalid body", http.StatusBadRequest)
		return
	}

	chat, err := storage.GetChat(title)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusNotFound)
		return
	}

	errors, err := cont.EditMessage(chat, idx, &newMsg, s.mode, s.mcpMgr.ToolExists)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := storage.SaveChat(chat); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if errors != nil {
		s.writeJSON(w, map[string]any{"ok": true, "errors": errors})
	} else {
		s.writeJSON(w, map[string]bool{"ok": true})
	}
}

func (s *Server) handleInsertMessage(w http.ResponseWriter, r *http.Request) {
	title := r.PathValue("title")
	idx, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		s.writeError(w, "invalid index", http.StatusBadRequest)
		return
	}

	if s.engine.IsInferencingWith(title) {
		s.writeError(w, "chat is currently being processed", http.StatusConflict)
		return
	}

	var newMsg model.Message
	if err := json.NewDecoder(r.Body).Decode(&newMsg); err != nil {
		s.writeError(w, "invalid body", http.StatusBadRequest)
		return
	}

	chat, err := storage.GetChat(title)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusNotFound)
		return
	}

	insertMode := s.mode
	if s.mode == "readonly" {
		if !isReadonlyAskUserInsert(chat, idx, &newMsg) {
			s.writeError(w, "readonly mode", http.StatusForbidden)
			return
		}
		insertMode = "writable"
	}

	errors, err := cont.InsertMessage(chat, idx, &newMsg, insertMode)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := storage.SaveChat(chat); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if errors != nil {
		s.writeJSON(w, map[string]any{"ok": true, "errors": errors})
	} else {
		s.writeJSON(w, map[string]bool{"ok": true})
	}
}

func isReadonlyAskUserInsert(chat *model.Chat, idx int, msg *model.Message) bool {
	if chat == nil || msg == nil || idx != len(chat.Messages) || len(chat.Messages) == 0 {
		return false
	}
	if msg.Role != "tool" || msg.Name != "ask_user" || msg.ToolCallID == "" || !msg.SendToServer {
		return false
	}

	origIdx := len(chat.Messages) - 1
	for origIdx >= 0 && chat.Messages[origIdx].Role == "tool" {
		if chat.Messages[origIdx].ToolCallID == msg.ToolCallID {
			return false
		}
		origIdx--
	}
	if origIdx < 0 || chat.Messages[origIdx].Role != "assistant" {
		return false
	}

	for _, tc := range chat.Messages[origIdx].ToolCalls {
		if tc.ID == msg.ToolCallID && tc.Function.Name == "ask_user" {
			return true
		}
	}
	return false
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	origPath := r.URL.Path
	path := origPath
	if path == "/" {
		path = "web/index.html"
	} else {
		path = path[1:]
	}

	data, err := s.staticFS.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if origPath == "/" {
		version := fmt.Sprintf("%d", time.Now().UnixNano())
		http.SetCookie(w, &http.Cookie{
			Name:     "Version",
			Value:    version,
			Path:     "/",
			HttpOnly: true,
		})
		s.mu.Lock()
		s.versionCookie = version
		s.mu.Unlock()
	}

	contentType := mime.TypeByExtension(filepath.Ext(r.URL.Path))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}

func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	data, err := s.staticFS.ReadFile("assets/favicon.ico")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/x-icon")
	w.Write(data)
}
