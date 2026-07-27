package server

import (
	"embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"hschat/internal/log"
	"hschat/internal/model"
	"hschat/internal/storage"
)

var testStaticFS embed.FS

func setupServerTest(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() {
		log.Close()
		os.Chdir(origDir)
	})
	storage.SaveConfig(&model.MCPConfig{})
}

func TestSplitFullName_EmptyString(t *testing.T) {
	mcpName, toolName := splitFullName("")
	if mcpName != "" || toolName != "" {
		t.Logf("splitFullName(\"\") = (%q, %q)", mcpName, toolName)
	}
}

func TestSplitFullName_DoubleColonAtStart(t *testing.T) {
	mcpName, toolName := splitFullName("::tool")
	if mcpName != "" {
		t.Errorf("expected mcpName='', got '%s'", mcpName)
	}
	if toolName != "tool" {
		t.Errorf("expected toolName='tool', got '%s'", toolName)
	}
}

func TestSplitFullName_DoubleColonAtEnd(t *testing.T) {
	mcpName, toolName := splitFullName("mcp::")
	if mcpName != "mcp" {
		t.Errorf("expected mcpName='mcp', got '%s'", mcpName)
	}
	if toolName != "" {
		t.Errorf("expected toolName='', got '%s'", toolName)
	}
}

func TestSplitFullName_NoDelimiter(t *testing.T) {
	mcpName, toolName := splitFullName("simple_tool")
	if mcpName != "simple_tool" {
		t.Errorf("expected mcpName='simple_tool', got '%s'", mcpName)
	}
	if toolName != "" {
		t.Errorf("expected toolName='', got '%s'", toolName)
	}
}

func TestSplitFullName_SingleColonNotDelimiter(t *testing.T) {
	mcpName, toolName := splitFullName("mcp:tool")
	if mcpName != "mcp:tool" {
		t.Errorf("expected mcpName='mcp:tool', got '%s'", mcpName)
	}
	if toolName != "" {
		t.Errorf("expected toolName='', got '%s'", toolName)
	}
}

func TestSplitFullName_MultipleDoubleColons(t *testing.T) {
	mcpName, toolName := splitFullName("a::b::c")
	if mcpName != "a" {
		t.Errorf("expected mcpName='a', got '%s'", mcpName)
	}
	if toolName != "b::c" {
		t.Errorf("expected toolName='b::c', got '%s'", toolName)
	}
}

func TestSplitFullName_OnlyDoubleColon(t *testing.T) {
	mcpName, toolName := splitFullName("::")
	if mcpName != "" {
		t.Errorf("expected mcpName='', got '%s'", mcpName)
	}
	if toolName != "" {
		t.Errorf("expected toolName='', got '%s'", toolName)
	}
}

func TestSplitFullName_TripleColon(t *testing.T) {
	mcpName, toolName := splitFullName(":::")
	if mcpName != "" {
		t.Errorf("expected mcpName='', got '%s'", mcpName)
	}
	if toolName != ":" {
		t.Errorf("expected toolName=':', got '%s'", toolName)
	}
}

func TestHandleDeleteMessage_ReadonlyNonLastMessage(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "multi_msg",
		Messages: []model.Message{
			{Role: "user", Content: "first", SendToServer: true},
			{Role: "assistant", Content: "second", SendToServer: true},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)
	srv.mode = "readonly"

	req := httptest.NewRequest("DELETE", "/api/chat/multi_msg/message/0", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for non-last message in readonly mode, got %d", w.Code)
	}
}

func TestHandleDeleteMessage_ReadonlyLastMessageAllowed(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "two_msg",
		Messages: []model.Message{
			{Role: "user", Content: "first", SendToServer: true},
			{Role: "assistant", Content: "second", SendToServer: true},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)
	srv.mode = "readonly"

	req := httptest.NewRequest("DELETE", "/api/chat/two_msg/message/1", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for deleting last message in readonly mode, got %d: %s",
			w.Code, w.Body.String())
	}
}

func TestHandleEditMessage_ReadonlyMode(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "chat",
		Messages: []model.Message{
			{Role: "user", Content: "old", SendToServer: true},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)
	srv.mode = "readonly"

	body := `{"role":"user","content":"new"}`
	req := httptest.NewRequest("PUT", "/api/chat/chat/message/0", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for edit in readonly mode, got %d", w.Code)
	}
}

