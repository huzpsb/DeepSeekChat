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
	// pictureResolver resolves (endpoint, apiKey, model) for the
	// check_picture vision sub-agent from the live model_providers
	// registry. Set once by the MCP manager after construction; read on
	// every check_picture call so a provider switch applies without a
	// restart.
	pictureResolver func() (endpoint, apiKey, model string, err error)
}

// SetPictureAgentResolver installs the live resolver used by the
// check_picture tool (see the pictureResolver field).
func (p *Provider) SetPictureAgentResolver(fn func() (endpoint, apiKey, model string, err error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pictureResolver = fn
}

// getPictureResolver returns the resolver under the read lock so a
// withRootDir copy (which shares nothing but copied fields) and a
// concurrent SetPictureAgentResolver cannot race.
func (p *Provider) getPictureResolver() func() (string, string, string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.pictureResolver
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
func (p *Provider) withRootDir(dir string) *Provider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return &Provider{
		rootDir:         ResolveLiveRootDir(dir, p.rootDir),
		extBlacklist:    p.extBlacklist,
		sandboxDisabled: p.sandboxDisabled,
		pictureResolver: p.pictureResolver,
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

// ResolveLiveRootDir resolves dir to an absolute path, falling back to
// fallback (also resolved) when dir no longer exists on disk. A deleted
// project directory must never be resurrected by an implicit MkdirAll, nor
// cause tool failures: everything falls back to the provider default.
func ResolveLiveRootDir(dir, fallback string) string {
	resolved := ResolveRootDir(dir)
	if info, err := os.Stat(resolved); err == nil && info.IsDir() {
		return resolved
	}
	return ResolveRootDir(fallback)
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
