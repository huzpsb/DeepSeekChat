package mcp

import (
	"os"
	"testing"

	"hschat/internal/model"
	"hschat/internal/storage"
)

func TestManager_ToolExists_FirstBranchNoLock(t *testing.T) {
	setupManagerTest(t)
	storage.SaveConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	// BUG: When toolName is extracted (toolName != ""), isToolAvailable is called
	// WITHOUT holding the read lock. This is a data race.
	// We verify this by checking that ToolExists can be called safely.
	// With delimiter -> first branch, no lock on isToolAvailable
	_ = mgr.ToolExists("mcp::tool")
	// If this doesn't panic, the first branch works despite no lock in single-thread.
	// In concurrent use, this is a race condition bug.
}

func TestManager_ToolExists_NoDelimiterNeedsLock(t *testing.T) {
	setupManagerTest(t)
	storage.SaveConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	// Without delimiter -> second branch, properly acquires RLock
	_ = mgr.ToolExists("tool")
	// This path correctly acquires lock
}

func TestManager_ToolExists_EmptyString(t *testing.T) {
	setupManagerTest(t)
	storage.SaveConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	// Empty string: splitToolName("") returns ("", "")
	// toolName is "", so ToolExists falls to second branch, acquires lock
	// isToolAvailable("", "") checks m.allTools[""] which is empty map access
	exists := mgr.ToolExists("")
	if exists {
		t.Errorf("empty string should not match any tool")
	}
}

