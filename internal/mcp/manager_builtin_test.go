package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hschat/internal/model"
	"hschat/internal/storage"
)

func TestManager_LoadAndConnect_CodingEnabled(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools: true,
	})

	mgr := NewManager()
	err := mgr.LoadAndConnect()
	if err != nil {
		t.Fatalf("LoadAndConnect failed: %v", err)
	}

	// CodingMCP should be a connected client
	if !mgr.isMCPConnected("CodingMCP") {
		t.Errorf("expected CodingMCP to be connected")
	}

	// CodingMCP tools should be available
	tools, ok := mgr.allTools["CodingMCP"]
	if !ok {
		t.Fatalf("expected CodingMCP in allTools")
	}
	if len(tools) < 10 {
		t.Errorf("expected at least 10 coding tools, got %d", len(tools))
	}

	// Verify specific tools exist
	expectedTools := []string{"tree", "search_name", "search_content", "read_content",
		"replace_content", "create_dir", "create_file", "rm", "move", "rewrite_file"}
	for _, name := range expectedTools {
		found := false
		for _, tool := range tools {
			if tool.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected tool '%s' in CodingMCP tools", name)
		}
	}
}

func TestManager_LoadAndConnect_CodingDisabled(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools: false,
	})

	mgr := NewManager()
	err := mgr.LoadAndConnect()
	if err != nil {
		t.Fatalf("LoadAndConnect failed: %v", err)
	}

	if mgr.isMCPConnected("CodingMCP") {
		t.Errorf("expected CodingMCP not to be connected when disabled")
	}
}

func TestManager_LoadAndConnect_CodingDefault(t *testing.T) {
	// When EnableCodingTools is not set (false by default), coding should not load
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	err := mgr.LoadAndConnect()
	if err != nil {
		t.Fatalf("LoadAndConnect failed: %v", err)
	}

	if mgr.isMCPConnected("CodingMCP") {
		t.Errorf("expected CodingMCP not to be connected by default")
	}
}

func TestManager_GetTools_WithCoding(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools: true,
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	tools := mgr.GetTools()

	// Find coding tools in the result
	codingTools := 0
	for _, ts := range tools {
		if ts.MCPName == "CodingMCP" && ts.Available {
			codingTools++
		}
	}
	if codingTools < 10 {
		t.Errorf("expected at least 10 coding tools, got %d", codingTools)
	}
}

func TestManager_GetAllowedTools_WithApprovedCoding(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools: true,
		ApprovedTools:     []string{"CodingMCP::tree", "CodingMCP::read_content"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	allowed := mgr.GetAllowedTools()
	if len(allowed) != 2 {
		t.Errorf("expected 2 allowed tools, got %d", len(allowed))
	}

	foundTree := false
	foundRead := false
	for _, tool := range allowed {
		if tool.Name == "tree" {
			foundTree = true
		}
		if tool.Name == "read_content" {
			foundRead = true
		}
	}
	if !foundTree {
		t.Errorf("expected 'tree' in allowed tools")
	}
	if !foundRead {
		t.Errorf("expected 'read_content' in allowed tools")
	}
}

func TestManager_GetAllowedTools_NoApprovedCoding(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools: true,
		ApprovedTools:     []string{},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	allowed := mgr.GetAllowedTools()
	if len(allowed) != 0 {
		t.Errorf("expected 0 allowed tools, got %d", len(allowed))
	}
}

func TestManager_ExecuteTool_CodingTool(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools: true,
		ApprovedTools:     []string{"CodingMCP::create_file"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	// Execute create_file via the coding provider
	result, err := mgr.ExecuteTool("CodingMCP::create_file", `{"file":"test.txt","content":"hello"}`)
	if err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Content))
	}
	if !strings.Contains(result.Content[0].Text, "Success") {
		t.Errorf("expected 'Success', got '%s'", result.Content[0].Text)
	}

	// Verify file was created
	cwd, _ := os.Getwd()
	data, err := os.ReadFile(filepath.Join(cwd, "test.txt"))
	if err != nil {
		t.Fatalf("expected file to be created: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(data))
	}
}

func TestManager_ExecuteTool_CodingTool_ReadContent(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools: true,
		ApprovedTools:     []string{"CodingMCP::read_content"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	// First create a file
	mgr.ExecuteTool("CodingMCP::create_file", `{"file":"readme.txt","content":"line1\nline2\nline3"}`)

	// Then read it back
	result, err := mgr.ExecuteTool("CodingMCP::read_content", `{"file":"readme.txt"}`)
	if err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "line2") {
		t.Errorf("expected 'line2' in read result, got '%s'", result.Content[0].Text)
	}
}

