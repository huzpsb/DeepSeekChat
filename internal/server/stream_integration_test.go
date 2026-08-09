package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"hschat/internal/model"
	"hschat/internal/storage"
)

type sseEvent struct {
	typ  string
	data string
}

// readSSEUntil reads events from the response body until the wanted event
// type arrives or the timeout elapses. Returns all events read.
func readSSEUntil(t *testing.T, resp *http.Response, wanted string, timeout time.Duration) []sseEvent {
	t.Helper()
	type result struct {
		events []sseEvent
	}
	ch := make(chan result, 1)
	go func() {
		var events []sseEvent
		scanner := bufio.NewScanner(resp.Body)
		var curType, curData string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				curType = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				curData = strings.TrimPrefix(line, "data: ")
			} else if line == "" && curType != "" {
				events = append(events, sseEvent{typ: curType, data: curData})
				if curType == wanted {
					break
				}
				curType, curData = "", ""
			}
		}
		ch <- result{events}
	}()
	select {
	case r := <-ch:
		return r.events
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for SSE event %q", wanted)
		return nil
	}
}

func mockLLMSSEBody(deltas ...string) string {
	var b strings.Builder
	for _, d := range deltas {
		b.WriteString("data: {\"choices\":[{\"delta\":{\"content\":" + strconv.Quote(d) + "}}]}\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func TestStreamReentrant_Integration(t *testing.T) {
	setupServerTest(t)

	gate := make(chan struct{})
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(mockLLMSSEBody("hel", "lo")))
	}))
	defer llm.Close()

	storage.SaveConfig(&model.MCPConfig{
		ModelProviders: []model.ModelProvider{
			{
				Name:     "mock",
				Endpoint: llm.URL,
				APIKey:   "k",
				Models:   []string{"mock"},
			},
		},
		Provider: "mock",
		Model:    "mock",
	})
	storage.SaveChat(&model.Chat{Title: "rc"})

	srv := New(testStaticFS)
	web := httptest.NewServer(srv.mux)
	defer web.Close()

	// start a run; POST must return immediately without streaming
	resp, err := http.Post(web.URL+"/api/chat/continue", "application/json",
		strings.NewReader(`{"title":"rc","input":"hi","auto_continue":false}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("continue start failed: %d", resp.StatusCode)
	}

	// starting again while busy must be rejected per-chat
	resp2, err := http.Post(web.URL+"/api/chat/continue", "application/json",
		strings.NewReader(`{"title":"rc","input":"again","auto_continue":false}`))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for busy chat, got %d", resp2.StatusCode)
	}

	// wait until the user message is persisted (run is now blocked in the LLM gate)
	deadline := time.Now().Add(3 * time.Second)
	for {
		chat, _ := storage.GetChat("rc")
		if len(chat.Messages) >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("user message was not persisted immediately")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// connect mid-run (this is the refresh-reconnect path)
	streamResp, err := http.Get(web.URL + "/api/chat/stream?title=rc")
	if err != nil {
		t.Fatal(err)
	}
	defer streamResp.Body.Close()

	events := readSSEUntil(t, streamResp, "sync", 3*time.Second)
	if len(events) != 1 {
		t.Fatalf("expected sync first, got %#v", events)
	}
	var syncMsg struct {
		Gen      int64 `json:"gen"`
		SavedPos int   `json:"saved_pos"`
		Running  bool  `json:"running"`
	}
	if err := json.Unmarshal([]byte(events[0].data), &syncMsg); err != nil {
		t.Fatal(err)
	}
	if !syncMsg.Running {
		t.Fatalf("expected running=true in sync, got %s", events[0].data)
	}
	if syncMsg.SavedPos != 1 {
		t.Fatalf("expected saved_pos=1 (user message persisted), got %d", syncMsg.SavedPos)
	}

	// release the model; deltas must arrive live, then idle
	close(gate)
	events = readSSEUntil(t, streamResp, "idle", 5*time.Second)
	var deltas, assistantDone int
	for _, ev := range events {
		if ev.typ == "delta" {
			deltas++
		}
		if ev.typ == "assistant_done" {
			assistantDone++
		}
	}
	if deltas != 2 || assistantDone != 1 {
		t.Fatalf("expected 2 deltas + 1 assistant_done before idle, got %#v", events)
	}

	// after the run, disk is authoritative and consistent with saved_pos
	resp3, err := http.Get(web.URL + "/api/chats/rc")
	if err != nil {
		t.Fatal(err)
	}
	var chatView struct {
		Messages []model.Message `json:"messages"`
		SavedPos int             `json:"saved_pos"`
		Running  bool            `json:"running"`
	}
	json.NewDecoder(resp3.Body).Decode(&chatView)
	resp3.Body.Close()
	if chatView.Running {
		t.Fatalf("expected running=false")
	}
	if len(chatView.Messages) != 2 || chatView.Messages[1].Content != "hello" {
		t.Fatalf("unexpected messages: %#v", chatView.Messages)
	}

	// a brand-new subscriber after the run: sync says idle, no event replay
	streamResp2, err := http.Get(web.URL + "/api/chat/stream?title=rc")
	if err != nil {
		t.Fatal(err)
	}
	defer streamResp2.Body.Close()
	events = readSSEUntil(t, streamResp2, "idle", 3*time.Second)
	if len(events) != 2 || events[0].typ != "sync" || events[1].typ != "idle" {
		t.Fatalf("expected sync+idle for finished chat, got %#v", events)
	}
	if err := json.Unmarshal([]byte(events[0].data), &syncMsg); err != nil {
		t.Fatal(err)
	}
	if syncMsg.Running {
		t.Fatalf("expected running=false after run, got %s", events[0].data)
	}
}
