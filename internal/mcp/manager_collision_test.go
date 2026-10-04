package mcp

import (
	"context"
	"fmt"
	"testing"

	"hschat/internal/model"
)

// fakeCollisionClient records which MCP the manager dispatched a bare tool
// call to.
type fakeCollisionClient struct {
	name  string
	calls []string
}

func (f *fakeCollisionClient) Initialize() error { return nil }
func (f *fakeCollisionClient) ListTools() ([]model.ToolDef, error) {
	return nil, nil
}
func (f *fakeCollisionClient) Name() string { return f.name }
func (f *fakeCollisionClient) Close() error { return nil }
func (f *fakeCollisionClient) CallTool(_ context.Context, tool string, _ map[string]any) (*model.ToolResult, error) {
	f.calls = append(f.calls, tool)
	return &model.ToolResult{Content: []model.ToolContent{{
		Type: "text",
		Text: fmt.Sprintf("dispatched via %s", f.name),
	}}}, nil
}

// seedCollisionManager builds a Manager where MCPs "aaa" and "bbb" both
// expose a tool named "search" (with distinguishable descriptions), and
// wires up fake clients so ExecuteTool dispatch is observable.
func seedCollisionManager(t *testing.T, approvedTools []string) (*Manager, *fakeCollisionClient, *fakeCollisionClient) {
	t.Helper()
	setupManagerTest(t)

	aaa := &fakeCollisionClient{name: "aaa"}
	bbb := &fakeCollisionClient{name: "bbb"}

	mgr := NewManager()
	mgr.mu.Lock()
	mgr.config = &model.MCPConfig{ApprovedTools: approvedTools}
	mgr.clients = map[string]Client{"aaa": aaa, "bbb": bbb}
	mgr.allTools = map[string][]model.ToolDef{
		"aaa": {{Name: "search", Description: "from aaa"}},
		"bbb": {{Name: "search", Description: "from bbb"}},
	}
	mgr.mu.Unlock()
	return mgr, aaa, bbb
}

// Both MCPs approved: the tools array must contain exactly ONE "search"
// (the lower-named aaa's), and a bare "search" call must dispatch to aaa.
func TestManager_BareNameCollision_LowerNameWins(t *testing.T) {
	mgr, aaa, bbb := seedCollisionManager(t, []string{"aaa::search", "bbb::search"})

	for i := 0; i < 50; i++ {
		allowed := mgr.GetAllowedTools()
		if len(allowed) != 1 {
			t.Fatalf("iteration %d: expected exactly 1 allowed tool on collision, got %d", i, len(allowed))
		}
		if allowed[0].Description != "from aaa" {
			t.Fatalf("iteration %d: expected aaa's def to win, got %q", i, allowed[0].Description)
		}
	}

	def := mgr.GetToolDef("search")
	if def == nil || def.Description != "from aaa" {
		t.Fatalf("GetToolDef(bare) must resolve to aaa's def, got %+v", def)
	}

	if _, err := mgr.ExecuteTool(context.Background(), "search", `{}`); err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}
	if len(aaa.calls) != 1 || len(bbb.calls) != 0 {
		t.Fatalf("bare call must dispatch to aaa only: aaa=%v bbb=%v", aaa.calls, bbb.calls)
	}

	// Full names stay unambiguous and unaffected by the shadowing.
	if _, err := mgr.ExecuteTool(context.Background(), "bbb::search", `{}`); err != nil {
		t.Fatalf("ExecuteTool(bbb::search) failed: %v", err)
	}
	if len(bbb.calls) != 1 {
		t.Fatalf("full-name call must reach bbb: bbb=%v", bbb.calls)
	}
}

// Only the higher-named MCP's tool is approved: no collision in the allowed
// set, so bbb's def is exposed and the bare call must go to bbb (the tool
// the LLM actually saw) — not to the lower-named but unapproved aaa.
func TestManager_BareNameCollision_OnlyAllowedCandidateWins(t *testing.T) {
	mgr, aaa, bbb := seedCollisionManager(t, []string{"bbb::search"})

	allowed := mgr.GetAllowedTools()
	if len(allowed) != 1 || allowed[0].Description != "from bbb" {
		t.Fatalf("expected only bbb's def allowed, got %+v", allowed)
	}

	def := mgr.GetToolDef("search")
	if def == nil || def.Description != "from bbb" {
		t.Fatalf("GetToolDef(bare) must resolve to the allowed bbb def, got %+v", def)
	}

	if _, err := mgr.ExecuteTool(context.Background(), "search", `{}`); err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}
	if len(bbb.calls) != 1 || len(aaa.calls) != 0 {
		t.Fatalf("bare call must dispatch to the allowed bbb: aaa=%v bbb=%v", aaa.calls, bbb.calls)
	}
}

// No candidate approved (non-gated caller, e.g. unit tests / CLI before
// approval): resolution still deterministic — lowest name wins, never map
// order.
func TestManager_BareNameCollision_FallbackDeterministic(t *testing.T) {
	mgr, aaa, bbb := seedCollisionManager(t, nil)

	for i := 0; i < 50; i++ {
		def := mgr.GetToolDef("search")
		if def == nil || def.Description != "from aaa" {
			t.Fatalf("iteration %d: fallback must deterministically pick aaa, got %+v", i, def)
		}
	}
	if len(aaa.calls) != 0 || len(bbb.calls) != 0 {
		t.Fatalf("no calls expected yet")
	}
}

// warnBareNameConflicts must name both sides and the winner explicitly.
// Assert via the observable piece: it must not panic and must tolerate a
// server with no conflicts at all.
func TestManager_WarnBareNameConflicts_NoConflict(t *testing.T) {
	setupManagerTest(t)
	mgr := NewManager()
	mgr.mu.Lock()
	mgr.allTools = map[string][]model.ToolDef{
		"aaa": {{Name: "x"}},
		"bbb": {{Name: "y"}},
	}
	mgr.warnBareNameConflicts("bbb")
	mgr.mu.Unlock()
}
