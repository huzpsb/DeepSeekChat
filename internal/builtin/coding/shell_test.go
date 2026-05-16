package coding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hschat/internal/model"
)

func TestRunShellTool_Basic(t *testing.T) {
	p := setupProvider(t)
	p.fileBlacklist = []string{}

	result := p.runShellTool(model.ShellTool{
		Command: "echo test_output_123",
		Timeout: 10,
	})
	checkContains(t, result, "test_output_123")
}

func TestRunShellTool_BlacklistBlock(t *testing.T) {
	p := setupProvider(t)
	os.WriteFile(filepath.Join(p.rootDir, "bad.go"), []byte("contains os/exec import\n"), 0644)
	p.fileBlacklist = []string{"os/exec"}

	result := p.runShellTool(model.ShellTool{
		Command: "echo should not run",
		Timeout: 10,
	})
	checkContains(t, result, "ClamAV")
	checkContains(t, result, "potentially malicious")
}

func TestRunShellTool_BlacklistClean(t *testing.T) {
	p := setupProvider(t)
	os.WriteFile(filepath.Join(p.rootDir, "clean.go"), []byte("package main\n"), 0644)
	p.fileBlacklist = []string{"os/exec"}

	result := p.runShellTool(model.ShellTool{
		Command: "echo clean_pass",
		Timeout: 10,
	})
	if strings.Contains(result, "ClamAV") {
		t.Errorf("expected no ClamAV block for clean file:\n%s", result)
	}
	checkContains(t, result, "clean_pass")
}

func TestRunShellTool_BlacklistEmpty(t *testing.T) {
	p := setupProvider(t)
	os.WriteFile(filepath.Join(p.rootDir, "bad.go"), []byte("os/exec"), 0644)
	p.fileBlacklist = []string{}

	result := p.runShellTool(model.ShellTool{
		Command: "echo should_run",
		Timeout: 10,
	})
	if strings.Contains(result, "ClamAV") {
		t.Errorf("expected no ClamAV block when blacklist is empty")
	}
	checkContains(t, result, "should_run")
}

func TestRunShellTool_Timeout(t *testing.T) {
	p := setupProvider(t)
	p.fileBlacklist = []string{}

	result := p.runShellTool(model.ShellTool{
		Command: "ping -n 10 127.0.0.1",
		Timeout: 1,
	})
	// should mention exit code (timeout or killed)
	checkContains(t, result, "Exit Code")
}

func TestRunShellTool_IpynoredNames(t *testing.T) {
	p := setupProvider(t)
	os.MkdirAll(filepath.Join(p.rootDir, ".trash_can", "bad"), 0755)
	os.WriteFile(filepath.Join(p.rootDir, ".trash_can", "bad", "mal.go"), []byte("os/exec\n"), 0644)
	p.fileBlacklist = []string{"os/exec"}

	result := p.runShellTool(model.ShellTool{
		Command: "echo should_pass",
		Timeout: 10,
	})
	if strings.Contains(result, "ClamAV") {
		t.Errorf("expected no ClamAV block for files in .trash_can:\n%s", result)
	}
}

func TestRunShellTool_RuntimeIgnored(t *testing.T) {
	p := setupProvider(t)
	os.MkdirAll(filepath.Join(p.rootDir, "_runtime"), 0755)
	os.WriteFile(filepath.Join(p.rootDir, "_runtime", "bad.go"), []byte("os/exec\n"), 0644)
	p.fileBlacklist = []string{"os/exec"}

	result := p.runShellTool(model.ShellTool{
		Command: "echo ok",
		Timeout: 10,
	})
	if strings.Contains(result, "ClamAV") {
		t.Errorf("expected no ClamAV block for files in _runtime:\n%s", result)
	}
}

func TestRunShellTool_LargeFileSkip(t *testing.T) {
	p := setupProvider(t)
	p.fileBlacklist = []string{"os/exec"}

	// Create a file larger than 1MB that would contain blacklisted content
	bigFile := filepath.Join(p.rootDir, "big.bin")
	f, _ := os.Create(bigFile)
	f.WriteString("os/exec\n")
	// pad to > 1MB
	f.Write(make([]byte, 1024*1024+1))
	f.Close()

	result := p.runShellTool(model.ShellTool{
		Command: "echo should_run",
		Timeout: 10,
	})
	if strings.Contains(result, "ClamAV") {
		t.Errorf("expected no ClamAV block for large file that is skipped:\n%s", result)
	}
}

func TestRunShellTool_StderrCapture(t *testing.T) {
	p := setupProvider(t)
	p.fileBlacklist = []string{}

	result := p.runShellTool(model.ShellTool{
		Command: "cmd /c \"echo stdout_text & echo stderr_text 1>&2\"",
		Timeout: 10,
	})
	checkContains(t, result, "stdout_text")
	checkContains(t, result, "stderr_text")
	checkContains(t, result, "--- Stdout ---")
	checkContains(t, result, "--- Stderr ---")
}
