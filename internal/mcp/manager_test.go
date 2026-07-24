package mcp

import (
	"os"
	"testing"

	"hschat/internal/model"
	"hschat/internal/storage"
)

func setupManagerTest(t *testing.T) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })
	return tmpDir, origDir
}

func seedConfig(cfg *model.MCPConfig) error {
	return storage.SaveConfig(cfg)
}

func TestManager_LoadAndConnect_EmptyConfig(t *testing.T) {
	setupManagerTest(t)
	storage.SaveConfig(&model.MCPConfig{})

	mgr := NewManager()
	err := mgr.LoadAndConnect()
	if err != nil {
		t.Fatalf("LoadAndConnect failed: %v", err)
	}
	if mgr.config == nil {
		t.Fatalf("expected config to be loaded")
	}
	if len(mgr.clients) != 2 {
		t.Errorf("expected 2 clients (Sandbox + AskUser), got %d", len(mgr.clients))
	}
}

func TestManager_LoadAndConnect_NoServers(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		APIKey:                "test-key",
		MCPServers:            []model.MCPServer{},
		ApprovedTools:         nil,
		ManuallyApprovedTools: nil,
	})

	mgr := NewManager()
	err := mgr.LoadAndConnect()
	if err != nil {
		t.Fatalf("LoadAndConnect failed: %v", err)
	}
	if mgr.config.APIKey != "test-key" {
		t.Errorf("expected 'test-key', got '%s'", mgr.config.APIKey)
	}
}

func TestManager_GetTools_Empty(t *testing.T) {
	setupManagerTest(t)
	storage.SaveConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()
	tools := mgr.GetTools()
	if len(tools) != 11 {
		t.Errorf("expected 11 builtin tools, got %d", len(tools))
	}
}

func TestManager_AskUserCannotBeApproved(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	if err := mgr.SetToolStatus("AskUser", "ask_user", "approved"); err == nil {
		t.Fatalf("expected approving ask_user to fail")
	}

	if err := mgr.SetToolStatus("AskUser", "ask_user", "manually_approved"); err != nil {
		t.Fatalf("expected manually approving ask_user to succeed: %v", err)
	}
}

func TestManager_SetToolStatus(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools:         []string{},
		ManuallyApprovedTools: []string{},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	// Use a tool from an unconnected MCP (tool status for disconnected MCPs)
	// Since no MCPs are connected, this is purely config manipulation
	err := mgr.SetToolStatus("test_mcp", "tool_a", "approved")
	if err != nil {
		t.Fatalf("SetToolStatus failed: %v", err)
	}

	err = mgr.SetToolStatus("test_mcp", "tool_b", "manually_approved")
	if err != nil {
		t.Fatalf("SetToolStatus failed: %v", err)
	}

	// Verify via config reload
	cfg, _ := storage.LoadConfig()
	if len(cfg.ApprovedTools) != 1 {
		t.Fatalf("expected 1 approved tool, got %d", len(cfg.ApprovedTools))
	}
	if cfg.ApprovedTools[0] != "test_mcp::tool_a" {
		t.Errorf("expected 'test_mcp::tool_a', got '%s'", cfg.ApprovedTools[0])
	}
	if len(cfg.ManuallyApprovedTools) != 1 {
		t.Fatalf("expected 1 manually approved tool, got %d", len(cfg.ManuallyApprovedTools))
	}
	if cfg.ManuallyApprovedTools[0] != "test_mcp::tool_b" {
		t.Errorf("expected 'test_mcp::tool_b', got '%s'", cfg.ManuallyApprovedTools[0])
	}
}

func TestManager_SetToolStatus_InvalidStatus(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	err := mgr.SetToolStatus("mcp", "tool", "invalid")
	if err == nil {
		t.Errorf("expected error for invalid status")
	}
}

