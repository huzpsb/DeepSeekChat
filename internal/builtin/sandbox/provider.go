package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hschat/internal/builtin"
	"hschat/internal/model"
)

type Provider struct {
	rootDir      string
	extBlacklist []string
}

func New(cfg *model.SandboxConfig) builtin.Provider {
	p := &Provider{
		extBlacklist: cfg.ExtBlacklist,
	}

	if cfg.RootDir == "" {
		cfg.RootDir = filepath.Join(".", "agent")
	}
	if abs, err := filepath.Abs(cfg.RootDir); err == nil {
		cfg.RootDir = abs
	}
	if err := os.MkdirAll(cfg.RootDir, 0755); err != nil {
		panic(fmt.Sprintf("failed to create root directory %s: %v", cfg.RootDir, err))
	}
	p.rootDir = cfg.RootDir

	return p
}

func (p *Provider) Name() string {
	return "Sandbox"
}

func (p *Provider) Initialize(configPath string) error {
	return nil
}

func (p *Provider) Close() error {
	return nil
}

func SafePath(rootDir, rel string) (string, error) {
	cleanRel := filepath.Clean(rel)

	if strings.Contains(rel, "..") {
		trimmed := strings.TrimPrefix(cleanRel, "/")
		trimmed = strings.TrimPrefix(trimmed, "\\")
		if trimmed == "" || trimmed == "." {
			return "", fmt.Errorf("security error: path traversal detected")
		}
	}

	cleanRel = strings.TrimPrefix(cleanRel, "/")
	cleanRel = strings.TrimPrefix(cleanRel, "\\")

	target := filepath.Join(rootDir, cleanRel)
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}

	absRoot, _ := filepath.Abs(rootDir)
	relPath, err := filepath.Rel(absRoot, absTarget)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("security error: access denied to path outside root directory")
	}

	return absTarget, nil
}

func (p *Provider) getSafePath(rel string) (string, error) {
	return SafePath(p.rootDir, rel)
}