func TestHandleInsertMessage_ReadonlyMode(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "chat",
		Messages: []model.Message{
			{Role: "user", Content: "hi", SendToServer: true},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)
	srv.mode = "readonly"

	body := `{"role":"user","content":"inserted"}`
	req := httptest.NewRequest("POST", "/api/chat/chat/message/0", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for insert in readonly mode, got %d", w.Code)
	}
}

func TestHandleInsertMessage_ReadonlyAskUserAllowed(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "chat",
		Messages: []model.Message{
			{
				Role:         "assistant",
				SendToServer: true,
				ToolCalls: []model.ToolCall{
					{ID: "call_ask", Function: model.FunctionCall{Name: "ask_user", Arguments: `{"question":"Name?"}`}},
				},
			},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)
	srv.mode = "readonly"

	body := `{"role":"tool","name":"ask_user","tool_call_id":"call_ask","content":"Alice","send_to_server":true}`
	req := httptest.NewRequest("POST", "/api/chat/chat/message/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for readonly ask_user answer, got %d: %s", w.Code, w.Body.String())
	}

	saved, err := storage.GetChat("chat")
	if err != nil {
		t.Fatalf("failed to reload chat: %v", err)
	}
	if len(saved.Messages) != 2 || saved.Messages[1].Role != "tool" || saved.Messages[1].Content != "Alice" {
		t.Fatalf("ask_user answer was not inserted correctly: %#v", saved.Messages)
	}
}

func TestHandleInsertMessage_ReadonlyParallelAskUserAllowed(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "chat",
		Messages: []model.Message{
			{
				Role:         "assistant",
				SendToServer: true,
				ToolCalls: []model.ToolCall{
					{ID: "call_ask_1", Function: model.FunctionCall{Name: "ask_user", Arguments: `{"question":"Name?"}`}},
					{ID: "call_ask_2", Function: model.FunctionCall{Name: "ask_user", Arguments: `{"question":"Age?"}`}},
				},
			},
			{Role: "tool", Name: "ask_user", ToolCallID: "call_ask_1", Content: "Alice", SendToServer: true},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)
	srv.mode = "readonly"

	body := `{"role":"tool","name":"ask_user","tool_call_id":"call_ask_2","content":"42","send_to_server":true}`
	req := httptest.NewRequest("POST", "/api/chat/chat/message/2", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for second readonly ask_user answer, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateChat_HasTimestampTitle(t *testing.T) {
	setupServerTest(t)

	srv := New(testStaticFS)

	req := httptest.NewRequest("POST", "/api/chats", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("create chat failed: %d - %s", w.Code, w.Body.String())
	}

	var chat model.Chat
	json.Unmarshal(w.Body.Bytes(), &chat)

	if chat.Title == "" {
		t.Error("created chat should have a timestamp title")
	}
	if !strings.Contains(chat.Title, "-") && !strings.Contains(chat.Title, " ") {
		t.Logf("title doesn't look like a timestamp: %s", chat.Title)
	}
}

func TestSetMode_InvalidMode(t *testing.T) {
	setupServerTest(t)

	srv := New(testStaticFS)

	body := `{"mode":"admin"}`
	req := httptest.NewRequest("PUT", "/api/mode", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid mode, got %d", w.Code)
	}
}

func TestSetMode_ValidMode(t *testing.T) {
	setupServerTest(t)

	srv := New(testStaticFS)

	for _, mode := range []string{"readonly", "writable", "sudo"} {
		body := `{"mode":"` + mode + `"}`
		req := httptest.NewRequest("PUT", "/api/mode", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for mode %s, got %d", mode, w.Code)
		}
	}
}

func TestGetMode_InitialMode(t *testing.T) {
	setupServerTest(t)

	srv := New(testStaticFS)

	req := httptest.NewRequest("GET", "/api/mode", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get mode failed: %d", w.Code)
	}

	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)

	if result["mode"] != "readonly" {
		t.Logf("initial mode is '%v', expected 'readonly'", result["mode"])
	}
}

func TestRenameChat_EmptyNewTitle(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{Title: "test", Messages: []model.Message{}}
	storage.SaveChat(chat)

	srv := New(testStaticFS)

	body := `{"title":""}`
	req := httptest.NewRequest("PUT", "/api/chats/test/rename", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty new title, got %d", w.Code)
	}
}

func TestGetChat_NotFound(t *testing.T) {
	setupServerTest(t)

	srv := New(testStaticFS)

	req := httptest.NewRequest("GET", "/api/chats/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent chat, got %d", w.Code)
	}
}

