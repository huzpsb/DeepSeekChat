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
	"time"

	"hschat/internal/builtin/sandbox"
	cont "hschat/internal/continue"
	"hschat/internal/engine"
	"hschat/internal/llm"
	"hschat/internal/log"
	"hschat/internal/mcp"
	"hschat/internal/model"
	"hschat/internal/storage"
)

type Server struct {
	mode     string
	mux      *http.ServeMux
	staticFS embed.FS
	mcpMgr   *mcp.Manager
	engine   *engine.StreamEngine
}

func New(staticFS embed.FS) *Server {
	if err := log.Init("states.log"); err != nil {
		fmt.Printf("failed to init log: %v\n", err)
	}

	mcpMgr := mcp.NewManager()
	if err := mcpMgr.LoadAndConnect(); err != nil {
		log.Printf("MCP init warning: %v", err)
	}

	// The MCP Manager is the single owner of the app config; the server
	// reads it via mcpMgr.Config() and mutates it via mcpMgr.UpdateConfig().
	cfg := mcpMgr.Config()
	endpoint, apiKey, modelName := cfg.ResolveModel()
	client := llm.NewClient(endpoint, apiKey, modelName)

	s := &Server{
		mode:     "readonly",
		mux:      http.NewServeMux(),
		staticFS: staticFS,
		mcpMgr:   mcpMgr,
		engine:   engine.Init(client, mcpMgr),
	}
	s.registerRoutes()
	return s
}

func (s *Server) Port() int {
	if cfg := s.mcpMgr.Config(); cfg.Port > 0 {
		return cfg.Port
	}
	return 5234
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /api/mode", s.handleGetMode)
	s.mux.HandleFunc("PUT /api/mode", s.handleSetMode)
	s.mux.HandleFunc("GET /api/config", s.handleGetConfig)
	s.mux.HandleFunc("PUT /api/config", s.handleSetConfig)
	s.mux.HandleFunc("GET /api/chats", s.handleListChats)
	s.mux.HandleFunc("GET /api/chats/status", s.handleChatsStatus)
	s.mux.HandleFunc("POST /api/chats", s.handleCreateChat)
	s.mux.HandleFunc("GET /api/chats/{title}", s.handleGetChat)
	s.mux.HandleFunc("DELETE /api/chats/{title}", s.handleDeleteChat)
	s.mux.HandleFunc("POST /api/chats/{title}/dupe", s.handleDupeChat)
	s.mux.HandleFunc("PUT /api/chats/{title}/rename", s.handleRenameChat)
	s.mux.HandleFunc("PUT /api/chats/{title}/rootdir", s.handleSetChatRootDir)
	s.mux.HandleFunc("GET /api/validate/{title}", s.handleValidate)
	s.mux.HandleFunc("POST /api/chat/continue", s.handleContinue)
	s.mux.HandleFunc("GET /api/chat/stream", s.handleStream)
	s.mux.HandleFunc("POST /api/chat/interrupt", s.handleInterrupt)
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

func (s *Server) handleGetMode(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, map[string]any{"mode": s.mode})
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

type providerInfo struct {
	Name   string   `json:"name"`
	Models []string `json:"models"`
}

func (s *Server) configResponse() map[string]any {
	cfg := s.mcpMgr.Config()
	providers := make([]providerInfo, 0, len(cfg.ModelProviders))
	for _, p := range cfg.ModelProviders {
		providers = append(providers, providerInfo{Name: p.Name, Models: p.Models})
	}
	rootDirs := cfg.Sandbox.RootDirs
	if len(rootDirs) == 0 {
		rootDirs = []string{"./agent"}
	}
	return map[string]any{
		"root_dirs": rootDirs,
		"provider":  cfg.Provider,
		"model":     cfg.Model,
		"providers": providers,
	}
}

func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, s.configResponse())
}

