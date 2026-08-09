package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cont "hschat/internal/continue"
	"hschat/internal/deepseek"
	"hschat/internal/model"
	"hschat/internal/storage"
)

type mockExecutor struct{}

func (mockExecutor) IsToolApproved(string) (bool, bool) { return true, false }
func (mockExecutor) ToolExists(string) bool             { return true }
func (mockExecutor) ExecuteTool(context.Context, string, string) (*model.ToolResult, error) {
	return &model.ToolResult{Content: []model.ToolContent{{Type: "text", Text: "ok"}}}, nil
}
func (mockExecutor) GetAllowedTools() []model.ToolDef { return nil }
func (mockExecutor) GetToolDef(string) *model.ToolDef { return nil }

func setupEngineTest(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })
}

func newTestEngine(endpoint string) *StreamEngine {
	dsClient := deepseek.NewClient("test-key", model.ThirdPartyConfig{
		Enabled:  true,
		Endpoint: endpoint,
		Model:    "mock",
	})
	return Init(dsClient, mockExecutor{})
}

func sseBody(deltas ...string) string {
	var b strings.Builder
	for _, d := range deltas {
		b.WriteString("data: {\"choices\":[{\"delta\":{\"content\":" + strconv.Quote(d) + "}}]}\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func waitCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestSubscribe_IdleSession(t *testing.T) {
	setupEngineTest(t)
	e := newTestEngine("http://127.0.0.1:1")

	reader := e.Subscribe("nope")
	res, ok := reader.Wait(waitCtx(t))
	if !ok || !res.Reset {
		t.Fatalf("expected initial Reset, got %+v ok=%v", res, ok)
	}
	if res.Running {
		t.Fatalf("expected running=false for idle session")
	}
	res, ok = reader.Wait(waitCtx(t))
	if !ok || !res.Idle {
		t.Fatalf("expected Idle after Reset on idle session, got %+v ok=%v", res, ok)
	}
	// no more events: Wait should block until ctx expires
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, ok = reader.Wait(ctx)
	if ok {
		t.Fatalf("expected Wait to block on idle session")
	}
}

func TestRun_ReplayAndPersistence(t *testing.T) {
	setupEngineTest(t)
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-gate: // hold the LLM response until the test releases it
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseBody("he", "llo")))
	}))
	defer srv.Close()

	e := newTestEngine(srv.URL)
	chat := &model.Chat{Title: "c1", Messages: nil}
	if err := storage.SaveChat(chat); err != nil {
		t.Fatal(err)
	}

	if err := e.StartInference("c1", "hi", false); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := e.StartInference("c1", "again", false); err == nil {
		t.Fatalf("expected busy rejection for same chat")
	}

	reader := e.Subscribe("c1")
	res, ok := reader.Wait(waitCtx(t))
	if !ok || !res.Reset || !res.Running {
		t.Fatalf("expected Reset with running=true, got %+v ok=%v", res, ok)
	}

	close(gate)

	var got []cont.ContinueEvent
	sawIdle := false
	for !sawIdle {
		res, ok = reader.Wait(waitCtx(t))
		if !ok {
			t.Fatalf("subscription closed before idle")
		}
		got = append(got, res.Events...)
		if res.Idle {
			sawIdle = true
		}
	}
	e.WaitForIdle("c1")

	// whatever the replay start position was, the disk must now be authoritative
	saved, savedPos, running, err := e.ReadChatConsistent("c1")
	if err != nil {
		t.Fatal(err)
	}
	if running {
		t.Fatalf("expected not running")
	}
	if len(saved.Messages) != 2 || saved.Messages[0].Role != "user" || saved.Messages[1].Role != "assistant" {
		t.Fatalf("unexpected messages on disk: %#v", saved.Messages)
	}
	if saved.Messages[1].Content != "hello" {
		t.Fatalf("expected assistant content 'hello', got %q", saved.Messages[1].Content)
	}
	sess := e.getSession("c1", false)
	sess.mu.Lock()
	events := len(sess.events)
	sess.mu.Unlock()
	if savedPos != events {
		t.Fatalf("expected savedPos == len(events) after run end, got %d != %d", savedPos, events)
	}

	// a fresh subscriber after the run sees sync + idle, no stale events
	reader2 := e.Subscribe("c1")
	res, _ = reader2.Wait(waitCtx(t))
	if !res.Reset || res.Running {
		t.Fatalf("fresh subscriber expected idle Reset, got %+v", res)
	}
	res, _ = reader2.Wait(waitCtx(t))
	if !res.Idle {
		t.Fatalf("fresh subscriber expected Idle, got %+v", res)
	}
	_ = got
}