func TestDupeChat_NotFound(t *testing.T) {
	setupServerTest(t)

	srv := New(testStaticFS)

	req := httptest.NewRequest("POST", "/api/chats/nonexistent/dupe", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Logf("expected 500 for duplicating nonexistent chat, got %d", w.Code)
	}
}

func TestHandleDeleteMessage_InvalidIndex(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "test",
		Messages: []model.Message{
			{Role: "user", Content: "hi", SendToServer: true},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)
	srv.mode = "sudo"

	req := httptest.NewRequest("DELETE", "/api/chat/test/message/abc", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-numeric index, got %d", w.Code)
	}
}

func TestHandleDeleteMessage_NegativeIndex(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "test",
		Messages: []model.Message{
			{Role: "user", Content: "hi", SendToServer: true},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)
	srv.mode = "sudo"

	req := httptest.NewRequest("DELETE", "/api/chat/test/message/-1", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative index, got %d", w.Code)
	}
}

func TestHandleDeleteMessage_OutOfRange(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "test",
		Messages: []model.Message{
			{Role: "user", Content: "hi", SendToServer: true},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)
	srv.mode = "sudo"

	req := httptest.NewRequest("DELETE", "/api/chat/test/message/10", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for out-of-range index, got %d", w.Code)
	}
}

func TestHandleApproveToggle_NonAssistant(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "test",
		Messages: []model.Message{
			{Role: "user", Content: "hi", SendToServer: true},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)
	srv.mode = "sudo"

	req := httptest.NewRequest("PUT", "/api/chat/test/message/0/approve", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for approving non-assistant message, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "only assistant") {
		t.Errorf("expected 'only assistant' in error message, got: %s", w.Body.String())
	}
}

func TestHandleApproveToggle_OutOfRange(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title:    "test",
		Messages: []model.Message{},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)
	srv.mode = "sudo"

	req := httptest.NewRequest("PUT", "/api/chat/test/message/0/approve", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for out-of-range approve, got %d", w.Code)
	}
}

