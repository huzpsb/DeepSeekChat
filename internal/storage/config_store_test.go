package storage

import (
	"os"
	"testing"

	"hschat/internal/model"
)

func TestLoadConfig_NotExist(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig should not error when file missing: %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected non-nil config")
	}
	if cfg.APIKey != "" {
		t.Errorf("expected empty api_key, got '%s'", cfg.APIKey)
	}
	if !cfg.EnableCodingTools {
		t.Errorf("expected enable_coding_tools=true by default")
	}
	if len(cfg.MCPServers) != 2 {
		t.Errorf("expected 2 default servers, got %d", len(cfg.MCPServers))
	}
	if cfg.MCPServers[0].Name != "SSE_Example" || cfg.MCPServers[0].Type != "sse" {
		t.Errorf("expected SSE_Example server, got %v", cfg.MCPServers[0])
	}
	if cfg.MCPServers[1].Name != "STDIO_Example" || cfg.MCPServers[1].Type != "stdio" {
		t.Errorf("expected STDIO_Example server, got %v", cfg.MCPServers[1])
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg := &model.MCPConfig{
		APIKey: "sk-test-key",
		MCPServers: []model.MCPServer{
			{
				Name:    "test_mcp",
				Type:    "stdio",
				Command: []string{"test.exe", "--verbose"},
			},
		},
		ApprovedTools:         []string{"test_mcp::tool_a", "test_mcp::tool_b"},
		ManuallyApprovedTools: []string{"test_mcp::tool_c"},
	}

	err := SaveConfig(cfg)
	if err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if loaded.APIKey != "sk-test-key" {
		t.Errorf("expected api_key 'sk-test-key', got '%s'", loaded.APIKey)
	}
	if len(loaded.MCPServers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(loaded.MCPServers))
	}
	if loaded.MCPServers[0].Name != "test_mcp" {
		t.Errorf("expected server name 'test_mcp', got '%s'", loaded.MCPServers[0].Name)
	}
	if loaded.MCPServers[0].Type != "stdio" {
		t.Errorf("expected type 'stdio', got '%s'", loaded.MCPServers[0].Type)
	}
	if len(loaded.MCPServers[0].Command) != 2 || loaded.MCPServers[0].Command[0] != "test.exe" || loaded.MCPServers[0].Command[1] != "--verbose" {
		t.Errorf("expected command ['test.exe','--verbose'], got %v", loaded.MCPServers[0].Command)
	}
	if len(loaded.ApprovedTools) != 2 {
		t.Errorf("expected 2 approved tools, got %d", len(loaded.ApprovedTools))
	}
	if len(loaded.ManuallyApprovedTools) != 1 {
		t.Errorf("expected 1 manually approved tool, got %d", len(loaded.ManuallyApprovedTools))
	}
}

func TestSaveConfig_Overwrite(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg1 := &model.MCPConfig{APIKey: "key1"}
	cfg2 := &model.MCPConfig{APIKey: "key2"}

	SaveConfig(cfg1)
	SaveConfig(cfg2)

	loaded, _ := LoadConfig()
	if loaded.APIKey != "key2" {
		t.Errorf("expected 'key2' after overwrite, got '%s'", loaded.APIKey)
	}
}

func TestLoadConfig_Corrupt(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	os.WriteFile("config.json", []byte("not valid json"), 0644)

	_, err := LoadConfig()
	if err == nil {
		t.Errorf("expected error for corrupt config")
	}
}

func TestSaveConfig_NilFields(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg := &model.MCPConfig{}
	err := SaveConfig(cfg)
	if err != nil {
		t.Fatalf("SaveConfig with empty config failed: %v", err)
	}

	loaded, _ := LoadConfig()
	if loaded.APIKey != "" {
		t.Errorf("expected empty api_key")
	}
}

func TestSaveConfig_SSEServer(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg := &model.MCPConfig{
		MCPServers: []model.MCPServer{
			{
				Name: "cloud_mcp",
				Type: "sse",
				URL:  "https://example.com/mcp",
			},
		},
	}

	err := SaveConfig(cfg)
	if err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, _ := LoadConfig()
	if loaded.MCPServers[0].URL != "https://example.com/mcp" {
		t.Errorf("expected URL, got '%s'", loaded.MCPServers[0].URL)
	}
	if loaded.MCPServers[0].Command != nil {
		t.Errorf("expected nil command for SSE, got %v", loaded.MCPServers[0].Command)
	}
}
