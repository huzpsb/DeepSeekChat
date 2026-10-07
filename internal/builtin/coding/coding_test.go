package coding

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"hschat/internal/model"
)

func setupProvider(t *testing.T) *Provider {
	t.Helper()
	tmpDir := t.TempDir()

	p := &Provider{
		rootDir:       tmpDir,
		shellTools:    make(map[string]model.ShellTool),
		fileBlacklist: []string{},
	}

	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("Hello World\nLine 2\nLine 3"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "subdir", "data.txt"), []byte("alpha\nbeta\ngamma\ndelta"), 0644)

	return p
}

func TestName(t *testing.T) {
	p := &Provider{}
	if p.Name() != "Coding" {
		t.Errorf("expected 'Coding', got '%s'", p.Name())
	}
}

func TestClose(t *testing.T) {
	p := setupProvider(t)
	if err := p.Close(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestTools(t *testing.T) {
	p := setupProvider(t)
	p.shellTools = map[string]model.ShellTool{
		"build": {Description: "build project", Command: "go build", Timeout: 60},
	}

	tools := p.Tools()

	shellFound := false
	for _, tool := range tools {
		if tool.Name == "build" {
			shellFound = true
			if tool.Description != "build project" {
				t.Errorf("expected description 'build project', got '%s'", tool.Description)
			}
		}
	}
	if !shellFound {
		t.Errorf("expected shell tool 'build' in Tools()")
	}

	if len(tools) != 1 {
		t.Errorf("expected 1 shell tool, got %d", len(tools))
	}
}

func TestTools_NoShellTools(t *testing.T) {
	p := setupProvider(t)
	tools := p.Tools()
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestTools_RunWithRawShell(t *testing.T) {
	p := setupProvider(t)
	if runtime.GOOS == "windows" {
		p.rawShell = &model.RawShellConfig{Enabled: true, Shell: []string{"cmd.exe", "/c"}, Preamble: "$original"}
	} else {
		p.rawShell = &model.RawShellConfig{Enabled: true, Shell: []string{"sh", "-c"}, Preamble: "$original"}
	}

	tools := p.Tools()

	runFound := false
	for _, tool := range tools {
		if tool.Name == "run" {
			runFound = true
			schema, ok := tool.InputSchema.(map[string]any)
			if !ok {
				t.Fatalf("expected InputSchema to be map[string]any")
			}
			props, ok := schema["properties"].(map[string]any)
			if !ok {
				t.Fatalf("expected properties in run schema")
			}
			to, ok := props["time_out"].(map[string]any)
			if !ok {
				t.Fatalf("expected time_out property in run schema")
			}
			if v, ok := to["default"].(int); !ok || v != 300 {
				t.Errorf("expected time_out default 300, got %v", to["default"])
			}
			oso, ok := props["output_size_limit"].(map[string]any)
			if !ok {
				t.Fatalf("expected output_size_limit property in run schema")
			}
			if v, ok := oso["default"].(int); !ok || v != defaultRunOutputSizeLimit {
				t.Errorf("expected output_size_limit default %d, got %v", defaultRunOutputSizeLimit, oso["default"])
			}
		}
	}
	if !runFound {
		t.Errorf("expected 'run' tool when rawShell is enabled")
	}
}

func TestCallTool_Unknown(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool(context.Background(), "unknown_tool_name", map[string]any{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "Error: Unknown tool" {
		t.Errorf("expected 'Error: Unknown tool', got '%s'", result.Content[0].Text)
	}
}

func TestCallTool_Run_TimeoutDefault(t *testing.T) {
	p := setupProvider(t)
	if runtime.GOOS == "windows" {
		p.rawShell = &model.RawShellConfig{Enabled: true, Shell: []string{"cmd.exe", "/c"}, Preamble: "$original"}
	} else {
		p.rawShell = &model.RawShellConfig{Enabled: true, Shell: []string{"sh", "-c"}, Preamble: "$original"}
	}

	result, err := p.CallTool(context.Background(), "run", map[string]any{
		"command": "echo ok",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "ok") {
		t.Errorf("expected 'ok' in output:\n%s", result.Content[0].Text)
	}
}

func TestCallTool_Run_CustomTimeout(t *testing.T) {
	p := setupProvider(t)
	if runtime.GOOS == "windows" {
		p.rawShell = &model.RawShellConfig{Enabled: true, Shell: []string{"cmd.exe", "/c"}, Preamble: "$original"}
	} else {
		p.rawShell = &model.RawShellConfig{Enabled: true, Shell: []string{"sh", "-c"}, Preamble: "$original"}
	}

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "ping -n 10 127.0.0.1"
	} else {
		cmd = "ping -c 10 127.0.0.1"
	}
	result, err := p.CallTool(context.Background(), "run", map[string]any{
		"command":  cmd,
		"time_out": float64(1),
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "Exit Code") {
		t.Errorf("expected timeout exit:\n%s", result.Content[0].Text)
	}
}

func TestCallTool_Run_EmptyCommand(t *testing.T) {
	p := setupProvider(t)
	if runtime.GOOS == "windows" {
		p.rawShell = &model.RawShellConfig{Enabled: true, Shell: []string{"cmd.exe", "/c"}, Preamble: "$original"}
	} else {
		p.rawShell = &model.RawShellConfig{Enabled: true, Shell: []string{"sh", "-c"}, Preamble: "$original"}
	}

	result, err := p.CallTool(context.Background(), "run", map[string]any{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "no command provided") {
		t.Errorf("expected 'no command provided' in output:\n%s", result.Content[0].Text)
	}
}

func TestCallTool_ShellTool(t *testing.T) {
	p := setupProvider(t)
	p.shellTools = map[string]model.ShellTool{
		"echo_test": {Description: "echo", Command: "echo hello", Timeout: 10},
	}

	result, err := p.CallTool(context.Background(), "echo_test", map[string]any{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "hello") {
		t.Errorf("expected 'hello' in output:\n%s", text)
	}
}

func checkContains(t *testing.T, text, sub string) {
	t.Helper()
	for i := 0; i <= len(text)-len(sub); i++ {
		if text[i:i+len(sub)] == sub {
			return
		}
	}
	t.Errorf("expected text to contain '%s', got:\n%s", sub, text)
}

// ============================================================
// Regression: raw shell config robustness (audit fixes)
// ============================================================

func rawShellCfgForOS() *model.RawShellConfig {
	if runtime.GOOS == "windows" {
		return &model.RawShellConfig{Enabled: true, Shell: []string{"cmd.exe", "/c"}, Preamble: "$original"}
	}
	return &model.RawShellConfig{Enabled: true, Shell: []string{"sh", "-c"}, Preamble: "$original"}
}

// An explicitly non-positive time_out used to expire the context the
// moment it was created, killing the command before any output.
func TestCallTool_Run_NonPositiveTimeoutClamped(t *testing.T) {
	p := setupProvider(t)
	p.rawShell = rawShellCfgForOS()

	result, err := p.CallTool(context.Background(), "run", map[string]any{
		"command":  "echo ok",
		"time_out": 0,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "ok") {
		t.Errorf("expected 'ok' in output:\n%s", text)
	}
	if strings.Contains(text, "context deadline exceeded") {
		t.Errorf("command was killed by an instantly-expired context:\n%s", text)
	}
}

// A negative timeout gets clamped too, and a large-enough custom timeout
// still works.
func TestCallTool_Run_NegativeTimeoutClamped(t *testing.T) {
	p := setupProvider(t)
	p.rawShell = rawShellCfgForOS()

	result, err := p.CallTool(context.Background(), "run", map[string]any{
		"command":  "echo ok",
		"time_out": -5,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "ok") {
		t.Errorf("expected 'ok' in output:\n%s", result.Content[0].Text)
	}
}

// raw_shell.shell = [] used to panic on Shell[0] both when listing tools
// and when executing.
func TestCallTool_Run_EmptyRawShellArgv(t *testing.T) {
	p := setupProvider(t)
	p.rawShell = &model.RawShellConfig{Enabled: true, Shell: []string{}, Preamble: "$original"}

	tools := p.Tools()
	foundRun := false
	for _, td := range tools {
		if td.Name == "run" {
			foundRun = true
		}
	}
	if !foundRun {
		t.Fatalf("expected 'run' tool in %+v", tools)
	}

	result, err := p.CallTool(context.Background(), "run", map[string]any{
		"command": "echo ok",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "ok") {
		t.Errorf("expected 'ok' in output:\n%s", result.Content[0].Text)
	}
}

// Named shell tools also list safely with an empty raw shell argv.
func TestTools_EmptyRawShellArgv_NamedTools(t *testing.T) {
	p := setupProvider(t)
	p.shellTools["go_test"] = model.ShellTool{Description: "Run go test", Command: "go test ./...", Timeout: 60}
	p.rawShell = &model.RawShellConfig{Enabled: true, Shell: []string{}, Preamble: "$original"}

	tools := p.Tools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools (go_test + run), got %d: %+v", len(tools), tools)
	}
}
