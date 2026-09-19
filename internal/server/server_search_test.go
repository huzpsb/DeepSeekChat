package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hschat/internal/model"
	"hschat/internal/storage"
)

func TestHandleSearchChats(t *testing.T) {
	setupServerTest(t)
	storage.SaveChat(&model.Chat{
		Title: "findme",
		Messages: []model.Message{
			{Role: "user", Content: "needle here", SendToServer: true},
			{Role: "tool", Name: "x", ToolCallID: "1", Content: "needle in tool", SendToServer: true},
		},
	})
	storage.SaveChat(&model.Chat{
		Title: "other",
		Messages: []model.Message{
			{Role: "user", Content: "unrelated", SendToServer: true},
		},
	})
	// title-only match: must still be returned, and "hits" must decode as
	// an empty array (JSON null would break clients that iterate it)
	storage.SaveChat(&model.Chat{
		Title: "needle by name",
		Messages: []model.Message{
			{Role: "user", Content: "no text match", SendToServer: true},
		},
	})

	srv := New(testStaticFS)

	// missing q -> 400
	req := httptest.NewRequest("GET", "/api/chats/search", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 without q, got %d", w.Code)
	}

	// the exact-literal route must win over /api/chats/{title}
	req = httptest.NewRequest("GET", "/api/chats/search?q=needle", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Query       string                   `json:"query"`
		IncludeTool bool                     `json:"include_tool"`
		Results     []model.ChatSearchResult `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if resp.Query != "needle" || resp.IncludeTool {
		t.Errorf("unexpected echo: %+v", resp)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected findme + title-only match, got %+v", resp.Results)
	}
	// results are ordered most-recently-modified first: the title-only
	// chat was saved last
	titleOnly := resp.Results[0]
	if titleOnly.Title != "needle by name" || !titleOnly.TitleMatch || titleOnly.TotalHits != 0 {
		t.Errorf("unexpected title-only result: %+v", titleOnly)
	}
	if titleOnly.Hits == nil {
		t.Errorf("title-only result must carry hits:[] (JSON null breaks clients)")
	}
	if resp.Results[1].Title != "findme" {
		t.Errorf("expected findme second, got %+v", resp.Results[1])
	}
	if resp.Results[1].TotalHits != 1 {
		t.Errorf("tool result must be excluded without tool=1, got %+v", resp.Results[1])
	}

	// tool=1 includes the tool message
	req = httptest.NewRequest("GET", "/api/chats/search?q=needle&tool=1", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Errorf("expected 2 results with tool=1, got %+v", resp.Results)
	}
	var findme *model.ChatSearchResult
	for i := range resp.Results {
		if resp.Results[i].Title == "findme" {
			findme = &resp.Results[i]
		}
	}
	if findme == nil || findme.TotalHits != 2 {
		t.Errorf("expected findme with 2 hits under tool=1, got %+v", resp.Results)
	}
}
