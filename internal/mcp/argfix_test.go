package mcp

import (
	"context"
	"testing"

	"hschat/internal/model"
)

func TestAutoFixArgs_RenamesAlias(t *testing.T) {
	tool := &model.ToolDef{
		Name:       "replace_content",
		ArgAliases: map[string][]string{"original": {"old"}},
	}
	args := map[string]any{"file": "a.txt", "old": "x", "new": "y"}

	fixes := AutoFixArgs(tool, args)

	if len(fixes) != 1 || fixes[0].Field != "original" || fixes[0].From != "old" {
		t.Fatalf("unexpected fixes: %v", fixes)
	}
	if args["original"] != "x" {
		t.Errorf("expected original=%q, got %v", "x", args["original"])
	}
	if _, ok := args["old"]; ok {
		t.Error("alias key 'old' should have been deleted")
	}
}

func TestAutoFixArgs_CanonicalWins(t *testing.T) {
	tool := &model.ToolDef{
		Name:       "replace_content",
		ArgAliases: map[string][]string{"original": {"old"}},
	}
	args := map[string]any{"original": "canon", "old": "alias"}

	fixes := AutoFixArgs(tool, args)

	if len(fixes) != 0 {
		t.Errorf("expected no fixes, got %v", fixes)
	}
	if args["original"] != "canon" {
		t.Errorf("canonical value was modified: %v", args["original"])
	}
	if _, ok := args["old"]; !ok {
		t.Error("untouched alias should be left as-is")
	}
}

func TestAutoFixArgs_FirstDeclaredAliasWins(t *testing.T) {
	tool := &model.ToolDef{
		Name:       "read_content",
		ArgAliases: map[string][]string{"length": {"limit", "max_lines"}},
	}
	args := map[string]any{"max_lines": 10.0, "limit": 5.0}

	fixes := AutoFixArgs(tool, args)

	if len(fixes) != 1 || fixes[0].From != "limit" {
		t.Fatalf("unexpected fixes: %v", fixes)
	}
	if args["length"] != 5.0 {
		t.Errorf("expected length=5, got %v", args["length"])
	}
	if _, ok := args["max_lines"]; !ok {
		t.Error("unconsumed alias should be left as-is")
	}
}

func TestAutoFixArgs_NoAliasPresent(t *testing.T) {
	tool := &model.ToolDef{
		Name:       "read_content",
		ArgAliases: map[string][]string{"length": {"limit"}},
	}
	args := map[string]any{"file": "a.txt"}

	if fixes := AutoFixArgs(tool, args); len(fixes) != 0 {
		t.Errorf("expected no fixes, got %v", fixes)
	}
	if _, ok := args["length"]; ok {
		t.Error("nothing should have been added")
	}
}

func TestAutoFixArgs_NoopCases(t *testing.T) {
	tool := &model.ToolDef{
		Name:       "read_content",
		ArgAliases: map[string][]string{"length": {"limit"}},
	}
	if fixes := AutoFixArgs(nil, map[string]any{"limit": 1.0}); fixes != nil {
		t.Errorf("nil tool: expected nil fixes, got %v", fixes)
	}
	if fixes := AutoFixArgs(tool, nil); fixes != nil {
		t.Errorf("nil args: expected nil fixes, got %v", fixes)
	}
	plain := &model.ToolDef{Name: "tree"}
	if fixes := AutoFixArgs(plain, map[string]any{"limit": 1.0}); fixes != nil {
		t.Errorf("no alias table: expected nil fixes, got %v", fixes)
	}
}

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
