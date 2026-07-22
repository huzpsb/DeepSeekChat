package storage

import (
	"encoding/json"
	"os"

	"hschat/internal/model"
)

const configPath = "config.json"

func LoadConfig() (*model.MCPConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return nil, err
	}
	cfg := &model.MCPConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func defaultConfig() *model.MCPConfig {
	return &model.MCPConfig{
		Sandbox: model.SandboxConfig{
			RootDir:      "",
			ExtBlacklist: []string{"exe", "dll", "ppt", "pptx", "doc", "docx", "pdf"},
		},
		EnableCodingTools: true,
		EnableWebTools:    true,
		MCPServers: []model.MCPServer{
			{
				Name: "SSE_Example",
				Type: "sse",
				URL:  "http://127.0.0.1:12345/stream",
			},
			{
				Name:    "STDIO_Example",
				Type:    "stdio",
				Command: []string{"./mcp_server.exe", "--verbose"},
			},
		},
		ThirdParty: model.ThirdPartyConfig{
			Enabled:  false,
			Endpoint: "https://opencode.ai/zen/v1/chat/completions",
			Model:    "deepseek-v4-flash-free",
		},
	}
}

func SaveConfig(cfg *model.MCPConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}
