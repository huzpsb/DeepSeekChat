package model

import (
	"strings"
	"testing"
)

func checkPictureRegistry() []ModelProvider {
	return []ModelProvider{
		{Name: "deepseek", Endpoint: "https://api.deepseek.com/chat/completions", APIKey: "sk-ds", Models: []string{"deepseek-flash", "deepseek-v4-pro"}},
		{Name: "qwen", Endpoint: "https://qwen.example/v1/chat/completions", APIKey: "sk-qw", Models: []string{"qv"}},
	}
}

// An empty selection resolves to the deepseek provider and its first
// model (deepseek-flash in the default registry).
func TestResolveCheckPicture_Defaults(t *testing.T) {
	cfg := &MCPConfig{ModelProviders: checkPictureRegistry()}
	ep, key, m, err := cfg.ResolveCheckPicture()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep != "https://api.deepseek.com/chat/completions" || key != "sk-ds" || m != "deepseek-flash" {
		t.Errorf("got endpoint=%q key=%q model=%q", ep, key, m)
	}
}

func TestResolveCheckPicture_Explicit(t *testing.T) {
	cfg := &MCPConfig{
		ModelProviders: checkPictureRegistry(),
		CheckPicture:   CheckPictureConfig{Provider: "qwen", Model: "qv"},
	}
	ep, key, m, err := cfg.ResolveCheckPicture()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep != "https://qwen.example/v1/chat/completions" || key != "sk-qw" || m != "qv" {
		t.Errorf("got endpoint=%q key=%q model=%q", ep, key, m)
	}
}

// A stale model (e.g. removed from the provider's list) falls back to the
// provider's first model, mirroring ResolveModel.
func TestResolveCheckPicture_StaleModelFallsBackToFirst(t *testing.T) {
	cfg := &MCPConfig{
		ModelProviders: checkPictureRegistry(),
		CheckPicture:   CheckPictureConfig{Provider: "qwen", Model: "gone"},
	}
	_, _, m, err := cfg.ResolveCheckPicture()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != "qv" {
		t.Errorf("expected fallback to first model qv, got %q", m)
	}
}

// A missing provider must error: silently sending the picture to another
// registry entry would leak it to an endpoint the user did not pick.
func TestResolveCheckPicture_MissingProvider_Errors(t *testing.T) {
	cfg := &MCPConfig{
		ModelProviders: checkPictureRegistry(),
		CheckPicture:   CheckPictureConfig{Provider: "nope"},
	}
	_, _, _, err := cfg.ResolveCheckPicture()
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("expected missing-provider error naming the provider, got %v", err)
	}
}

// The default fallback itself errors when the registry has no deepseek
// entry (e.g. the user deleted it).
func TestResolveCheckPicture_NoDeepseekInRegistry(t *testing.T) {
	cfg := &MCPConfig{ModelProviders: []ModelProvider{
		{Name: "qwen", Endpoint: "e", APIKey: "k", Models: []string{"qv"}},
	}}
	_, _, _, err := cfg.ResolveCheckPicture()
	if err == nil || !strings.Contains(err.Error(), `"deepseek"`) {
		t.Errorf("expected deepseek-fallback error, got %v", err)
	}
}
