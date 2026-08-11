package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"hschat/internal/builtin"
	"hschat/internal/model"
)

type rootDirCtxKey struct{}

// WithRootDir attaches a per-call sandbox root dir override to the context.
func WithRootDir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, rootDirCtxKey{}, dir)
}

// RootDirFromContext returns the root dir override attached to ctx, if any.
func RootDirFromContext(ctx context.Context) (string, bool) {
	dir, ok := ctx.Value(rootDirCtxKey{}).(string)
	return dir, ok && dir != ""
}

type Provider struct {
	mu              sync.RWMutex
	rootDir         string
	extBlacklist    []string
	sandboxDisabled bool
}

func New(cfg *model.SandboxConfig) builtin.Provider {
	p := &Provider{
		extBlacklist:    cfg.ExtBlacklist,
		sandboxDisabled: cfg.SandboxDisabled,
	}

	rootDir := ResolveRootDir(cfg.DefaultRootDir())
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		panic(fmt.Sprintf("failed to create root directory %s: %v", rootDir, err))
	}
	p.rootDir = rootDir

	return p
}

func (p *Provider) Name() string {
	return "Sandbox"
}

func (p *Provider) Initialize(_ string) error {
	return nil
}

func (p *Provider) Close() error {
	return nil
}

func (p *Provider) SetSandboxDisabled(disabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sandboxDisabled = disabled
}

// SetRootDir switches the sandbox root at runtime, creating the directory
// if needed.
func (p *Provider) SetRootDir(dir string) error {
	rootDir := ResolveRootDir(dir)
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return fmt.Errorf("failed to create root directory %s: %v", rootDir, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rootDir = rootDir
	return nil
}

func (p *Provider) getRootDir() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.rootDir
}

// withRootDir returns a lightweight copy of the provider rooted at dir.
// Used for per-call overrides carried by the tool-call context.
func (p *Provider) withRootDir(dir string) *Provider {
	rootDir := ResolveRootDir(dir)
	_ = os.MkdirAll(rootDir, 0755)
	p.mu.RLock()
	defer p.mu.RUnlock()
	return &Provider{
		rootDir:         rootDir,
		extBlacklist:    p.extBlacklist,
		sandboxDisabled: p.sandboxDisabled,
	}
}

func ResolveRootDir(dir string) string {
	if dir == "" {
		dir = filepath.Join(".", "agent")
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// ValidateRootDir resolves dir and ensures the directory can be created,
// mirroring the checks performed when a root dir is activated.
func ValidateRootDir(dir string) error {
	rootDir := ResolveRootDir(dir)
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return fmt.Errorf("failed to create root directory %s: %v", rootDir, err)
	}
	return nil
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
	p.mu.RLock()
	defer p.mu.RUnlock()
	path, _, err := safePath(p.rootDir, rel, p.sandboxDisabled)
	return path, err
}

func (p *Provider) getSafePathWithWarning(rel string) (string, bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return safePath(p.rootDir, rel, p.sandboxDisabled)
}
