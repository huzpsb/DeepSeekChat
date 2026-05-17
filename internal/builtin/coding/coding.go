package coding

import (
	"hschat/internal/builtin"
	"hschat/internal/model"
	"hschat/internal/storage"
)

type Provider struct {
	rootDir       string
	shellTools    map[string]model.ShellTool
	fileBlacklist []string
}

func New(rootDir string) builtin.Provider {
	return &Provider{
		rootDir:       rootDir,
		shellTools:    make(map[string]model.ShellTool),
		fileBlacklist: []string{},
	}
}

func (p *Provider) Name() string {
	return "Coding"
}

func (p *Provider) Initialize(configPath string) error {
	cfg, err := storage.LoadCodingConfig()
	if err != nil {
		return err
	}

	if cfg.Blacklist == nil {
		cfg.Blacklist = []string{"os/exec"}
	}
	p.fileBlacklist = cfg.Blacklist

	if cfg.ShellTools == nil {
		cfg.ShellTools = map[string]model.ShellTool{
			"go_test": {
				Description: "Run go test",
				Command:     "go test ./...",
				Timeout:     60,
			},
		}
	}
	for name, tool := range cfg.ShellTools {
		if tool.Timeout <= 0 {
			tool.Timeout = 60
		}
		cfg.ShellTools[name] = tool
	}
	p.shellTools = cfg.ShellTools

	storage.SaveCodingConfig(cfg)

	return nil
}

func (p *Provider) Close() error {
	return nil
}