func (s *Server) handleSetConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RootDirs *[]string `json:"root_dirs"`
		Provider *string   `json:"provider"`
		Model    *string   `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "invalid body", http.StatusBadRequest)
		return
	}

	// Validate + compute root dirs up front (pure, no config mutation), and
	// apply the runtime switch before saving so a SetRootDir failure leaves
	// both config and disk untouched.
	var dirs []string
	if req.RootDirs != nil {
		seen := map[string]bool{}
		for _, d := range *req.RootDirs {
			d = strings.TrimSpace(d)
			if d == "" || seen[d] {
				continue
			}
			seen[d] = true
			dirs = append(dirs, d)
		}
		if len(dirs) == 0 {
			s.writeError(w, "root_dirs cannot be empty", http.StatusBadRequest)
			return
		}
		for _, d := range dirs {
			if err := sandbox.ValidateRootDir(d); err != nil {
				s.writeError(w, fmt.Sprintf("invalid root dir %q: %v", d, err), http.StatusBadRequest)
				return
			}
		}
		if err := s.mcpMgr.SetRootDir(dirs[0]); err != nil {
			s.writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Mutate + persist atomically under the Manager's write lock. valErr
	// distinguishes a client-caused validation failure (400) from a save
	// failure (500).
	var valErr error
	err := s.mcpMgr.UpdateConfig(func(cfg *model.MCPConfig) error {
		if req.Provider != nil {
			p := cfg.FindProvider(*req.Provider)
			if p == nil {
				valErr = fmt.Errorf("unknown provider")
				return valErr
			}
			cfg.Provider = p.Name
			valid := false
			for _, m := range p.Models {
				if m == cfg.Model {
					valid = true
					break
				}
			}
			if !valid && len(p.Models) > 0 {
				cfg.Model = p.Models[0]
			}
		}

		if req.Model != nil {
			p := cfg.SelectedProvider()
			valid := false
			if p != nil {
				for _, m := range p.Models {
					if m == *req.Model {
						valid = true
						break
					}
				}
			}
			if !valid {
				valErr = fmt.Errorf("unknown model")
				return valErr
			}
			cfg.Model = *req.Model
		}

		if req.RootDirs != nil {
			cfg.Sandbox.RootDirs = dirs
		}
		return nil
	})
	if valErr != nil {
		s.writeError(w, valErr.Error(), http.StatusBadRequest)
		return
	}
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cfg := s.mcpMgr.Config()
	endpoint, apiKey, modelName := cfg.ResolveModel()
	s.engine.SetClient(llm.NewClient(endpoint, apiKey, modelName))
	log.Printf("[server] config_updated root_dirs=%q provider=%q model=%q\n", cfg.Sandbox.RootDirs, cfg.Provider, cfg.Model)
	s.writeJSON(w, s.configResponse())
}

func (s *Server) handleListChats(w http.ResponseWriter, _ *http.Request) {
	chats, err := storage.ListChats()
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	running := s.engine.RunningChats()
	summaries := make([]model.ChatSummary, 0, len(chats))
	for _, c := range chats {
		summaries = append(summaries, model.ChatSummary{Title: c.Title, Running: running[c.Title]})
	}
	s.writeJSON(w, summaries)
}

func (s *Server) handleCreateChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RootDir string `json:"root_dir"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	req.RootDir = strings.TrimSpace(req.RootDir)

	cfg := s.mcpMgr.Config()
	var messages []model.Message
	if cfg.DefaultPrompt != "" {
		messages = append(messages, model.Message{
			Role:         "system",
			Content:      cfg.DefaultPrompt,
			SendToServer: true,
		})
	}
	chat := &model.Chat{
		Title:    time.Now().Format("2006-01-02 150405"),
		Messages: messages,
	}
	if req.RootDir != "" {
		if !cfg.Sandbox.HasRootDir(req.RootDir) {
			s.writeError(w, "root dir not in configured list", http.StatusBadRequest)
			return
		}
		if req.RootDir == cfg.Sandbox.DefaultRootDir() {
			chat.RootDir = ""
		} else {
			chat.RootDir = req.RootDir
		}
	}
	if err := storage.SaveChat(chat); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, chat)
}