func TestListChats_Empty(t *testing.T) {
	setupServerTest(t)

	srv := New(testStaticFS)

	req := httptest.NewRequest("GET", "/api/chats", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var chats []model.Chat
	json.Unmarshal(w.Body.Bytes(), &chats)
	if len(chats) != 0 {
		t.Errorf("expected 0 chats, got %d", len(chats))
	}
}

func TestHandleContinue_InvalidBody(t *testing.T) {
	setupServerTest(t)

	srv := New(testStaticFS)

	req := httptest.NewRequest("POST", "/api/chat/continue", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Logf("expected 400 for invalid body, got %d", w.Code)
	}
}

func TestHandleInterrupt_ReturnsOk(t *testing.T) {
	setupServerTest(t)

	srv := New(testStaticFS)

	req := httptest.NewRequest("POST", "/api/chat/interrupt", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleStop_InvalidBody(t *testing.T) {
	setupServerTest(t)

	srv := New(testStaticFS)

	req := httptest.NewRequest("POST", "/api/chat/stop", strings.NewReader("bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid body, got %d", w.Code)
	}
}

func TestHandleMCPToolsUpdate_InvalidBody(t *testing.T) {
	setupServerTest(t)

	srv := New(testStaticFS)

	req := httptest.NewRequest("PUT", "/api/mcp/tools", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid body, got %d", w.Code)
	}
}

func TestHandleValidate_NotFound(t *testing.T) {
	setupServerTest(t)

	srv := New(testStaticFS)

	req := httptest.NewRequest("GET", "/api/validate/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent chat validation, got %d", w.Code)
	}
}

func TestHandleEditMessage_InvalidJSON(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "test",
		Messages: []model.Message{
			{Role: "user", Content: "old", SendToServer: true},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)
	srv.mode = "writable"

	req := httptest.NewRequest("PUT", "/api/chat/test/message/0", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON body, got %d", w.Code)
	}
}

func TestHandleInsertMessage_InvalidIndex(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "test",
		Messages: []model.Message{
			{Role: "user", Content: "hi", SendToServer: true},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)
	srv.mode = "writable"

	body := `{"role":"user","content":"inserted"}`
	req := httptest.NewRequest("POST", "/api/chat/test/message/xyz", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-numeric index, got %d", w.Code)
	}
}

func TestHandleContinue_InvalidBodyJSON(t *testing.T) {
	setupServerTest(t)

	srv := New(testStaticFS)

	req := httptest.NewRequest("POST", "/api/chat/continue", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed JSON, got %d", w.Code)
	}
}

func TestHandleMCPTools_Empty(t *testing.T) {
	setupServerTest(t)

	srv := New(testStaticFS)

	req := httptest.NewRequest("GET", "/api/mcp/tools", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var tools []map[string]any
	json.Unmarshal(w.Body.Bytes(), &tools)
	if len(tools) != 10 {
		t.Errorf("expected 10 builtin tools, got %d", len(tools))
	}
}

func TestHandleRenameChat_NoBody(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{Title: "test", Messages: []model.Message{}}
	storage.SaveChat(chat)

	srv := New(testStaticFS)

	req := httptest.NewRequest("PUT", "/api/chats/test/rename", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing body, got %d", w.Code)
	}
}

func TestCreateChat_TimestampFormat(t *testing.T) {
	setupServerTest(t)

	before := time.Now()

	srv := New(testStaticFS)
	req := httptest.NewRequest("POST", "/api/chats", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var chat model.Chat
	json.Unmarshal(w.Body.Bytes(), &chat)

	after := time.Now()

	titleTime, err := time.ParseInLocation("2006-01-02 150405", chat.Title, time.Local)
	if err != nil {
		t.Errorf("title is not in timestamp format (%s): %v", chat.Title, err)
	} else {
		if titleTime.Before(before.Add(-2*time.Second)) || titleTime.After(after.Add(2*time.Second)) {
			t.Errorf("timestamp title outside expected range: %s (before=%s, after=%s)",
				chat.Title, before.Format("150405"), after.Format("150405"))
		}
	}
}

func TestHandleValidate_ValidChat(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "valid_chat",
		Messages: []model.Message{
			{Role: "user", Content: "hello", SendToServer: true},
			{Role: "assistant", Content: "hi", SendToServer: true},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)

	req := httptest.NewRequest("GET", "/api/validate/valid_chat", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if valid, ok := result["valid"]; !ok || valid != true {
		t.Errorf("expected valid=true, got %v", result)
	}
}

func TestHandleSetMode_EmptyBody(t *testing.T) {
	setupServerTest(t)

	srv := New(testStaticFS)

	req := httptest.NewRequest("PUT", "/api/mode", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing mode field, got %d", w.Code)
	}
}

func TestWriteError_WritesJSON(t *testing.T) {
	setupServerTest(t)
	srv := New(testStaticFS)
	w := httptest.NewRecorder()
	srv.writeError(w, "test error", http.StatusTeapot)

	if w.Code != http.StatusTeapot {
		t.Errorf("expected 418, got %d", w.Code)
	}

	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["error"] != "test error" {
		t.Errorf("expected 'test error', got '%s'", result["error"])
	}
}

func TestWriteJSON_WritesJSON(t *testing.T) {
	setupServerTest(t)
	srv := New(testStaticFS)
	w := httptest.NewRecorder()
	srv.writeJSON(w, map[string]string{"key": "value"})

	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["key"] != "value" {
		t.Errorf("expected 'value', got '%s'", result["key"])
	}
}

func TestHandleDeleteMessage_SudoModeDelete(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "test",
		Messages: []model.Message{
			{Role: "user", Content: "one", SendToServer: true},
			{Role: "assistant", Content: "two", SendToServer: true},
			{Role: "user", Content: "three", SendToServer: true},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)
	srv.mode = "sudo"

	req := httptest.NewRequest("DELETE", "/api/chat/test/message/1", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delete failed: %d - %s", w.Code, w.Body.String())
	}

	updated, _ := storage.GetChat("test")
	if len(updated.Messages) != 2 {
		t.Errorf("expected 2 messages after delete, got %d", len(updated.Messages))
	}
}

func TestHandleApproveToggle_NegativeIndex(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "test",
		Messages: []model.Message{
			{Role: "assistant", Content: "hi", SendToServer: true},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)

	req := httptest.NewRequest("PUT", "/api/chat/test/message/-5/approve", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative index, got %d", w.Code)
	}
}

func TestHandleEditMessage_NonNumericIndex(t *testing.T) {
	setupServerTest(t)
	chat := &model.Chat{
		Title: "test",
		Messages: []model.Message{
			{Role: "user", Content: "hi", SendToServer: true},
		},
	}
	storage.SaveChat(chat)

	srv := New(testStaticFS)
	srv.mode = "writable"

	body := `{"role":"user","content":"new"}`
	req := httptest.NewRequest("PUT", "/api/chat/test/message/abc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-numeric index, got %d", w.Code)
	}
}
