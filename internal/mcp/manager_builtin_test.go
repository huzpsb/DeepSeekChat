package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hschat/internal/model"
	"hschat/internal/storage"
)

func TestManager_LoadAndConnect_SandboxAlways(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	err := mgr.LoadAndConnect()
	if err != nil {
		t.Fatalf("LoadAndConnect failed: %v", err)
	}

	if !mgr.isMCPConnected("Sandbox") {
		t.Errorf("expected Sandbox to be connected unconditionally")
	}

	tools, ok := mgr.allTools["Sandbox"]
	if !ok {
		t.Fatalf("expected Sandbox in allTools")
	}
	if len(tools) != 10 {
		t.Errorf("expected 10 sandbox tools, got %d", len(tools))
	}

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
			t.Errorf("expected tool '%s' in Sandbox tools", name)
		}
	}
}

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

	if !mgr.isMCPConnected("Coding") {
		t.Errorf("expected Coding to be connected")
	}

	tools, ok := mgr.allTools["Coding"]
	if !ok {
		t.Fatalf("expected Coding in allTools")
	}
	if len(tools) < 1 {
		t.Errorf("expected at least 1 coding tool, got %d", len(tools))
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

	if mgr.isMCPConnected("Coding") {
		t.Errorf("expected Coding not to be connected when disabled")
	}
}

func TestManager_LoadAndConnect_CodingDefault(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	err := mgr.LoadAndConnect()
	if err != nil {
		t.Fatalf("LoadAndConnect failed: %v", err)
	}

	// Coding defaults to false when not set explicitly
	if mgr.isMCPConnected("Coding") {
		t.Errorf("expected Coding not to be connected by default")
	}
}

func TestManager_GetTools_WithSandbox(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	tools := mgr.GetTools()

	sandboxTools := 0
	for _, ts := range tools {
		if ts.MCPName == "Sandbox" && ts.Available {
			sandboxTools++
		}
	}
	if sandboxTools != 10 {
		t.Errorf("expected 10 sandbox tools, got %d", sandboxTools)
	}
}

func TestManager_GetAllowedTools_WithApprovedSandbox(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools: []string{"Sandbox::tree", "Sandbox::read_content"},
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

func TestManager_GetAllowedTools_NoApproved(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools:     []string{},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	allowed := mgr.GetAllowedTools()
	if len(allowed) != 0 {
		t.Errorf("expected 0 allowed tools, got %d", len(allowed))
	}
}

func TestManager_ExecuteTool_SandboxTool(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools: []string{"Sandbox::create_file"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	result, err := mgr.ExecuteTool("Sandbox::create_file", `{"file":"test.txt","content":"hello"}`)
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

	cwd, _ := os.Getwd()
	data, err := os.ReadFile(filepath.Join(cwd, "test.txt"))
	if err != nil {
		t.Fatalf("expected file to be created: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(data))
	}
}

func TestManager_ExecuteTool_SandboxTool_ReadContent(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools: []string{"Sandbox::create_file", "Sandbox::read_content"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	mgr.ExecuteTool("Sandbox::create_file", `{"file":"readme.txt","content":"line1\nline2\nline3"}`)

	result, err := mgr.ExecuteTool("Sandbox::read_content", `{"file":"readme.txt"}`)
	if err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "line2") {
		t.Errorf("expected 'line2' in read result, got '%s'", result.Content[0].Text)
	}
}

func TestManager_ToolExists_WithSandbox(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	if !mgr.ToolExists("Sandbox::tree") {
		t.Errorf("expected Sandbox::tree to exist")
	}
	if !mgr.ToolExists("Sandbox::search_name") {
		t.Errorf("expected Sandbox::search_name to exist")
	}
	if mgr.ToolExists("Sandbox::nonexistent_tool") {
		t.Errorf("expected Sandbox::nonexistent_tool not to exist")
	}
}

func TestManager_IsToolApproved_WithSandbox(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools:         []string{"Sandbox::tree", "Sandbox::rm"},
		ManuallyApprovedTools: []string{"Sandbox::move"},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	approved, manual := mgr.IsToolApproved("Sandbox::tree")
	if !approved || manual {
		t.Errorf("expected approved=true, manual=false for tree")
	}

	approved, manual = mgr.IsToolApproved("Sandbox::rm")
	if !approved || manual {
		t.Errorf("expected approved=true, manual=false for rm")
	}

	approved, manual = mgr.IsToolApproved("Sandbox::move")
	if approved || !manual {
		t.Errorf("expected approved=false, manual=true for move")
	}

	approved, manual = mgr.IsToolApproved("Sandbox::read_content")
	if approved || manual {
		t.Errorf("expected approved=false, manual=false for unapproved read_content")
	}
}

func TestManager_SetToolStatus_WithSandbox(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	err := mgr.SetToolStatus("Sandbox", "tree", "approved")
	if err != nil {
		t.Fatalf("SetToolStatus failed: %v", err)
	}

	cfg, _ := storage.LoadConfig()
	found := false
	for _, t := range cfg.ApprovedTools {
		if t == "Sandbox::tree" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Sandbox::tree in approved tools")
	}

	mgr.SetToolStatus("Sandbox", "rm", "manually_approved")
	cfg, _ = storage.LoadConfig()
	found = false
	for _, t := range cfg.ManuallyApprovedTools {
		if t == "Sandbox::rm" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Sandbox::rm in manually approved tools")
	}
}

func TestManager_Reload_WithCoding(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		EnableCodingTools: true,
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	if !mgr.isMCPConnected("Coding") {
		t.Fatalf("expected Coding to be connected before reload")
	}

	err := mgr.Reload()
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	if !mgr.isMCPConnected("Coding") {
		t.Errorf("expected Coding to be connected after reload")
	}

	tools, ok := mgr.allTools["Coding"]
	if !ok {
		t.Fatalf("expected Coding in allTools after reload")
	}
	if len(tools) < 1 {
		t.Errorf("expected at least 1 tool after reload, got %d", len(tools))
	}
}

func TestManager_ExecuteTool_SandboxTool_NoArgs(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	result, err := mgr.ExecuteTool("Sandbox::tree", "")
	if err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if len(result.Content) == 0 {
		t.Errorf("expected content in result")
	}
}

func TestManager_ExecuteTool_SandboxTool_BadJson(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	mgr.LoadAndConnect()

	result, err := mgr.ExecuteTool("Sandbox::tree", "not json")
	if err != nil {
		t.Fatalf("ExecuteTool should handle bad JSON: %v", err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Errorf("expected non-empty result")
	}
}

func TestManager_GetTools_UnapprovedSandboxTools(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools: []string{},
	})

	mgr := NewManager()
	mgr.LoadAndConnect()

	tools := mgr.GetTools()

	unapprovedCount := 0
	for _, ts := range tools {
		if ts.MCPName == "Sandbox" && ts.Available && ts.Status == "unapproved" {
			unapprovedCount++
		}
	}
	if unapprovedCount != 10 {
		t.Errorf("expected 10 unapproved sandbox tools, got %d", unapprovedCount)
	}
}

func TestManager_GetAllowedTools_CategorizedByApproval(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{
		ApprovedTools:         []string{"Sandbox::tree"},
		ManuallyApprovedTools: []string{"Sandbox::search_name"},
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
