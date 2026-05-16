package storage

import (
	"encoding/json"
	"os"

	"hschat/internal/model"
)

const codingConfigPath = "coding.json"

func LoadCodingConfig() (*model.CodingConfig, error) {
	cfg := &model.CodingConfig{}
	data, err := os.ReadFile(codingConfigPath)
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

func SaveCodingConfig(cfg *model.CodingConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(codingConfigPath, data, 0644)
}
