package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"hschat/internal/model"
)

func TestBuildAPIMessages_FiltersSendToServerFalse(t *testing.T) {
	messages := []model.Message{
		{Role: "user", Content: "visible", SendToServer: true},
		{Role: "user", Content: "hidden", SendToServer: false},
		{Role: "assistant", Content: "also_hidden", SendToServer: false},
		{Role: "user", Content: "visible2", SendToServer: true},
	}

	result := buildAPIMessages(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d (bug: SendToServer=false messages filtered out)", len(result))
	}
	if result[0]["content"] != "visible" {
		t.Errorf("first message content mismatch: %v", result[0]["content"])
	}
	if result[1]["content"] != "visible2" {
		t.Errorf("second message content mismatch: %v", result[1]["content"])
	}
}

func TestBuildAPIMessages_ToolRoleAlwaysHasContent(t *testing.T) {
	messages := []model.Message{
		{Role: "user", Content: "hi", SendToServer: true},
		{Role: "tool", Content: "", ToolCallID: "call_1", Name: "my_tool", SendToServer: true},
	}

	result := buildAPIMessages(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	toolMsg := result[1]
	content, hasContent := toolMsg["content"]
	if !hasContent {
		t.Fatal("tool message should always have content key, even for empty string")
	}
	if content != "" {
		t.Errorf("content should be empty string, got %v (verifying tool always has content field)", content)
	}
	if toolMsg["role"] != "tool" {
		t.Errorf("expected role 'tool', got %v", toolMsg["role"])
	}
}

func TestBuildAPIMessages_UserRoleWithEmptyContentSkipsContent(t *testing.T) {
	messages := []model.Message{
		{Role: "user", Content: "", SendToServer: true},
	}

	result := buildAPIMessages(messages)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	_, hasContent := result[0]["content"]
	if hasContent {
		t.Error("user message with empty content should NOT have content key (but it might)")
	}
}

func TestBuildAPIMessages_AssistantToolCallsSerialized(t *testing.T) {
	messages := []model.Message{
		{Role: "user", Content: "do it", SendToServer: true},
		{
			Role: "assistant", Content: "ok",
			ToolCalls: []model.ToolCall{
				{ID: "tc1", Function: model.FunctionCall{Name: "read", Arguments: `{"path":"/tmp"}`}},
				{ID: "tc2", Function: model.FunctionCall{Name: "write", Arguments: `{"path":"/tmp","data":"x"}`}},
			},
			SendToServer: true,
		},
	}

	result := buildAPIMessages(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	assistantMsg := result[1]
	tcs, ok := assistantMsg["tool_calls"].([]map[string]any)
	if !ok {
		t.Fatal("assistant message should have tool_calls")
	}
	if len(tcs) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(tcs))
	}
}

func TestBuildAPIMessages_ReasoningContentIncluded(t *testing.T) {
	messages := []model.Message{
		{Role: "user", Content: "think", SendToServer: true},
		{Role: "assistant", ReasoningContent: "I need to...", SendToServer: true},
	}

	result := buildAPIMessages(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	assistantMsg := result[1]
	rc, ok := assistantMsg["reasoning_content"]
	if !ok {
		t.Fatal("assistant message should include reasoning_content")
	}
	if rc != "I need to..." {
		t.Errorf("reasoning_content mismatch: %v", rc)
	}
}

func TestParseSSE_MalformedJSONDoesNotCrash(t *testing.T) {
	input := `data: {not valid json at all}

data: {"choices":[{"delta":{"content":"hello"}}]}

`

	var events []StreamEvent
	parseSSE(strings.NewReader(input), func(evt StreamEvent) {
		events = append(events, evt)
	})

	foundDelta := false
	for _, e := range events {
		if e.Type == "delta" && e.Content == "hello" {
			foundDelta = true
		}
	}
	if !foundDelta {
		t.Error("valid delta after malformed line should have been parsed")
	}
}

func TestParseSSE_EmitsDoneOnScannerError(t *testing.T) {
	// parseSSE always emits a "done" event at the end, even after scanner errors
	// The scanner silently continues past errors
	input := `data: [DONE]
`

	var events []StreamEvent
	parseSSE(strings.NewReader(input), func(evt StreamEvent) {
		events = append(events, evt)
	})

	if len(events) != 1 || events[0].Type != "done" {
		t.Errorf("expected single [DONE] event, got %d events: %+v", len(events), events)
	}
}

func TestParseSSE_EmptyInputEmitsDone(t *testing.T) {
	var events []StreamEvent
	parseSSE(strings.NewReader(""), func(evt StreamEvent) {
		events = append(events, evt)
	})

	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event (done), got %d", len(events))
	}
	if events[0].Type != "done" {
		t.Errorf("expected 'done' event, got '%s'", events[0].Type)
	}
}

