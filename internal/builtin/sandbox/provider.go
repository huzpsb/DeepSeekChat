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
	rootDir             string
	extBlacklist        []string
	disablePathWarnings bool
}

func New(cfg *model.SandboxConfig) builtin.Provider {
	p := &Provider{
		extBlacklist:        cfg.ExtBlacklist,
		disablePathWarnings: cfg.DisablePathWarnings,
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

func (p *Provider) SetDisablePathWarnings(disable bool) {
	p.disablePathWarnings = disable
}

func SafePath(rootDir, rel string) (string, error) {
	path, _, err := SafePathWithWarning(rootDir, rel)
	return path, err
}

func SafePathWithWarning(rootDir, rel string) (string, bool, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", false, err
	}

	warn := false
	target := rel
	if filepath.IsAbs(rel) {
		warn = true
		target = filepath.Clean(rel)
	} else {
		cleanRel := filepath.Clean(rel)
		if strings.HasPrefix(cleanRel, "/") || strings.HasPrefix(cleanRel, "\\") {
			warn = true
		}

		cleanRel = strings.TrimPrefix(cleanRel, "/")
		cleanRel = strings.TrimPrefix(cleanRel, "\\")
		target = filepath.Join(absRoot, cleanRel)
	}

	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", false, err
	}

	relPath, err := filepath.Rel(absRoot, absTarget)
	if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", false, fmt.Errorf("sandbox fs error: access denied (-1)")
	}

	return absTarget, warn, nil
}

func (p *Provider) getSafePath(rel string) (string, error) {
	return SafePath(p.rootDir, rel)
}

func (p *Provider) getSafePathWithWarning(rel string) (string, bool, error) {
	return SafePathWithWarning(p.rootDir, rel)
}
