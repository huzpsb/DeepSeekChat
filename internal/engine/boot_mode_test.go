package engine

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"hschat/internal/llm"
	"hschat/internal/model"
	"hschat/internal/storage"
)

// TestInit_BootsReadonly pins the fail-closed boot default: an engine
// taken straight from Init() — before any SetMode call, i.e. exactly the
// state the server runs in between startup and the first PUT /api/mode —
// must gate like readonly, not like the "" zero value. The "" zero value
// silently behaved like writable: a malformed tool call interrupted the
// run with an error event instead of being answered with a "tool not
// found" tool_result, which is what the UI's "read" badge promises.
func TestInit_BootsReadonly(t *testing.T) {
	setupEngineTest(t)

	if got := Init(nil, mockExecutor{}).Mode(); got != "readonly" {
		t.Fatalf("Init() mode = %q, want readonly", got)
	}
}

// TestRun_InvalidToolCall_BootDefaultIsReadonly replays the original
// glitch end-to-end: no SetMode call at all, engine fresh from Init(),
// malformed tool call in the chat. The run must continue (tool_result
// echo + follow-up assistant answer), not die with an error event.
func TestRun_InvalidToolCall_BootDefaultIsReadonly(t *testing.T) {
	setupEngineTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseBody("after")))
	}))
	defer srv.Close()

	client := llm.NewClient(srv.URL, "test-key", "mock")
	e := Init(client, existsExecutor{exists: func(string) bool { return false }})
	// Deliberately NO e.SetMode(...) — this is the boot state.

	title := "boot-default"
	chat := &model.Chat{Title: title, Messages: []model.Message{{
		Role:         "assistant",
		SendToServer: true,
		ToolCalls: []model.ToolCall{{
			ID:       "bad1",
			Function: model.FunctionCall{Name: "no_such_tool", Arguments: "{}"},
		}},
	}}}
	if err := storage.SaveChat(chat); err != nil {
		t.Fatal(err)
	}

	if err := e.StartInference(title, "", false); err != nil {
		t.Fatalf("start: %v", err)
	}

	reader := e.Subscribe(title)
	var sawIdle, gotErr bool
	var resultContent string
	for !sawIdle {
		res, ok := reader.Wait(waitCtx(t))
		if !ok {
			t.Fatalf("subscription closed before idle")
		}
		if len(res.Errors) > 0 {
			gotErr = true
		}
		for _, evt := range res.Events {
			switch {
			case evt.Type == "error":
				gotErr = true
			case evt.Type == "tool_result" && evt.ToolResult != nil:
				resultContent = evt.ToolResult.Message.Content
			}
		}
		if res.Idle {
			sawIdle = true
		}
	}
	e.WaitForIdle(title)

	if gotErr {
		t.Fatal("boot-default run interrupted with an error event — engine gated like writable, want readonly")
	}
	if resultContent != "tool not found" {
		t.Fatalf("expected 'tool not found' tool_result echo, got %q", resultContent)
	}

	saved, _, _, err := e.ReadChatConsistent(title)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Messages) != 3 || saved.Messages[1].Role != "tool" || saved.Messages[2].Content != "after" {
		t.Fatalf("run must have continued to the follow-up answer, got %#v", saved.Messages)
	}
}