func TestManager_SetToolStatus_Switch(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	// Add as approved
	mgr.SetToolStatus("m", "t", "approved")
	cfg, _ := storage.LoadConfig()
	if len(cfg.ApprovedTools) != 1 || cfg.ApprovedTools[0] != "m::t" {
		t.Errorf("expected approved tool")
	}

	// Switch to manually_approved
	mgr.SetToolStatus("m", "t", "manually_approved")
	cfg, _ = storage.LoadConfig()
	if len(cfg.ApprovedTools) != 0 {
		t.Errorf("expected 0 approved after switch to manual")
	}
	if len(cfg.ManuallyApprovedTools) != 1 || cfg.ManuallyApprovedTools[0] != "m::t" {
		t.Errorf("expected 1 manually approved")
	}

	// Switch back
	mgr.SetToolStatus("m", "t", "approved")
	cfg, _ = storage.LoadConfig()
	if len(cfg.ApprovedTools) != 1 || cfg.ApprovedTools[0] != "m::t" {
		t.Errorf("expected approved tool back")
	}
	if len(cfg.ManuallyApprovedTools) != 0 {
		t.Errorf("expected 0 manually approved")
	}

	// Remove
	mgr.SetToolStatus("m", "t", "unapproved")
	cfg, _ = storage.LoadConfig()
	if len(cfg.ApprovedTools) != 0 || len(cfg.ManuallyApprovedTools) != 0 {
		t.Errorf("expected both lists empty")
	}
}