func (s *Server) handleGetChat(w http.ResponseWriter, r *http.Request) {
	title := r.PathValue("title")
	chat, savedPos, running, err := s.engine.ReadChatConsistent(title)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusNotFound)
		return
	}
	rootDir := chat.RootDir
	if rootDir == "" {
		cfg := s.mcpMgr.Config()
		rootDir = cfg.Sandbox.DefaultRootDir()
	}
	s.writeJSON(w, map[string]any{
		"title":        chat.Title,
		"root_dir":     rootDir,
		"messages":     chat.Messages,
		"context_size": chat.ContextSize,
		"saved_pos":    savedPos,
		"running":      running,
	})
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
	s.engine.DropSession(title)
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
	s.engine.DropSession(oldTitle)
	s.writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleSetChatRootDir(w http.ResponseWriter, r *http.Request) {
	title := r.PathValue("title")
	var req struct {
		RootDir string `json:"root_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.RootDir) == "" {
		s.writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	req.RootDir = strings.TrimSpace(req.RootDir)

	cfg := s.mcpMgr.Config()
	if !cfg.Sandbox.HasRootDir(req.RootDir) {
		s.writeError(w, "root dir not in configured list", http.StatusBadRequest)
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
	if req.RootDir == cfg.Sandbox.DefaultRootDir() {
		chat.RootDir = ""
	} else {
		chat.RootDir = req.RootDir
	}
	if err := storage.SaveChat(chat); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("[server] chat_rootdir_updated title=%q root_dir=%q\n", title, req.RootDir)
	s.writeJSON(w, map[string]any{"ok": true, "root_dir": req.RootDir})
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
	errors := cont.ValidateChat(chat, lookup, s.mcpMgr.IsToolApproved)
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[server] continue_bad_request err=%q\n", err.Error())
		s.writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		s.writeError(w, "missing title", http.StatusBadRequest)
		return
	}
	log.Printf("[server] continue_request title=%q input_len=%d auto_continue=%v mode=%s\n", req.Title, len(req.Input), req.AutoContinue, s.mode)

	if err := s.engine.StartInference(req.Title, req.Input, req.AutoContinue); err != nil {
		log.Printf("[server] continue_start_error title=%q err=%q\n", req.Title, err.Error())
		if err.Error() == "chat is currently being processed" {
			s.writeError(w, err.Error(), http.StatusConflict)
		} else {
			s.writeError(w, err.Error(), http.StatusNotFound)
		}
		return
	}
	log.Printf("[server] continue_started title=%q\n", req.Title)
	s.writeJSON(w, map[string]bool{"ok": true})
}

// handleChatsStatus is the global running-state SSE endpoint. On connect it
// sends a snapshot of all currently running chats, then a new snapshot on
// every running-state change of any chat:
//
//	event: status
//	data: {"running": {"title A": true, ...}}
//
// Clients use it to keep the sidebar running markers of ALL chats (not just
// the subscribed one) in sync without polling.
func (s *Server) handleChatsStatus(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	running, version := s.engine.RunningChatsSnapshot()
	for {
		data, _ := json.Marshal(map[string]any{"running": running})
		log.Printf("[server] chats_status running=%v\n", running)
		fmt.Fprintf(w, "event: status\ndata: %s\n\n", data)
		flusher.Flush()
		running, version, ok = s.engine.WaitStatusChange(ctx, version)
		if !ok {
			return
		}
	}
}

// handleStream is the reentrant SSE subscription endpoint. A client (fresh
// page, refreshed page, or extra tab) connects here and receives:
//  1. a "sync" event {gen, saved_pos, running, errors?} ("errors" re-delivers
//     the run's error events, which are never persisted and could otherwise
//     be missed when saved_pos already covers them)
//  2. a replay of all not-yet-persisted events of the current run
//  3. live events as the run progresses
//  4. an "idle" event when the run finishes (or immediately if idle)
//
// On a new run (gen change) the sequence restarts from step 1.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	if title == "" {
		s.writeError(w, "missing title", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	log.Printf("[server] stream_open title=%q\n", title)
	defer log.Printf("[server] stream_closed title=%q\n", title)

	reader := s.engine.Subscribe(title)
	ctx := r.Context()
	for {
		res, ok := reader.Wait(ctx)
		if !ok {
			return
		}
		if res.Reset {
			payload := map[string]any{
				"gen":       res.Gen,
				"saved_pos": res.SavedPos,
				"running":   res.Running,
			}
			// Error events are never persisted, so saved_pos may already
			// cover them; re-deliver them with the sync so the client always
			// learns why a run failed.
			if len(res.Errors) > 0 {
				payload["errors"] = res.Errors
			}
			data, _ := json.Marshal(payload)
			log.Printf("[server] stream_sync title=%q gen=%d saved_pos=%d running=%v errors=%d\n", title, res.Gen, res.SavedPos, res.Running, len(res.Errors))
			fmt.Fprintf(w, "event: sync\ndata: %s\n\n", data)
			flusher.Flush()
			continue
		}
		for _, evt := range res.Events {
			data, _ := json.Marshal(evt)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
		}
		if len(res.Events) > 0 {
			flusher.Flush()
		}
		if res.Idle {
			data, _ := json.Marshal(map[string]any{"gen": res.Gen})
			log.Printf("[server] stream_idle title=%q gen=%d\n", title, res.Gen)
			fmt.Fprintf(w, "event: idle\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (s *Server) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	log.Printf("[server] interrupt_request title=%q\n", req.Title)
	accepted := false
	if req.Title != "" {
		accepted = s.engine.RequestInterrupt(req.Title)
	}
	s.writeJSON(w, map[string]any{
		"ok":       true,
		"accepted": accepted,
	})
}

func (s *Server) handleMCPTools(w http.ResponseWriter, _ *http.Request) {
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
		mcpName, toolName := mcp.SplitToolName(fullName)
		if err := s.mcpMgr.SetToolStatus(mcpName, toolName, status); err != nil {
			s.writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	s.writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleMCPReload(w http.ResponseWriter, _ *http.Request) {
	if err := s.mcpMgr.Reload(); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Reload re-read the config from disk; rebuild the LLM client so
	// provider/model/key edits take effect as well. Otherwise the UI would
	// show the new provider (GET /api/config reads the fresh config) while
	// inference silently keeps using the old endpoint.
	cfg := s.mcpMgr.Config()
	endpoint, apiKey, modelName := cfg.ResolveModel()
	s.engine.SetClient(llm.NewClient(endpoint, apiKey, modelName))
	log.Printf("[server] mcp_reloaded provider=%q model=%q\n", cfg.Provider, cfg.Model)
	s.writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	title := r.PathValue("title")
	idx, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		log.Printf("[server] delete_message_bad_index title=%q raw_index=%q\n", title, r.PathValue("index"))
		s.writeError(w, "invalid index", http.StatusBadRequest)
		return
	}
	log.Printf("[server] delete_message_request title=%q idx=%d mode=%s\n", title, idx, s.mode)

	if s.engine.IsInferencingWith(title) {
		log.Printf("[server] delete_message_reject title=%q idx=%d reason=inferencing_same_chat\n", title, idx)
		s.writeError(w, "chat is currently being processed", http.StatusConflict)
		return
	}

	chat, err := storage.GetChat(title)
	if err != nil {
		log.Printf("[server] delete_message_load_error title=%q idx=%d err=%q\n", title, idx, err.Error())
		s.writeError(w, err.Error(), http.StatusNotFound)
		return
	}
	log.Printf("[server] delete_message_loaded title=%q idx=%d messages=%d last=%s target=%s\n", title, idx, len(chat.Messages), describeServerLastMessage(chat), describeServerMessage(chat, idx))

	if s.mode == "readonly" && idx != len(chat.Messages)-1 {
		log.Printf("[server] delete_message_reject title=%q idx=%d reason=readonly_non_last messages=%d\n", title, idx, len(chat.Messages))
		s.writeError(w, "readonly mode", http.StatusForbidden)
		return
	}

	delMode := s.mode
	if delMode == "readonly" {
		delMode = "writable"
	}

	errors, err := cont.DeleteMessage(chat, idx, delMode)
	if err != nil {
		log.Printf("[server] delete_message_error title=%q idx=%d err=%q\n", title, idx, err.Error())
		s.writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("[server] delete_message_after title=%q idx=%d del_mode=%s messages=%d last=%s validation_errors=%d\n", title, idx, delMode, len(chat.Messages), describeServerLastMessage(chat), len(errors))

	if err := storage.SaveChat(chat); err != nil {
		log.Printf("[server] delete_message_save_error title=%q idx=%d err=%q\n", title, idx, err.Error())
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("[server] delete_message_saved title=%q idx=%d messages=%d last=%s\n", title, idx, len(chat.Messages), describeServerLastMessage(chat))

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

func describeServerLastMessage(chat *model.Chat) string {
	if chat == nil || len(chat.Messages) == 0 {
		return "none"
	}
	return describeServerMessage(chat, len(chat.Messages)-1)
}

func describeServerMessage(chat *model.Chat, idx int) string {
	if chat == nil || idx < 0 || idx >= len(chat.Messages) {
		return "none"
	}
	msg := chat.Messages[idx]
	return fmt.Sprintf("idx=%d role=%s content_len=%d reasoning_len=%d tool_calls=%d tool_call_id=%q name=%q send=%v approved=%v", idx, msg.Role, len(msg.Content), len(msg.ReasoningContent), len(msg.ToolCalls), msg.ToolCallID, msg.Name, msg.SendToServer, msg.Approved)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
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

	contentType := mime.TypeByExtension(filepath.Ext(r.URL.Path))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}

func (s *Server) handleFavicon(w http.ResponseWriter, _ *http.Request) {
	data, err := s.staticFS.ReadFile("assets/favicon.ico")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/x-icon")
	w.Write(data)
}
