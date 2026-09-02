package storage

import (
	"encoding/json"
	"os"

	"hschat/internal/jupyter"
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
	// On a Linux box with a running Jupyter server the intended workflow
	// is "start DsChat and immediately chat with full shell access", so
	// the default coding config there is a raw bash shell instead of the
	// curated Windows-oriented tool set.
	if jupyter.Detect() {
		return &model.CodingConfig{
			ShellTools: map[string]model.ShellTool{},
			Blacklist:  []string{},
			RawShell: &model.RawShellConfig{
				Enabled:  true,
				Shell:    []string{"bash", "-c"},
				Preamble: "$original",
			},
		}
	}
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