func TestParseSSE_ToolCallAccumulatesArgs(t *testing.T) {
	input := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"file.txt\"}"}}]}}]}

`

	var events []StreamEvent
	parseSSE(strings.NewReader(input), func(evt StreamEvent) {
		events = append(events, evt)
	})

	// Should have at least one tool_call event with accumulated args
	toolCalls := 0
	for _, e := range events {
		if e.Type == "tool_call" {
			toolCalls++
			if e.ID != "call_1" {
				t.Errorf("expected ID 'call_1', got '%s'", e.ID)
			}
			if e.Name != "read" {
				t.Errorf("expected Name 'read', got '%s'", e.Name)
			}
			// Last emission should have full args
		}
	}
	if toolCalls == 0 {
		t.Error("expected at least one tool_call event")
	}
}

func TestParseSSE_ToolCallResetsOnNewID(t *testing.T) {
	input := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"tool_a"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"tool_b"}}]}}]}

`

	var events []StreamEvent
	parseSSE(strings.NewReader(input), func(evt StreamEvent) {
		events = append(events, evt)
	})

	var seenIDs []string
	var seenNames []string
	for _, e := range events {
		if e.Type == "tool_call" {
			seenIDs = append(seenIDs, e.ID)
			seenNames = append(seenNames, e.Name)
		}
	}
	if len(seenIDs) < 2 {
		t.Fatalf("expected at least 2 tool_call events, got %d", len(seenIDs))
	}
	if seenIDs[0] != "call_1" || seenNames[0] != "tool_a" {
		t.Errorf("first tool call wrong: id=%s name=%s", seenIDs[0], seenNames[0])
	}
	if seenIDs[len(seenIDs)-1] != "call_2" || seenNames[len(seenNames)-1] != "tool_b" {
		t.Errorf("second tool call wrong: id=%s name=%s", seenIDs[len(seenIDs)-1], seenNames[len(seenNames)-1])
	}
}

func TestParseSSE_CommentLinesAreSkipped(t *testing.T) {
	input := `: this is a comment
data: {"choices":[{"delta":{"content":"hello"}}]}

`

	var events []StreamEvent
	parseSSE(strings.NewReader(input), func(evt StreamEvent) {
		events = append(events, evt)
	})

	if len(events) < 1 {
		t.Fatal("expected at least the delta event + done")
	}
	found := false
	for _, e := range events {
		if e.Type == "delta" && e.Content == "hello" {
			found = true
		}
	}
	if !found {
		t.Error("delta event not found after comment line")
	}
}

func TestParseSSE_ChoicesEmptyDoesNotCrash(t *testing.T) {
	input := `data: {"choices":[]}

data: {"choices":[{"delta":{"content":"ok"}}]}

`

	var events []StreamEvent
	parseSSE(strings.NewReader(input), func(evt StreamEvent) {
		events = append(events, evt)
	})

	found := false
	for _, e := range events {
		if e.Type == "delta" && e.Content == "ok" {
			found = true
		}
	}
	if !found {
		t.Error("delta event after empty choices should have been parsed")
	}
}

func TestBuildAPIMessages_NilToolsListProducesNilField(t *testing.T) {
	// When tools list is nil, the "tools" field should not be in the request body
	messages := []model.Message{
		{Role: "system", Content: "you are helpful", SendToServer: true},
	}
	result := buildAPIMessages(messages)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0]["role"] != "system" {
		t.Errorf("expected role 'system', got %v", result[0]["role"])
	}
}

func TestParseSSE_ReasoningDelta(t *testing.T) {
	input := `data: {"choices":[{"delta":{"reasoning_content":"I am thinking..."}}]}

`

	var events []StreamEvent
	parseSSE(strings.NewReader(input), func(evt StreamEvent) {
		events = append(events, evt)
	})

	found := false
	for _, e := range events {
		if e.Type == "reasoning_delta" && e.Content == "I am thinking..." {
			found = true
		}
	}
	if !found {
		t.Error("reasoning_delta event should have been emitted")
	}
}

