package coding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hschat/internal/builtin"
	"hschat/internal/model"
	"hschat/internal/storage"
)

type Provider struct {
	rootDir       string
	shellTools    map[string]model.ShellTool
	fileBlacklist []string
	extBlacklist  []string
}

func New() builtin.Provider {
	return &Provider{
		shellTools:    make(map[string]model.ShellTool),
		fileBlacklist: []string{},
		extBlacklist:  []string{},
	}
}

func (p *Provider) Name() string {
	return "CodingMCP"
}

func (p *Provider) Initialize(configPath string) error {
	cfg, err := storage.LoadCodingConfig()
	if err != nil {
		return err
	}

	// default rootDir = current working directory
	if cfg.RootDir == "" {
		cfg.RootDir, _ = os.Getwd()
	}
	if abs, err := filepath.Abs(cfg.RootDir); err == nil {
		cfg.RootDir = abs
	}
	p.rootDir = cfg.RootDir

	// defaults
	if cfg.Blacklist == nil {
		cfg.Blacklist = []string{"os/exec"}
	}
	p.fileBlacklist = cfg.Blacklist

	if cfg.ExtBlacklist == nil {
		cfg.ExtBlacklist = []string{"exe", "dll", "ppt", "pptx", "doc", "docx", "pdf"}
	}
	p.extBlacklist = cfg.ExtBlacklist

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

	// Save the config (auto-generate coding.json if it doesn't exist)
	storage.SaveCodingConfig(cfg)

	return nil
}

func (p *Provider) Close() error {
	return nil
}

func (p *Provider) getSafePath(rel string) (string, error) {
	cleanRel := filepath.Clean(rel)
	if strings.HasPrefix(cleanRel, "/") || strings.HasPrefix(cleanRel, "\\") {
		cleanRel = cleanRel[1:]
	}
	target := filepath.Join(p.rootDir, cleanRel)
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absTarget, p.rootDir) {
		return "", fmt.Errorf("security error: access denied to path outside root directory")
	}
	return absTarget, nil
}

func (p *Provider) trashPath(base string) string {
	trashDir := filepath.Join(p.rootDir, ".trash_can")
	_ = os.MkdirAll(trashDir, 0755)
	timestamp := (time.Now().UnixNano()) % 1_0000_0000
	return filepath.Join(trashDir, fmt.Sprintf("%d_%s", timestamp, filepath.Base(base)))
}

func (p *Provider) moveToTrash(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return os.Remove(path)
	}
	return os.Rename(path, p.trashPath(path))
}

func isIgnoredName(name string) bool {
	return name == ".trash_can" || name == "_runtime"
}
