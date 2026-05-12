package storage

import (
	"encoding/json"
	"os"

	"hschat/internal/model"
)

const configPath = "config.json"

func LoadConfig() (*model.MCPConfig, error) {
	cfg := &model.MCPConfig{}
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func SaveConfig(cfg *model.MCPConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}