func TestRun_ParallelChats(t *testing.T) {
	setupEngineTest(t)
	var inFlight int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&inFlight, 1)
		defer atomic.AddInt32(&inFlight, -1)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseBody("x")))
	}))
	defer srv.Close()

	e := newTestEngine(srv.URL)
	storage.SaveChat(&model.Chat{Title: "a"})
	storage.SaveChat(&model.Chat{Title: "b"})

	if err := e.StartInference("a", "hi", false); err != nil {
		t.Fatalf("start a: %v", err)
	}
	if err := e.StartInference("b", "hi", false); err != nil {
		t.Fatalf("start b: %v (parallel chats must be allowed)", err)
	}
	if !e.IsInferencingWith("a") || !e.IsInferencingWith("b") {
		t.Fatalf("expected both chats inferencing")
	}
	close(release)
	e.WaitForIdle("a")
	e.WaitForIdle("b")

	savedA, _, _, _ := e.ReadChatConsistent("a")
	savedB, _, _, _ := e.ReadChatConsistent("b")
	if len(savedA.Messages) != 2 || len(savedB.Messages) != 2 {
		t.Fatalf("expected both chats to have 2 messages, got %d and %d", len(savedA.Messages), len(savedB.Messages))
	}
}

func TestRun_Interrupt(t *testing.T) {
	setupEngineTest(t)
	hang := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// hang until the test releases it or the client disconnects
		select {
		case <-hang:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(hang)

	e := newTestEngine(srv.URL)
	storage.SaveChat(&model.Chat{Title: "c2"})

	if err := e.StartInference("c2", "hi", false); err != nil {
		t.Fatalf("start: %v", err)
	}
	// give the goroutine a moment to reach the HTTP call
	time.Sleep(100 * time.Millisecond)
	e.RequestInterrupt("c2")

	done := make(chan struct{})
	go func() {
		e.WaitForIdle("c2")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("interrupt did not stop the run")
	}

	saved, savedPos, running, _ := e.ReadChatConsistent("c2")
	if running {
		t.Fatalf("expected not running after interrupt")
	}
	if len(saved.Messages) == 0 {
		t.Fatalf("expected at least the user message on disk after interrupt")
	}
	sess := e.getSession("c2", false)
	sess.mu.Lock()
	events := len(sess.events)
	sess.mu.Unlock()
	if savedPos != events {
		t.Fatalf("savedPos mismatch after interrupt: %d != %d", savedPos, events)
	}
}

func TestEventReader_GenReset(t *testing.T) {
	setupEngineTest(t)
	e := newTestEngine("http://127.0.0.1:1")
	sess := e.getSession("g", true)

	sess.mu.Lock()
	sess.running = true
	sess.gen = 1
	sess.events = []cont.ContinueEvent{{Type: "user_added", Content: "a"}}
	sess.savedPos = 1
	sess.broadcastLocked()
	sess.mu.Unlock()

	reader := e.Subscribe("g")
	res, ok := reader.Wait(waitCtx(t))
	if !ok || !res.Reset || res.Gen != 1 || res.SavedPos != 1 {
		t.Fatalf("unexpected initial reset: %+v", res)
	}

	// simulate a new run: gen bump + cleared log
	sess.mu.Lock()
	sess.gen = 2
	sess.events = nil
	sess.savedPos = 0
	sess.broadcastLocked()
	sess.mu.Unlock()

	res, ok = reader.Wait(waitCtx(t))
	if !ok || !res.Reset || res.Gen != 2 || res.SavedPos != 0 {
		t.Fatalf("expected Reset for new gen, got %+v", res)
	}

	// new events of gen 2 are delivered
	sess.mu.Lock()
	sess.events = append(sess.events, cont.ContinueEvent{Type: "delta", Content: "x"})
	sess.broadcastLocked()
	sess.mu.Unlock()

	res, ok = reader.Wait(waitCtx(t))
	if !ok || len(res.Events) != 1 || res.Events[0].Type != "delta" {
		t.Fatalf("expected live delta, got %+v", res)
	}

	// run finishes -> idle exactly once
	sess.mu.Lock()
	sess.running = false
	sess.broadcastLocked()
	sess.mu.Unlock()

	res, ok = reader.Wait(waitCtx(t))
	if !ok || !res.Idle {
		t.Fatalf("expected Idle, got %+v", res)
	}

	// and a third run resets again with running=true
	sess.mu.Lock()
	sess.gen = 3
	sess.events = nil
	sess.savedPos = 0
	sess.running = true
	sess.broadcastLocked()
	sess.mu.Unlock()

	res, ok = reader.Wait(waitCtx(t))
	if !ok || !res.Reset || res.Gen != 3 || !res.Running {
		t.Fatalf("expected Reset running=true for gen 3, got %+v", res)
	}
}

func TestRun_ReconnectAfterMidRunSave(t *testing.T) {
	setupEngineTest(t)
	var reqCount int32
	gate2 := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&reqCount, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			// first request: a tool call, then done
			w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"some_tool\",\"arguments\":\"{}\"}}]}}]}\n\ndata: [DONE]\n\n"))
			return
		}
		// second request: gated, then the final answer
		select {
		case <-gate2:
		case <-r.Context().Done():
			return
		}
		w.Write([]byte(sseBody("final")))
	}))
	defer srv.Close()

	e := newTestEngine(srv.URL)
	storage.SaveChat(&model.Chat{Title: "tc"})

	if err := e.StartInference("tc", "hi", true); err != nil {
		t.Fatalf("start: %v", err)
	}

	// wait until the tool result is persisted (mid-run save happened)
	deadline := time.Now().Add(5 * time.Second)
	var savedPosAtConnect int
	for {
		chat, savedPos, _, _ := e.ReadChatConsistent("tc")
		if len(chat.Messages) >= 3 && chat.Messages[2].Role == "tool" {
			savedPosAtConnect = savedPos
			break
		}
		if time.Now().After(deadline) {
			chat2, _, _, _ := e.ReadChatConsistent("tc")
			t.Fatalf("tool result not persisted; messages=%#v", chat2.Messages)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// reconnect now: replay must start exactly at the persisted position,
	// so user_added / tool_call / tool_result are NOT replayed
	reader := e.Subscribe("tc")
	res, ok := reader.Wait(waitCtx(t))
	if !ok || !res.Reset {
		t.Fatalf("expected Reset, got %+v", res)
	}
	if res.SavedPos != savedPosAtConnect {
		t.Fatalf("replay start %d != persisted %d", res.SavedPos, savedPosAtConnect)
	}

	close(gate2)

	var replayed []string
	for {
		res, ok = reader.Wait(waitCtx(t))
		if !ok {
			t.Fatalf("subscription closed early")
		}
		for _, ev := range res.Events {
			replayed = append(replayed, ev.Type)
		}
		if res.Idle {
			break
		}
	}
	for _, typ := range replayed {
		if typ == "user_added" || typ == "tool_call" || typ == "tool_result" {
			t.Fatalf("persisted event %q was replayed (would render twice)", typ)
		}
	}

	saved, savedPos, running, _ := e.ReadChatConsistent("tc")
	if running {
		t.Fatalf("expected not running")
	}
	if len(saved.Messages) != 4 || saved.Messages[3].Content != "final" {
		t.Fatalf("unexpected final messages: %#v", saved.Messages)
	}
	sess := e.getSession("tc", false)
	sess.mu.Lock()
	events := len(sess.events)
	sess.mu.Unlock()
	if savedPos != events {
		t.Fatalf("final savedPos %d != events %d", savedPos, events)
	}
}

func TestReadChatConsistent_NoSession(t *testing.T) {
	setupEngineTest(t)
	e := newTestEngine("http://127.0.0.1:1")
	_, _, _, err := e.ReadChatConsistent("missing")
	if err == nil {
		t.Fatalf("expected error for missing chat")
	}
}
