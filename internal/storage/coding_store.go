package storage

import (
	"encoding/json"
	"os"

	"hschat/internal/model"
)

const codingConfigPath = "coding.json"

func LoadCodingConfig() (*model.CodingConfig, error) {
	data, err := os.ReadFile(codingConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultCodingConfig()
			SaveCodingConfig(cfg)
			return cfg, nil
		}
		return nil, err
	}
	cfg := &model.CodingConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func DefaultCodingConfig() *model.CodingConfig {
	relativeOverwrite := true
	return &model.CodingConfig{
		ShellTools: map[string]model.ShellTool{
			"go_test": {
				Description:       "Run go test",
				Command:           "go test ./...",
				Timeout:           60,
				RelativeOverwrite: &relativeOverwrite,
			},
		},
		Blacklist: []string{"os/exec"},
		RawShell: &model.RawShellConfig{
			Enabled: false,
			Shell:   []string{"powershell", "-NoProfile", "-Command"},
			Preamble: "function python { & \"E:\\rl\\rt3913\\python\" $args }; " +
				"function pip { & \"E:\\rl\\rt3913\\python\" -m pip $args }; " +
				"$env:all_proxy = \"socks5://127.0.0.1:1080\"; " +
				"$env:https_proxy = \"socks5://127.0.0.1:1080\"; " +
				"$env:http_proxy = \"socks5://127.0.0.1:1080\"; " +
				"$original",
		},
	}
}

func SaveCodingConfig(cfg *model.CodingConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(codingConfigPath, data, 0644)
}
