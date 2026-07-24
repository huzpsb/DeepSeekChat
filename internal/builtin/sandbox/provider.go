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
	rootDir         string
	extBlacklist    []string
	sandboxDisabled bool
}

func New(cfg *model.SandboxConfig) builtin.Provider {
	p := &Provider{
		extBlacklist:    cfg.ExtBlacklist,
		sandboxDisabled: cfg.SandboxDisabled,
	}

	rootDir := cfg.RootDir
	if rootDir == "" {
		rootDir = filepath.Join(".", "agent")
	}
	if abs, err := filepath.Abs(rootDir); err == nil {
		rootDir = abs
	}
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		panic(fmt.Sprintf("failed to create root directory %s: %v", rootDir, err))
	}
	p.rootDir = rootDir

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

func (p *Provider) SetSandboxDisabled(disabled bool) {
	p.sandboxDisabled = disabled
}

func SafePath(rootDir, rel string) (string, error) {
	path, _, err := safePath(rootDir, rel, false)
	return path, err
}

func SafePathWithWarning(rootDir, rel string) (string, bool, error) {
	return safePath(rootDir, rel, false)
}

func safePath(rootDir, rel string, sandboxDisabled bool) (string, bool, error) {
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

	if !sandboxDisabled {
		relPath, err := filepath.Rel(absRoot, absTarget)
		if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
			return "", false, fmt.Errorf("sandbox fs error: access denied (-1)")
		}
	}

	return absTarget, warn, nil
}

func (p *Provider) getSafePath(rel string) (string, error) {
	path, _, err := safePath(p.rootDir, rel, p.sandboxDisabled)
	return path, err
}

func (p *Provider) getSafePathWithWarning(rel string) (string, bool, error) {
	return safePath(p.rootDir, rel, p.sandboxDisabled)
}
