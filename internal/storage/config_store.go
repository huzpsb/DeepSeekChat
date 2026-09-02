package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"hschat/internal/model"
)

const configPath = "config.json"

func LoadConfig() (*model.MCPConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := defaultConfig()
			SaveConfig(cfg)
			return cfg, nil
		}
		return nil, err
	}
	cfg := &model.MCPConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	// The selected provider/model may refer to entries the user has since
	// deleted from model_providers. ResolveModel already falls back at
	// inference time, but the raw fields still reach the UI (GET
	// /api/config), where a stale provider matches nothing and the model
	// dropdown silently renders the first option. Normalize and persist
	// here so config, UI, and inference all agree.
	changed := normalizeModelSelection(cfg)
	// Root dirs that no longer exist on disk must not be silently
	// recreated by the sandbox provider: drop them from the list instead.
	// The builtin default ("./agent") is exempt — it is the guaranteed
	// fallback workspace and is (re)created on demand.
	if pruneMissingRootDirs(cfg) {
		changed = true
	}
	if changed {
		SaveConfig(cfg)
	}
	return cfg, nil
}

// pruneMissingRootDirs removes root dir entries whose directory has been
// deleted from disk, except the builtin default "./agent". If nothing is
// left, the list falls back to ["./agent"]. It reports whether the config
// was modified.
func pruneMissingRootDirs(cfg *model.MCPConfig) bool {
	dirs := cfg.Sandbox.RootDirs
	if len(dirs) == 0 {
		return false
	}
	def := absPath("./agent")
	kept := make([]string, 0, len(dirs))
	changed := false
	for _, d := range dirs {
		trimmed := strings.TrimSpace(d)
		if trimmed == "" {
			changed = true
			continue
		}
		if absPath(trimmed) == def {
			kept = append(kept, d)
			continue
		}
		if info, err := os.Stat(absPath(trimmed)); err == nil && info.IsDir() {
			kept = append(kept, d)
		} else {
			changed = true
		}
	}
	if len(kept) == 0 {
		kept = []string{"./agent"}
		changed = true
	}
	if !changed {
		return false
	}
	cfg.Sandbox.RootDirs = kept
	return true
}

func absPath(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// normalizeModelSelection falls the selection back to the first configured
// provider/model when the stored provider is unknown or the stored model is
// not offered by the selected provider. It reports whether anything changed.
func normalizeModelSelection(cfg *model.MCPConfig) bool {
	if len(cfg.ModelProviders) == 0 {
		return false
	}
	changed := false
	p := cfg.FindProvider(cfg.Provider)
	if p == nil {
		p = &cfg.ModelProviders[0]
		cfg.Provider = p.Name
		changed = true
	}
	if len(p.Models) == 0 {
		return changed
	}
	found := false
	for _, m := range p.Models {
		if m == cfg.Model {
			found = true
			break
		}
	}
	if !found {
		cfg.Model = p.Models[0]
		changed = true
	}
	return changed
}

func defaultConfig() *model.MCPConfig {
	return &model.MCPConfig{
		Port: 5234,
		Sandbox: model.SandboxConfig{
			RootDirs:     []string{"./agent"},
			ExtBlacklist: []string{"exe", "dll", "ppt", "pptx", "doc", "docx", "pdf", "class", "dex", "apk", "bin", "jpg", "jpeg", "png", "gif"},
		},
		EnableCodingTools: true,
		DefaultPrompt:     "You are a helpful assistant.",
		MCPServers: []model.MCPServer{
			{
				Name: "Streamable_Example",
				Type: "streamable",
				URL:  "http://127.0.0.1:12345/stream",
			},
			{
				Name:    "STDIO_Example",
				Type:    "stdio",
				Command: []string{"./mcp_server.exe", "--verbose"},
			},
		},
		ModelProviders: []model.ModelProvider{
			{
				Name:     "opencode-zen",
				Endpoint: "https://opencode.ai/zen/v1/chat/completions",
				Models:   []string{"deepseek-v4-flash-free"},
			},
			{
				Name:     "deepseek",
				Endpoint: "https://api.deepseek.com/chat/completions",
				APIKey:   "sk-xxxx",
				Models:   []string{"deepseek-v4-flash", "deepseek-v4-pro"},
			},
			{
				Name:     "kimi-code",
				Endpoint: "https://api.kimi.com/coding/v1/chat/completions",
				APIKey:   "sk-xxxx",
				Models:   []string{"k3-256k"},
			},
		},
		Provider: "opencode-zen",
		Model:    "deepseek-v4-flash-free",
	}
}

func SaveConfig(cfg *model.MCPConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}
