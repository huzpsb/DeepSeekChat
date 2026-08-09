package coding

import (
	"sync"

	"hschat/internal/builtin"
	"hschat/internal/builtin/sandbox"
	"hschat/internal/model"
	"hschat/internal/storage"
)

func boolPtr(v bool) *bool {
	return &v
}

type Provider struct {
	mu             sync.RWMutex
	rootDir        string
	shellTools     map[string]model.ShellTool
	fileBlacklist  []string
	rawShell       *model.RawShellConfig
	rawShellWarned bool
}

func New(rootDir string) builtin.Provider {
	rootDir = sandbox.ResolveRootDir(rootDir)
	return &Provider{
		rootDir:       rootDir,
		shellTools:    make(map[string]model.ShellTool),
		fileBlacklist: []string{},
	}
}

func (p *Provider) Name() string {
	return "Coding"
}

func (p *Provider) SetRootDir(dir string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rootDir = sandbox.ResolveRootDir(dir)
}

func (p *Provider) getRootDir() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.rootDir
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
				Description:       "Run go test",
				Command:           "go test ./...",
				Timeout:           60,
				RelativeOverwrite: boolPtr(true),
			},
		}
	}
	for name, tool := range cfg.ShellTools {
		if tool.Timeout <= 0 {
			tool.Timeout = 60
		}
		if tool.RelativeOverwrite == nil {
			tool.RelativeOverwrite = boolPtr(true)
		}
		cfg.ShellTools[name] = tool
	}
	p.shellTools = cfg.ShellTools

	if cfg.RawShell == nil {
		cfg.RawShell = &model.RawShellConfig{
			Enabled: false,
			Shell:   []string{"powershell", "-NoProfile", "-Command"},
			Preamble: "function python { & \"E:\\rl\\rt3913\\python\" $args }; " +
				"function pip { & \"E:\\rl\\rt3913\\python\" -m pip $args }; " +
				"$env:all_proxy = \"socks5://127.0.0.1:1080\"; " +
				"$env:https_proxy = \"socks5://127.0.0.1:1080\"; " +
				"$env:http_proxy = \"socks5://127.0.0.1:1080\"; " +
				"$original",
		}
	}
	p.rawShell = cfg.RawShell

	storage.SaveCodingConfig(cfg)

	return nil
}

func (p *Provider) Close() error {
	return nil
}
