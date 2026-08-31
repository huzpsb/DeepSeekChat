package storage

import (
	"os"
	"strings"
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
	if len(cfg.ModelProviders) != 3 {
		t.Fatalf("expected 3 default providers, got %d", len(cfg.ModelProviders))
	}
	if cfg.ModelProviders[0].Name != "opencode-zen" || len(cfg.ModelProviders[0].Models) != 1 {
		t.Errorf("expected opencode-zen provider with 1 model, got %+v", cfg.ModelProviders[0])
	}
	if cfg.ModelProviders[1].Name != "deepseek" || len(cfg.ModelProviders[1].Models) != 2 {
		t.Errorf("expected deepseek provider with 2 models, got %+v", cfg.ModelProviders[1])
	}
	if cfg.Provider != "opencode-zen" || cfg.Model != "deepseek-v4-flash-free" {
		t.Errorf("expected opencode-zen/deepseek-v4-flash-free selection, got %s/%s", cfg.Provider, cfg.Model)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("expected config.json to be auto-generated on cold start: %v", err)
	}
	if !cfg.EnableCodingTools {
		t.Errorf("expected enable_coding_tools=true by default")
	}
	if len(cfg.MCPServers) != 2 {
		t.Errorf("expected 2 default servers, got %d", len(cfg.MCPServers))
	}
	if cfg.MCPServers[0].Name != "Streamable_Example" || cfg.MCPServers[0].Type != "streamable" {
		t.Errorf("expected Streamable_Example server, got %v", cfg.MCPServers[0])
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
		ModelProviders: []model.ModelProvider{
			{Name: "test", Endpoint: "http://127.0.0.1", APIKey: "sk-test-key", Models: []string{"m"}},
		},
		Provider: "test",
		Model:    "m",
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
	if loaded.ModelProviders[0].APIKey != "sk-test-key" {
		t.Errorf("expected api_key 'sk-test-key', got '%s'", loaded.ModelProviders[0].APIKey)
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

	cfg1 := &model.MCPConfig{Provider: "key1"}
	cfg2 := &model.MCPConfig{Provider: "key2"}

	SaveConfig(cfg1)
	SaveConfig(cfg2)

	loaded, _ := LoadConfig()
	if loaded.Provider != "key2" {
		t.Errorf("expected 'key2' after overwrite, got '%s'", loaded.Provider)
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
	if loaded.Provider != "" {
		t.Errorf("expected empty provider")
	}
}

func TestSaveConfig_StreamableServer(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg := &model.MCPConfig{
		MCPServers: []model.MCPServer{
			{
				Name: "cloud_mcp",
				Type: "streamable",
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
		t.Errorf("expected nil command for streamable, got %v", loaded.MCPServers[0].Command)
	}
}

func selectionConfig(provider, mdl string) *model.MCPConfig {
	return &model.MCPConfig{
		ModelProviders: []model.ModelProvider{
			{Name: "kimi-code", Models: []string{"k3-256k", "k3"}},
			{Name: "deepseek", Models: []string{"deepseek-v4-flash"}},
		},
		Provider: provider,
		Model:    mdl,
	}
}

func TestNormalizeModelSelection_ValidUntouched(t *testing.T) {
	cfg := selectionConfig("kimi-code", "k3")
	if normalizeModelSelection(cfg) {
		t.Fatal("valid selection must not be rewritten")
	}
	if cfg.Provider != "kimi-code" || cfg.Model != "k3" {
		t.Fatalf("unexpected rewrite: %s/%s", cfg.Provider, cfg.Model)
	}
}

func TestNormalizeModelSelection_DeletedProvider(t *testing.T) {
	// Reproduces the reported bug: provider "opencode-zen" was removed from
	// model_providers, leaving a stale selection the UI cannot render (the
	// model dropdown falls back to displaying the first option).
	cfg := selectionConfig("opencode-zen", "k3")
	if !normalizeModelSelection(cfg) {
		t.Fatal("stale provider must be rewritten")
	}
	if cfg.Provider != "kimi-code" || cfg.Model != "k3" {
		t.Fatalf("want kimi-code/k3 (valid model preserved), got %s/%s", cfg.Provider, cfg.Model)
	}
}

func TestNormalizeModelSelection_UnknownModel(t *testing.T) {
	cfg := selectionConfig("deepseek", "k3")
	if !normalizeModelSelection(cfg) {
		t.Fatal("model not offered by provider must be rewritten")
	}
	if cfg.Provider != "deepseek" || cfg.Model != "deepseek-v4-flash" {
		t.Fatalf("want deepseek/deepseek-v4-flash, got %s/%s", cfg.Provider, cfg.Model)
	}
}

func TestNormalizeModelSelection_NoProviders(t *testing.T) {
	cfg := &model.MCPConfig{Provider: "gone", Model: "gone"}
	if normalizeModelSelection(cfg) {
		t.Fatal("no providers configured: nothing to fall back to")
	}
}

func TestLoadConfig_NormalizesStaleSelection(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	if err := SaveConfig(selectionConfig("opencode-zen", "k3")); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if loaded.Provider != "kimi-code" || loaded.Model != "k3" {
		t.Fatalf("want persisted kimi-code/k3, got %s/%s", loaded.Provider, loaded.Model)
	}
	// Must be persisted, not just in-memory.
	raw, _ := os.ReadFile(configPath)
	if !strings.Contains(string(raw), `"provider": "kimi-code"`) {
		t.Errorf("fallback not persisted to config.json:\n%s", raw)
	}
}