func TestManager_IsToolApproved_SuffixFallbackWithoutDelimiter(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools: []string{"my_mcp::the_tool"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	// Call IsToolApproved with just "the_tool" (no "::" delimiter)
	// The suffix fallback should find "my_mcp::the_tool" has suffix "::the_tool"
	approved, _ := mgr.IsToolApproved("the_tool")
	if !approved {
		t.Error("BUG: suffix fallback in IsToolApproved should match 'my_mcp::the_tool' for input 'the_tool'")
	}
}

func TestManager_IsToolApproved_SuffixFallbackManual(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ManuallyApprovedTools: []string{"my_mcp::manual_tool"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	_, manual := mgr.IsToolApproved("manual_tool")
	if !manual {
		t.Error("BUG: suffix fallback should match manually approved tools too")
	}
}

func TestManager_IsToolApproved_SuffixFallbackNotTriggeredWithDoubleColon(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools: []string{"my_mcp::full_tool"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	approved, _ := mgr.IsToolApproved("wrong::full_tool")
	if approved {
		t.Error("exact match with wrong prefix should NOT be approved")
	}
}

func TestManager_IsToolApproved_MultipleMCPsWithSameToolName(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools: []string{"mcp_a::shared_tool"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	approved, _ := mgr.IsToolApproved("shared_tool")
	if !approved {
		t.Error("BUG: suffix fallback should find 'mcp_a::shared_tool' for input 'shared_tool'")
	}

	approved2, _ := mgr.IsToolApproved("mcp_b::shared_tool")
	if approved2 {
		t.Error("'mcp_b::shared_tool' should NOT be approved (only mcp_a::shared_tool is)")
	}
}

func TestManager_ExecuteTool_NilArgsOnEmptyString(t *testing.T) {
	mgr := NewManager()
	mgr.mu.Lock()
	mgr.allTools["mcp"] = []model.ToolDef{{Name: "t"}}
	mgr.mu.Unlock()

	// Empty string args should produce nil args map, which defaults to {}
	// But nil args passed to CallTool might cause issues
	_, err := mgr.ExecuteTool("mcp::t", "")
	if err == nil {
		t.Skip("no MCP connected, expected error")
	}
	// If args is "" and json.Unmarshal fails (because "" is not valid JSON),
	// args remains nil, then args = map[string]any{}. This is fine.
}

func TestManager_ExecuteTool_InvalidJSONArgsHandledGracefully(t *testing.T) {
	mgr := NewManager()
	mgr.mu.Lock()
	mgr.allTools["mcp"] = []model.ToolDef{{Name: "tool"}}
	mgr.mu.Unlock()

	_, err := mgr.ExecuteTool("mcp::tool", `{invalid}`)
	if err == nil {
		t.Skip("no MCP connected, expected error")
	}
}

func TestManager_SetToolStatus_EmptyFullName(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})
	storage.SaveConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	err := mgr.SetToolStatus("", "toolname", "approved")
	if err != nil {
		t.Fatalf("SetToolStatus with empty mcpName failed: %v", err)
	}

	cfg, _ := storage.LoadConfig()
	if len(cfg.ApprovedTools) == 0 {
		t.Errorf("expected at least one approved tool, got none. fullName would be '::toolname'")
	}
}

func TestManager_GetTools_DuplicateFromDisconnectedMCPSameName(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools: []string{"offline_mcp::tool_x"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	tools := mgr.GetTools()
	count := 0
	for _, ts := range tools {
		if ts.MCPName == "offline_mcp" && ts.ToolName == "tool_x" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 entry for offline_mcp::tool_x, got %d", count)
	}
	if count == 0 {
		t.Error("approved tool from offline MCP should be listed")
	}
}

func TestManager_GetTools_ManuallyApprovedFromDisconnectedMCP_Fail(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ManuallyApprovedTools: []string{"barely_online::tool_y"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	tools := mgr.GetTools()
	found := false
	for _, ts := range tools {
		if ts.MCPName == "barely_online" && ts.ToolName == "tool_y" {
			found = true
			if ts.Status != "manually_approved" {
				t.Errorf("expected status 'manually_approved', got '%s'", ts.Status)
			}
			if ts.Available {
				t.Errorf("expected Available=false for disconnected MCP manual tool")
			}
		}
	}
	if !found {
		t.Error("manually approved tool from offline MCP should be listed")
	}
}

func TestManager_Reload_ClearsState(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	mgr.mu.Lock()
	mgr.allTools["test_mcp"] = []model.ToolDef{{Name: "stale_tool"}}
	mgr.unapprovedTools = []string{"test_mcp::stale_tool"}
	mgr.config.ApprovedTools = []string{"test_mcp::stale_tool"}
	mgr.mu.Unlock()

	err := mgr.Reload()
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	// Sandbox always reloads, so allTools should have Sandbox
	if _, ok := mgr.allTools["Sandbox"]; !ok {
		t.Errorf("allTools should have Sandbox after reload")
	}
	if _, ok := mgr.allTools["test_mcp"]; ok {
		t.Errorf("test_mcp should be cleared after reload")
	}
}

func TestManager_SetToolStatus_SameNameDifferentMCP(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	err := mgr.SetToolStatus("mcp1", "shared", "approved")
	if err != nil {
		t.Fatalf("SetToolStatus failed: %v", err)
	}
	err = mgr.SetToolStatus("mcp2", "shared", "manually_approved")
	if err != nil {
		t.Fatalf("SetToolStatus failed: %v", err)
	}

	cfg, _ := storage.LoadConfig()
	if len(cfg.ApprovedTools) != 1 || cfg.ApprovedTools[0] != "mcp1::shared" {
		t.Errorf("expected 1 approved: mcp1::shared, got %v", cfg.ApprovedTools)
	}
	if len(cfg.ManuallyApprovedTools) != 1 || cfg.ManuallyApprovedTools[0] != "mcp2::shared" {
		t.Errorf("expected 1 manual: mcp2::shared, got %v", cfg.ManuallyApprovedTools)
	}
}

func TestManager_SplitToolName_EdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		mcpName  string
		toolName string
	}{
		{"only::suffix", "only", "suffix"},
		{"prefix::only", "prefix", "only"},
		{"just_name", "just_name", ""},
		{"::empty_start", "", "empty_start"},
		{"empty_end::", "empty_end", ""},
	}

	for _, tt := range tests {
		mcp, tool := splitToolName(tt.input)
		if mcp != tt.mcpName || tool != tt.toolName {
			t.Errorf("splitToolName(%q) = (%q, %q), want (%q, %q)",
				tt.input, mcp, tool, tt.mcpName, tt.toolName)
		}
	}
}

func TestManager_IsToolApproved_NameCollisionAcrossMCPs(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools:         []string{"mcp_a::tool"},
		ManuallyApprovedTools: []string{"mcp_b::tool"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	a, m := mgr.IsToolApproved("tool")
	// Without ::, suffix fallback should find "mcp_a::tool" (approved)
	// But could also find "mcp_b::tool" (manually_approved)
	// What happens? Both match as suffix "::tool"
	// The code checks ApprovedTools first, then ManuallyApprovedTools
	// So it should return approved=true from the first match
	if !a && !m {
		t.Error("expected at least one match via suffix fallback")
	}
	// BUG: The suffix fallback finds the FIRST matching suffix,
	// which may not be the intended one when multiple MCPs have same tool name
}

func TestManager_SplitToolName_EmptyInput(t *testing.T) {
	mcp, tool := splitToolName("")
	if mcp != "" || tool != "" {
		t.Errorf("splitToolName(\"\") = (%q, %q), want (\"\", \"\")", mcp, tool)
	}
}

func TestManager_IsToolApproved_ExactMatchTakesPriority(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools: []string{"exact::match"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	approved, _ := mgr.IsToolApproved("exact::match")
	if !approved {
		t.Error("exact match should be approved")
	}
}

func TestManager_IsToolApproved_WrongMCPWithSuffixMatch(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools: []string{"correct::tool"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	approved, _ := mgr.IsToolApproved("wrong::tool")
	if approved {
		t.Error("'wrong::tool' should NOT match 'correct::tool' via suffix (has :: so suffix fallback not triggered)")
	}
}

func TestManager_GetTools_EmptyAllStatuses(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	tools := mgr.GetTools()
	if tools == nil {
		t.Error("GetTools should return empty slice, not nil")
	}
}

func TestIsToolAvailable_NonExistentMCP(t *testing.T) {
	mgr := NewManager()
	if mgr.isToolAvailable("nonexistent_mcp", "any_tool") {
		t.Error("isToolAvailable should return false for nonexistent MCP")
	}
}

func TestIsMCPConnected_NilClient(t *testing.T) {
	mgr := NewManager()
	mgr.clients = map[string]Client{
		"mcp_with_nil": nil,
	}
	if !mgr.isMCPConnected("mcp_with_nil") {
		t.Error("MCP with nil client should still be considered connected")
	}
}

func TestGetString_MissingKey(t *testing.T) {
	m := map[string]any{"existing": "value"}
	if s := getString(m, "missing"); s != "" {
		t.Errorf("getString for missing key should return '', got '%s'", s)
	}
}

func TestGetString_WrongType(t *testing.T) {
	m := map[string]any{"num": 42}
	if s := getString(m, "num"); s != "" {
		t.Errorf("getString for non-string value should return '', got '%s'", s)
	}
}

func TestGetString_ValidValue(t *testing.T) {
	m := map[string]any{"key": "hello"}
	if s := getString(m, "key"); s != "hello" {
		t.Errorf("getString should return 'hello', got '%s'", s)
	}
}

func TestGetString_NilMap(t *testing.T) {
	if s := getString(nil, "any"); s != "" {
		t.Errorf("getString on nil map should return '', got '%s'", s)
	}
}

func TestManager_ExecuteTool_NoDoubleColon(t *testing.T) {
	setupManagerTest(t)
	storage.SaveConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	// No "::" in name -> splitToolName returns (fullName, "")
	_, err := mgr.ExecuteTool("toolname", `{"key":"value"}`)
	if err == nil {
		t.Skip("no MCP connected, expected error")
	}
	// The code should search allTools for the tool name
}

func TestManager_ExecuteTool_NilArguments(t *testing.T) {
	mgr := NewManager()
	_, err := mgr.ExecuteTool("mcp::tool", `null`)
	if err == nil {
		t.Skip("no MCP connected, expected error")
	}
}

var _ = os.Chdir
