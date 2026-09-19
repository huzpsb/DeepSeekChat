package storage

import (
	"os"
	"strings"
	"testing"

	"hschat/internal/model"
)

// setupSearchDir runs the test in an empty temp dir so the chats/ store is
// isolated, matching the convention of the other storage tests.
func setupSearchDir(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })
}

func TestSearchChats_CaseInsensitiveContentMatch(t *testing.T) {
	setupSearchDir(t)
	SaveChat(&model.Chat{
		Title: "alpha",
		Messages: []model.Message{
			{Role: "user", Content: "Hello Needle world", SendToServer: true},
		},
	})
	SaveChat(&model.Chat{
		Title: "beta",
		Messages: []model.Message{
			{Role: "user", Content: "nothing here", SendToServer: true},
		},
	})

	results := SearchChats("needle", false, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Title != "alpha" || r.TotalHits != 1 || len(r.Hits) != 1 {
		t.Fatalf("unexpected result: %+v", r)
	}
	h := r.Hits[0]
	if h.Index != 0 || h.Role != "user" || h.Field != model.SearchFieldContent || h.Count != 1 {
		t.Errorf("unexpected hit: %+v", h)
	}
	if !strings.Contains(h.Snippet, "Needle") {
		t.Errorf("snippet should contain the original-case match: %q", h.Snippet)
	}
}

func TestSearchChats_ToolScope(t *testing.T) {
	setupSearchDir(t)
	SaveChat(&model.Chat{
		Title: "chat",
		Messages: []model.Message{
			{Role: "assistant", Content: "let me check", ToolCalls: []model.ToolCall{
				{ID: "1", Function: model.FunctionCall{Name: "read_file", Arguments: `{"path":"needle.txt"}`}},
			}, SendToServer: true},
			{Role: "tool", Name: "read_file", ToolCallID: "1", Content: "needle in tool result", SendToServer: true},
		},
	})

	// Without tool scope: neither the tool result nor the call arguments match.
	results := SearchChats("needle", false, 10)
	if len(results) != 0 {
		t.Fatalf("expected 0 results without tool scope, got %d", len(results))
	}

	// With tool scope: tool_call + tool result both hit.
	results = SearchChats("needle", true, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result with tool scope, got %d", len(results))
	}
	r := results[0]
	if r.TotalHits != 2 || len(r.Hits) != 2 {
		t.Fatalf("expected 2 hits, got total=%d hits=%d", r.TotalHits, len(r.Hits))
	}
	if r.Hits[0].Field != model.SearchFieldToolCall || r.Hits[0].Index != 0 {
		t.Errorf("unexpected first hit: %+v", r.Hits[0])
	}
	if r.Hits[1].Field != model.SearchFieldContent || r.Hits[1].Index != 1 || r.Hits[1].Role != "tool" {
		t.Errorf("unexpected second hit: %+v", r.Hits[1])
	}
}

func TestSearchChats_TruncationKeepsLastHits(t *testing.T) {
	setupSearchDir(t)
	// 13 messages, one occurrence each: indexes 0..12.
	msgs := make([]model.Message, 13)
	for i := range msgs {
		msgs[i] = model.Message{Role: "user", Content: "prefix needle", SendToServer: true}
	}
	SaveChat(&model.Chat{Title: "long", Messages: msgs})

	results := SearchChats("needle", false, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if !r.Truncated {
		t.Error("expected Truncated to be set")
	}
	if r.TotalHits != 13 {
		t.Errorf("expected TotalHits=13, got %d", r.TotalHits)
	}
	if len(r.Hits) != 10 {
		t.Fatalf("expected 10 kept hits, got %d", len(r.Hits))
	}
	// later hits have priority: message indexes 3..12 are kept
	for i, h := range r.Hits {
		if h.Index != 3+i {
			t.Fatalf("expected hit %d at message index %d, got %d", i, 3+i, h.Index)
		}
	}
}

func TestSearchChats_MultipleHitsInOneMessage(t *testing.T) {
	setupSearchDir(t)
	// Far apart: three separate hits in the same message.
	filler := strings.Repeat("x", 60)
	SaveChat(&model.Chat{
		Title: "spread",
		Messages: []model.Message{
			{Role: "assistant", Content: "needle " + filler + " needle " + filler + " needle", SendToServer: true},
		},
	})
	results := SearchChats("needle", false, 10)
	if len(results) != 1 || results[0].TotalHits != 3 {
		t.Fatalf("expected 3 occurrences, got %+v", results)
	}
	if len(results[0].Hits) != 3 {
		t.Fatalf("expected 3 separate hits (too far apart to merge), got %d", len(results[0].Hits))
	}
	for _, h := range results[0].Hits {
		if h.Index != 0 || h.Count != 1 {
			t.Errorf("unexpected hit: %+v", h)
		}
	}

	// Adjacent: merged into one hit with Count=3.
	setupSearchDir(t)
	SaveChat(&model.Chat{
		Title: "adjacent",
		Messages: []model.Message{
			{Role: "assistant", Content: "needle needle needle", SendToServer: true},
		},
	})
	results = SearchChats("needle", false, 10)
	if len(results) != 1 || results[0].TotalHits != 3 {
		t.Fatalf("expected 3 occurrences, got %+v", results)
	}
	if len(results[0].Hits) != 1 || results[0].Hits[0].Count != 3 {
		t.Fatalf("expected merged hit with Count=3, got %+v", results[0].Hits)
	}
}

func TestSearchChats_TitleOnlyMatch(t *testing.T) {
	setupSearchDir(t)
	SaveChat(&model.Chat{
		Title: "needle chat",
		Messages: []model.Message{
			{Role: "user", Content: "unrelated", SendToServer: true},
		},
	})

	results := SearchChats("NEEDLE", false, 10)
	if len(results) != 1 {
		t.Fatalf("expected title-only match, got %d results", len(results))
	}
	r := results[0]
	if !r.TitleMatch || r.TotalHits != 0 || len(r.Hits) != 0 || r.Truncated {
		t.Errorf("unexpected result: %+v", r)
	}
}

func TestSearchChats_EmptyQuery(t *testing.T) {
	setupSearchDir(t)
	if results := SearchChats("   ", false, 10); results != nil {
		t.Errorf("expected nil for blank query, got %+v", results)
	}
}