func TestManager_ToolExists_WithCoding(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools: true,
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	// Full name with MCP prefix
	if !mgr.ToolExists("CodingMCP::tree") {
		t.Errorf("expected CodingMCP::tree to exist")
	}
	if !mgr.ToolExists("CodingMCP::search_name") {
		t.Errorf("expected CodingMCP::search_name to exist")
	}
	if mgr.ToolExists("CodingMCP::nonexistent_tool") {
		t.Errorf("expected CodingMCP::nonexistent_tool not to exist")
	}
}

func TestManager_IsToolApproved_WithCoding(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools:     true,
		ApprovedTools:         []string{"CodingMCP::tree", "CodingMCP::rm"},
		ManuallyApprovedTools: []string{"CodingMCP::move"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	approved, manual := mgr.IsToolApproved("CodingMCP::tree")
	if !approved || manual {
		t.Errorf("expected approved=true, manual=false for tree")
	}

	approved, manual = mgr.IsToolApproved("CodingMCP::rm")
	if !approved || manual {
		t.Errorf("expected approved=true, manual=false for rm")
	}

	approved, manual = mgr.IsToolApproved("CodingMCP::move")
	if approved || !manual {
		t.Errorf("expected approved=false, manual=true for move")
	}

	approved, manual = mgr.IsToolApproved("CodingMCP::read_content")
	if approved || manual {
		t.Errorf("expected approved=false, manual=false for unapproved read_content")
	}
}

func TestManager_SetToolStatus_WithCoding(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools: true,
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	// Approve a coding tool
	err := mgr.SetToolStatus("CodingMCP", "tree", "approved")
	if err != nil {
		t.Fatalf("SetToolStatus failed: %v", err)
	}

	// Verify config was updated
	cfg, _ := storage.LoadConfig()
	found := false
	for _, t := range cfg.ApprovedTools {
		if t == "CodingMCP::tree" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected CodingMCP::tree in approved tools")
	}

	// Set another to manually_approved
	mgr.SetToolStatus("CodingMCP", "rm", "manually_approved")
	cfg, _ = storage.LoadConfig()
	found = false
	for _, t := range cfg.ManuallyApprovedTools {
		if t == "CodingMCP::rm" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected CodingMCP::rm in manually approved tools")
	}
}

func TestManager_Reload_WithCoding(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools: true,
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	if !mgr.isMCPConnected("CodingMCP") {
		t.Fatalf("expected CodingMCP to be connected before reload")
	}

	// Reload should reconnect coding tools
	err := mgr.Reload()
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	if !mgr.isMCPConnected("CodingMCP") {
		t.Errorf("expected CodingMCP to be connected after reload")
	}

	tools, ok := mgr.allTools["CodingMCP"]
	if !ok {
		t.Fatalf("expected CodingMCP in allTools after reload")
	}
	if len(tools) < 10 {
		t.Errorf("expected at least 10 tools after reload, got %d", len(tools))
	}
}

func TestManager_ExecuteTool_CodingTool_NoArgs(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools: true,
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	// tree works with no arguments (uses defaults)
	result, err := mgr.ExecuteTool("CodingMCP::tree", "")
	if err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	// Should return something meaningful
	if len(result.Content) == 0 {
		t.Errorf("expected content in result")
	}
}

func TestManager_ExecuteTool_CodingTool_BadJson(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools: true,
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	// Bad JSON should still work — coding tools handle arg parsing gracefully
	result, err := mgr.ExecuteTool("CodingMCP::tree", "not json")
	if err != nil {
		t.Fatalf("ExecuteTool should handle bad JSON: %v", err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Errorf("expected non-empty result")
	}
}

func TestManager_GetTools_UnapprovedCodingTools(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools: true,
		ApprovedTools:     []string{},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	tools := mgr.GetTools()

	// All coding tools should appear as "unapproved"
	unapprovedCount := 0
	for _, ts := range tools {
		if ts.MCPName == "CodingMCP" && ts.Available && ts.Status == "unapproved" {
			unapprovedCount++
		}
	}
	if unapprovedCount < 10 {
		t.Errorf("expected at least 10 unapproved coding tools, got %d", unapprovedCount)
	}
}

func TestManager_GetAllowedTools_CategorizedByApproval(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools:     true,
		ApprovedTools:         []string{"CodingMCP::tree"},
		ManuallyApprovedTools: []string{"CodingMCP::search_name"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	allowed := mgr.GetAllowedTools()
	if len(allowed) != 2 {
		t.Fatalf("expected 2 allowed tools (1 approved + 1 manual), got %d", len(allowed))
	}

	treeFound := false
	searchFound := false
	for _, tool := range allowed {
		if tool.Name == "tree" {
			treeFound = true
		}
		if tool.Name == "search_name" {
			searchFound = true
		}
	}
	if !treeFound || !searchFound {
		t.Errorf("expected both tree and search_name in allowed, got %v", allowed)
	}
}