func TestManager_SetToolStatus_ConflictResolution(t *testing.T) {
	// If a tool is in both approved and manually_approved after switch, manual wins
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools:         []string{"mcp::multi"},
		ManuallyApprovedTools: []string{"mcp::multi"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	// After reconcileTools, approved should have been cleaned
	cfg, _ := storage.LoadConfig()
	if len(cfg.ApprovedTools) != 0 {
		t.Errorf("approved should be empty after reconcile, got %v", cfg.ApprovedTools)
	}
}

func TestManager_IsToolApproved(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools:         []string{"mcp::approved_tool"},
		ManuallyApprovedTools: []string{"mcp::manual_tool"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	approved, manual := mgr.IsToolApproved("mcp::approved_tool")
	if !approved || manual {
		t.Errorf("expected approved=true, manual=false, got %v, %v", approved, manual)
	}

	approved, manual = mgr.IsToolApproved("mcp::manual_tool")
	if approved || !manual {
		t.Errorf("expected approved=false, manual=true, got %v, %v", approved, manual)
	}

	approved, manual = mgr.IsToolApproved("mcp::unknown_tool")
	if approved || manual {
		t.Errorf("expected approved=false, manual=false for unknown, got %v, %v", approved, manual)
	}
}

func TestManager_IsToolApprovedByName(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools: []string{"my_mcp::safe_tool"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	approved, manual := mgr.IsToolApprovedByName("my_mcp", "safe_tool")
	if !approved || manual {
		t.Errorf("expected approved=true")
	}
}

func TestManager_ToolExists(t *testing.T) {
	setupManagerTest(t)
	storage.SaveConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	if !mgr.ToolExists("Sandbox::tree") {
		t.Errorf("expected Sandbox::tree to exist")
	}
	if mgr.ToolExists("any_mcp::any_tool") {
		t.Errorf("expected tool not to exist without connections")
	}
}

func TestManager_ExecuteTool_NotConnected(t *testing.T) {
	setupManagerTest(t)
	storage.SaveConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	_, err := mgr.ExecuteTool("nonexistent::tool", `{}`)
	if err == nil {
		t.Errorf("expected error for unconnected MCP")
	}
}

func TestManager_ExecuteTool_EmptyArgs(t *testing.T) {
	setupManagerTest(t)
	storage.SaveConfig(&model.MCPConfig{})

	mgr := NewManager()
	// No actual MCP connected, so this will fail with "not connected"
	_, err := mgr.ExecuteTool("mcp::tool", "")
	if err == nil {
		t.Errorf("expected error")
	}
}

func TestManager_ExecuteTool_BadJSON(t *testing.T) {
	setupManagerTest(t)
	storage.SaveConfig(&model.MCPConfig{})

	mgr := NewManager()
	_, err := mgr.ExecuteTool("mcp::t", "not json")
	if err == nil {
		t.Errorf("expected error")
	}
}

func TestSplitToolName(t *testing.T) {
	tests := []struct {
		input    string
		mcpName  string
		toolName string
	}{
		{"mcp::tool", "mcp", "tool"},
		{"no_delimiter", "no_delimiter", ""},
		{"a::b::c", "a", "b::c"},
		{"::", "", ""},
		{"", "", ""},
	}

	for _, tt := range tests {
		mcpName, toolName := splitToolName(tt.input)
		if mcpName != tt.mcpName || toolName != tt.toolName {
			t.Errorf("splitToolName(%q) = (%q, %q), want (%q, %q)",
				tt.input, mcpName, toolName, tt.mcpName, tt.toolName)
		}
	}
}

func TestManager_Reload_Empty(t *testing.T) {
	setupManagerTest(t)
	storage.SaveConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	err := mgr.Reload()
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
}

func TestManager_GetTools_ApprovedToolsFromDisconnectedMCP(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools: []string{"offline_mcp::tool_a"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	toolList := mgr.GetTools()
	found := false
	for _, ts := range toolList {
		if ts.MCPName == "offline_mcp" && ts.ToolName == "tool_a" {
			found = true
			if ts.Status != "approved" {
				t.Errorf("expected status 'approved', got '%s'", ts.Status)
			}
			if ts.Available {
				t.Errorf("expected Available=false for disconnected MCP")
			}
		}
	}
	if !found {
		t.Errorf("approved tool from offline MCP should be listed")
	}
}

func TestManager_GetTools_ManuallyApprovedFromDisconnectedMCP(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ManuallyApprovedTools: []string{"offline_mcp::tool_b"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	toolList := mgr.GetTools()
	found := false
	for _, ts := range toolList {
		if ts.MCPName == "offline_mcp" && ts.ToolName == "tool_b" {
			found = true
			if ts.Status != "manually_approved" {
				t.Errorf("expected status 'manually_approved'")
			}
			if ts.Available {
				t.Errorf("expected Available=false")
			}
		}
	}
	if !found {
		t.Errorf("manually approved tool from offline MCP should be listed")
	}
}

func TestManager_removeConflictingApproved(t *testing.T) {
	mgr := NewManager()
	mgr.config = &model.MCPConfig{
		ApprovedTools:         []string{"m::a", "m::b", "m::c"},
		ManuallyApprovedTools: []string{"m::b"},
	}
	removed := mgr.removeConflictingApproved()
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
	if len(mgr.config.ApprovedTools) != 2 {
		t.Errorf("expected 2 approved, got %d", len(mgr.config.ApprovedTools))
	}
}

func Test_isToolAvailable(t *testing.T) {
	mgr := NewManager()
	mgr.allTools = map[string][]model.ToolDef{
		"mcp1": {
			{Name: "tool_a"},
			{Name: "tool_b"},
		},
	}
	if !mgr.isToolAvailable("mcp1", "tool_a") {
		t.Errorf("expected tool_a to be available")
	}
	if mgr.isToolAvailable("mcp1", "nonexistent") {
		t.Errorf("nonexistent should not be available")
	}
	if mgr.isToolAvailable("mcp2", "tool_a") {
		t.Errorf("should not be available on wrong MCP")
	}
}

func Test_isMCPConnected(t *testing.T) {
	mgr := NewManager()
	mgr.clients = map[string]Client{
		"connected_mcp": nil,
	}
	if !mgr.isMCPConnected("connected_mcp") {
		t.Errorf("expected connected")
	}
	if mgr.isMCPConnected("offline_mcp") {
		t.Errorf("expected not connected")
	}
}

func TestManager_GetTools_Deduplication(t *testing.T) {
	// Set up tools from approved tools in config, make sure no duplicates
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools: []string{"mcp::tool_a", "mcp::tool_a"}, // duplicate
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	tools := mgr.GetTools()
	count := 0
	for _, t := range tools {
		if t.MCPName == "mcp" && t.ToolName == "tool_a" {
			count++
		}
	}
	// Config allows duplicates but GetTools should handle gracefully
	if count > 1 {
		t.Errorf("expected at most 1 entry for mcp::tool_a, got %d", count)
	}
}

func TestManager_SetToolStatus_RemoveFromApproved(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools: []string{"m::t"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	// Set to unapproved should remove from approved
	mgr.SetToolStatus("m", "t", "unapproved")
	cfg, _ := storage.LoadConfig()
	if len(cfg.ApprovedTools) != 0 {
		t.Errorf("expected 0 approved tools, got %d", len(cfg.ApprovedTools))
	}
	if len(cfg.ManuallyApprovedTools) != 0 {
		t.Errorf("expected 0 manually approved tools, got %d", len(cfg.ManuallyApprovedTools))
	}
}

func TestManager_GetTools_AllStatuses(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools:         []string{"offline::approved"},
		ManuallyApprovedTools: []string{"offline::manual"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	tools := mgr.GetTools()

	approvedFound := false
	manualFound := false
	for _, t := range tools {
		if t.MCPName == "offline" {
			if t.ToolName == "approved" && t.Status == "approved" {
				approvedFound = true
			}
			if t.ToolName == "manual" && t.Status == "manually_approved" {
				manualFound = true
			}
		}
	}
	if !approvedFound {
		t.Errorf("approved tool entry not found")
	}
	if !manualFound {
		t.Errorf("manually_approved tool entry not found")
	}
}

// TestManager_CLIRunner_ApproveAllTools verifies the headless CLI runner pattern:
// ApproveAll=true treats every available tool as approved in memory only,
// without persisting anything to the shared config file.
func TestManager_CLIRunner_ApproveAllTools(t *testing.T) {
	setupManagerTest(t)
	storage.SaveConfig(&model.MCPConfig{
		ManuallyApprovedTools: []string{"Sandbox::tree"},
	})

	mgr := NewManager()
	mgr.SkipAskUser = true // same as CLI runner
	mgr.ApproveAll = true  // same as CLI runner
	mgr.LoadAndConnect()

	tools := mgr.GetTools()
	if len(tools) == 0 {
		t.Fatalf("expected at least one built-in tool")
	}

	allowed := mgr.GetAllowedTools()
	if len(allowed) == 0 {
		t.Fatalf("expected non-empty GetAllowedTools with ApproveAll")
	}

	for _, tt := range tools {
		if !tt.Available {
			continue // unavailable config-only entries are not expected in GetAllowedTools
		}
		found := false
		for _, a := range allowed {
			if a.Name == tt.ToolName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("available tool %s::%s should be in GetAllowedTools with ApproveAll", tt.MCPName, tt.ToolName)
		}

		approved, manual := mgr.IsToolApproved(tt.MCPName + "::" + tt.ToolName)
		if !approved || manual {
			t.Errorf("IsToolApproved(%s::%s) = (%v, %v), want (true, false)", tt.MCPName, tt.ToolName, approved, manual)
		}
	}

	// A tool that does not exist must not be treated as approved
	if approved, _ := mgr.IsToolApproved("nonexistent_mcp::nonexistent_tool"); approved {
		t.Errorf("non-existent tool should not be approved even with ApproveAll")
	}

	// The persisted config must remain untouched: no approvals written and
	// the manually-approved list intact.
	disk, err := storage.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(disk.ApprovedTools) != 0 {
		t.Errorf("ApproveAll must not persist approved tools, got %v", disk.ApprovedTools)
	}
	if len(disk.ManuallyApprovedTools) != 1 || disk.ManuallyApprovedTools[0] != "Sandbox::tree" {
		t.Errorf("ApproveAll must not alter manually approved tools, got %v", disk.ManuallyApprovedTools)
	}
}
