package storage

import (
	"encoding/json"
	"os"

	"hschat/internal/model"
)

const webConfigPath = "web.json"

func LoadWebConfig() (*model.WebConfig, error) {
	cfg := &model.WebConfig{}
	data, err := os.ReadFile(webConfigPath)
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

func SaveWebConfig(cfg *model.WebConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(webConfigPath, data, 0644)
}
