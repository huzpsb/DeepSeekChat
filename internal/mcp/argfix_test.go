package mcp

import (
	"context"
	"testing"

	"hschat/internal/model"
)

// fakeClient captures the args passed to CallTool.
type fakeClient struct {
	gotName string
	gotArgs map[string]any
}

func (f *fakeClient) Initialize() error { return nil }
func (f *fakeClient) ListTools() ([]model.ToolDef, error) {
	return nil, nil
}
func (f *fakeClient) CallTool(_ context.Context, name string, args map[string]any) (*model.ToolResult, error) {
	f.gotName = name
	f.gotArgs = args
	return &model.ToolResult{Content: []model.ToolContent{{Type: "text", Text: "ok"}}}, nil
}
func (f *fakeClient) Close() error { return nil }
func (f *fakeClient) Name() string { return "fake" }

func TestManager_ExecuteTool_AutoFixesArgs(t *testing.T) {
	mgr := NewManager()
	fc := &fakeClient{}
	mgr.mu.Lock()
	mgr.clients["Sandbox"] = fc
	mgr.allTools["Sandbox"] = []model.ToolDef{{
		Name:       "replace_content",
		ArgAliases: map[string][]string{"original": {"old"}},
	}}
	mgr.mu.Unlock()

	_, err := mgr.ExecuteTool(context.Background(), "Sandbox::replace_content",
		`{"file":"a.txt","old":"x","new":"y"}`)
	if err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}
	if fc.gotName != "replace_content" {
		t.Errorf("expected tool name replace_content, got %q", fc.gotName)
	}
	if fc.gotArgs["original"] != "x" {
		t.Errorf("expected original=%q, got %v", "x", fc.gotArgs["original"])
	}
	if _, ok := fc.gotArgs["old"]; ok {
		t.Error("alias key 'old' should have been deleted before CallTool")
	}
}

func TestManager_ExecuteTool_AutoFixBareName(t *testing.T) {
	mgr := NewManager()
	fc := &fakeClient{}
	mgr.mu.Lock()
	mgr.clients["Sandbox"] = fc
	mgr.allTools["Sandbox"] = []model.ToolDef{{
		Name:       "read_content",
		ArgAliases: map[string][]string{"length": {"limit"}},
	}}
	mgr.mu.Unlock()

	// Bare tool name (no "::") resolves through the allTools scan; the
	// auto-fix must apply on that path too.
	_, err := mgr.ExecuteTool(context.Background(), "read_content",
		`{"file":"a.txt","limit":42}`)
	if err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}
	if fc.gotArgs["length"] != 42.0 {
		t.Errorf("expected length=42, got %v", fc.gotArgs["length"])
	}
}

// Reproduces the real-world call seen in chats/2026-08-26 205037.json:
// {"file": "...", "limit": "30"} — wrong key AND string-typed value.
// The fix must rename limit->length AND coerce "30"->30.0, or the
// builtin's .(float64) assertion would silently fall back to the default.
func TestManager_ExecuteTool_AutoFixAliasAndType(t *testing.T) {
	mgr := NewManager()
	fc := &fakeClient{}
	mgr.mu.Lock()
	mgr.clients["Sandbox"] = fc
	mgr.allTools["Sandbox"] = []model.ToolDef{{
		Name: "read_content",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file":   map[string]any{"type": "string"},
				"length": map[string]any{"type": "integer"},
			},
		},
		ArgAliases: map[string][]string{"length": {"limit"}},
	}}
	mgr.mu.Unlock()

	_, err := mgr.ExecuteTool(context.Background(), "Sandbox::read_content",
		`{"file":"a.txt","limit":"30"}`)
	if err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}
	if fc.gotArgs["length"] != 30.0 {
		t.Errorf("expected length=30.0 (float64), got %#v", fc.gotArgs["length"])
	}
	if _, ok := fc.gotArgs["limit"]; ok {
		t.Error("alias key 'limit' should have been deleted before CallTool")
	}
}