func TestParseSSE_RoleDelta(t *testing.T) {
	input := `data: {"choices":[{"delta":{"role":"assistant"}}]}

data: {"choices":[{"delta":{"content":"Hello!"}}]}

`

	var events []StreamEvent
	parseSSE(strings.NewReader(input), func(evt StreamEvent) {
		events = append(events, evt)
	})

	found := false
	for _, e := range events {
		if e.Type == "delta" && e.Content == "Hello!" {
			found = true
		}
	}
	if !found {
		t.Error("delta event with content should be emitted after role delta")
	}
}

func TestParseSSE_NonDataLinesAreSkipped(t *testing.T) {
	input := `event: ping

data: {"choices":[{"delta":{"content":"yes"}}]}

`

	var events []StreamEvent
	parseSSE(strings.NewReader(input), func(evt StreamEvent) {
		events = append(events, evt)
	})

	found := 0
	for _, e := range events {
		if e.Type == "delta" && e.Content == "yes" {
			found++
		}
	}
	if found != 1 {
		t.Errorf("expected 1 delta event, got %d", found)
	}
}

func TestBuildAPIMessages_EmptyMessagesList(t *testing.T) {
	result := buildAPIMessages(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result))
	}
}

func TestBuildAPIMessages_MessageWithNoContentAndNotEmptyRole(t *testing.T) {
	messages := []model.Message{
		{Role: "assistant", Content: "", SendToServer: true},
	}
	result := buildAPIMessages(messages)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	_, hasContent := result[0]["content"]
	if hasContent {
		t.Error("assistant with empty content should not have content key")
	}
}

func TestParseSSE_PartialToolCallAccumulatedState(t *testing.T) {
	// Simulates: tool_call ID arrives, name arrives, but arguments never complete
	// Then a new ID arrives - should reset
	input := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_x","type":"function","function":{"name":"tool_x"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"partial..."}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_y","type":"function","function":{"name":"tool_y"}}]}}]}

`

	var events []StreamEvent
	parseSSE(strings.NewReader(input), func(evt StreamEvent) {
		events = append(events, evt)
	})

	// After call_y arrives, call_y events should NOT have call_x's accumulated args
	for _, e := range events {
		if e.Type == "tool_call" && e.ID == "call_y" && e.Name == "tool_y" {
			if strings.Contains(e.Args, "partial") {
				t.Error("BUG: accumulated args from call_x leaked into call_y after ID reset")
			}
		}
	}
}

func TestStreamChat_RetriesOn429ThenSucceeds(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n <= 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "model")
	var events []StreamEvent
	err := client.StreamChat(context.Background(), []model.Message{
		{Role: "user", Content: "hello", SendToServer: true},
	}, nil, func(evt StreamEvent) {
		events = append(events, evt)
	})

	if err != nil {
		t.Fatalf("expected success after retries, got error: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 4 {
		t.Errorf("expected 4 attempts (3 retries), got %d", got)
	}
	found := false
	for _, e := range events {
		if e.Type == "delta" && e.Content == "hi" {
			found = true
		}
	}
	if !found {
		t.Error("expected delta event after successful retry")
	}
}

func TestStreamChat_GivesUpAfter5RetriesOn429(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "model")
	err := client.StreamChat(context.Background(), []model.Message{
		{Role: "user", Content: "hello", SendToServer: true},
	}, nil, func(evt StreamEvent) {})

	if err == nil {
		t.Fatal("expected error when server always returns 429")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should mention 429, got: %v", err)
	}
	// 1 initial request + 5 retries = 6 attempts total; a 7th must NOT happen
	if got := atomic.LoadInt32(&attempts); got != 6 {
		t.Errorf("expected exactly 6 attempts (5 retries then break), got %d", got)
	}
}

func TestStreamChat_Non429ErrorDoesNotRetry(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "model")
	err := client.StreamChat(context.Background(), []model.Message{
		{Role: "user", Content: "hello", SendToServer: true},
	}, nil, func(evt StreamEvent) {})

	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("expected exactly 1 attempt for non-429 error, got %d", got)
	}
}
