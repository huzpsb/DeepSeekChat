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

func (p *Provider) Initialize(_ string) error {
	cfg, err := storage.LoadCodingConfig()
	if err != nil {
		return err
	}

	// Missing fields fall back to the shared platform-aware defaults (raw
	// bash shell on Linux boxes running Jupyter, curated tool set
	// otherwise) instead of a second hardcoded copy.
	defaults := storage.DefaultCodingConfig()

	if cfg.Blacklist == nil {
		cfg.Blacklist = defaults.Blacklist
	}
	p.fileBlacklist = cfg.Blacklist

	if cfg.ShellTools == nil {
		cfg.ShellTools = defaults.ShellTools
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
		cfg.RawShell = defaults.RawShell
	}
	p.rawShell = cfg.RawShell

	storage.SaveCodingConfig(cfg)

	return nil
}

func (p *Provider) Close() error {
	return nil
}
