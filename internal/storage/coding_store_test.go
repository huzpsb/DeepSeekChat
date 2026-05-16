package storage

import (
	"os"
	"testing"

	"hschat/internal/model"
)

func TestLoadCodingConfig_NotExist(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg, err := LoadCodingConfig()
	if err != nil {
		t.Fatalf("LoadCodingConfig should not error when file missing: %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected non-nil config")
	}
	if cfg.RootDir != "" {
		t.Errorf("expected empty root_dir, got '%s'", cfg.RootDir)
	}
	if cfg.ShellTools != nil {
		t.Errorf("expected nil shell_tools, got %v", cfg.ShellTools)
	}
}

func TestSaveAndLoadCodingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg := &model.CodingConfig{
		RootDir: "/tmp/project",
		ShellTools: map[string]model.ShellTool{
			"build": {Description: "Build project", Command: "go build ./...", Timeout: 120},
		},
		Blacklist:    []string{"os/exec", "net/http"},
		ExtBlacklist: []string{"exe", "dll"},
	}

	err := SaveCodingConfig(cfg)
	if err != nil {
		t.Fatalf("SaveCodingConfig failed: %v", err)
	}

	loaded, err := LoadCodingConfig()
	if err != nil {
		t.Fatalf("LoadCodingConfig failed: %v", err)
	}
	if loaded.RootDir != "/tmp/project" {
		t.Errorf("expected '/tmp/project', got '%s'", loaded.RootDir)
	}
	if len(loaded.ShellTools) != 1 {
		t.Fatalf("expected 1 shell tool, got %d", len(loaded.ShellTools))
	}
	if loaded.ShellTools["build"].Command != "go build ./..." {
		t.Errorf("expected 'go build ./...', got '%s'", loaded.ShellTools["build"].Command)
	}
	if loaded.ShellTools["build"].Timeout != 120 {
		t.Errorf("expected timeout 120, got %d", loaded.ShellTools["build"].Timeout)
	}
	if len(loaded.Blacklist) != 2 || loaded.Blacklist[0] != "os/exec" {
		t.Errorf("expected blacklist ['os/exec','net/http'], got %v", loaded.Blacklist)
	}
	if len(loaded.ExtBlacklist) != 2 {
		t.Errorf("expected 2 ext blacklist items, got %d", len(loaded.ExtBlacklist))
	}
}

func TestSaveCodingConfig_Overwrite(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg1 := &model.CodingConfig{RootDir: "/a"}
	cfg2 := &model.CodingConfig{RootDir: "/b"}

	SaveCodingConfig(cfg1)
	SaveCodingConfig(cfg2)

	loaded, _ := LoadCodingConfig()
	if loaded.RootDir != "/b" {
		t.Errorf("expected '/b' after overwrite, got '%s'", loaded.RootDir)
	}
}

func TestLoadCodingConfig_Corrupt(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	os.WriteFile("coding.json", []byte("not valid json"), 0644)

	_, err := LoadCodingConfig()
	if err == nil {
		t.Errorf("expected error for corrupt config")
	}
}

func TestSaveCodingConfig_NilFields(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg := &model.CodingConfig{}
	err := SaveCodingConfig(cfg)
	if err != nil {
		t.Fatalf("SaveCodingConfig with empty config failed: %v", err)
	}

	loaded, _ := LoadCodingConfig()
	if loaded.RootDir != "" {
		t.Errorf("expected empty root_dir")
	}
}

func TestSaveCodingConfig_MultipleShellTools(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg := &model.CodingConfig{
		ShellTools: map[string]model.ShellTool{
			"a": {Description: "A", Command: "cmd_a", Timeout: 10},
			"b": {Description: "B", Command: "cmd_b", Timeout: 20},
			"c": {Description: "C", Command: "cmd_c", Timeout: 0},
		},
	}

	err := SaveCodingConfig(cfg)
	if err != nil {
		t.Fatalf("SaveCodingConfig failed: %v", err)
	}

	loaded, _ := LoadCodingConfig()
	if len(loaded.ShellTools) != 3 {
		t.Fatalf("expected 3 shell tools, got %d", len(loaded.ShellTools))
	}
	if loaded.ShellTools["c"].Timeout != 0 {
		t.Errorf("expected timeout 0 for c, got %d", loaded.ShellTools["c"].Timeout)
	}
}
