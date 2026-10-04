package mcp

import (
	"testing"

	"hschat/internal/model"
)

// Regression test for prefix-cache stability: GetAllowedTools and GetTools
// feed directly into the tools array of every LLM request, and providers
// (DeepSeek Context Caching, Kimi, ...) match the prompt cache on the
// serialized request prefix. Both functions used to range over the
// m.allTools map, whose iteration order Go re-randomizes on every range —
// so the tools array was reshuffled per request (and per restart), turning
// every follow-up turn into a cache miss. The order must now be exactly
// sorted-by-MCP-name, on every call.
func TestManager_ToolListOrder_Deterministic(t *testing.T) {
	setupManagerTest(t)

	mgr := NewManager()
	mgr.mu.Lock()
	mgr.config = &model.MCPConfig{
		ApprovedTools:         []string{"alpha::a", "beta::b", "gamma::c", "delta::d"},
		ManuallyApprovedTools: []string{"omega::o"},
	}
	mgr.allTools = map[string][]model.ToolDef{
		"delta": {{Name: "d"}},
		"alpha": {{Name: "a"}},
		"gamma": {{Name: "c"}},
		"beta":  {{Name: "b"}},
		"omega": {{Name: "o"}},
	}
	mgr.mu.Unlock()

	wantAllowed := []string{"a", "b", "d", "c", "o"} // alpha < beta < delta < gamma < omega

	// Enough iterations to catch a per-range reshuffle with near certainty
	// (map randomization is per-iteration, even for small maps).
	for i := 0; i < 200; i++ {
		got := mgr.GetAllowedTools()
		if len(got) != len(wantAllowed) {
			t.Fatalf("iteration %d: GetAllowedTools returned %d tools, want %d", i, len(got), len(wantAllowed))
		}
		for j := range wantAllowed {
			if got[j].Name != wantAllowed[j] {
				t.Fatalf("iteration %d: GetAllowedTools order not deterministic:\n got  %v\n want %v", i, toolDefNames(got), wantAllowed)
			}
		}

		statuses := mgr.GetTools()
		if len(statuses) != len(wantAllowed) {
			t.Fatalf("iteration %d: GetTools returned %d entries, want %d", i, len(statuses), len(wantAllowed))
		}
		for j := range wantAllowed {
			if statuses[j].ToolName != wantAllowed[j] {
				t.Fatalf("iteration %d: GetTools order not deterministic:\n got  %v\n want %v", i, toolStatusNames(statuses), wantAllowed)
			}
		}
	}
}

// sortedToolServers must return every key exactly once, in sorted order.
func TestSortedToolServers(t *testing.T) {
	all := map[string][]model.ToolDef{
		"zeta":  {{Name: "z"}},
		"beta":  {{Name: "b"}},
		"alpha": {{Name: "a"}},
	}
	got := sortedToolServers(all)
	want := []string{"alpha", "beta", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func toolDefNames(defs []model.ToolDef) []string {
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
}

func toolStatusNames(statuses []ToolStatus) []string {
	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = s.ToolName
	}
	return names
}
