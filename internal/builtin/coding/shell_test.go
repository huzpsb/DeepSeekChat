package coding

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"hschat/internal/encoding"
	"hschat/internal/model"
)

func TestRunShellTool_Basic(t *testing.T) {
	p := setupProvider(t)
	p.fileBlacklist = []string{}

	result := p.runShellTool(context.Background(), model.ShellTool{
		Command: "echo test_output_123",
		Timeout: 10,
	}, p.getRootDir())
	checkContains(t, result, "test_output_123")
}

func TestRunShellTool_BlacklistBlock(t *testing.T) {
	p := setupProvider(t)
	os.WriteFile(filepath.Join(p.rootDir, "bad.go"), []byte("contains os/exec import\n"), 0644)
	p.fileBlacklist = []string{"os/exec"}

	result := p.runShellTool(context.Background(), model.ShellTool{
		Command: "echo should not run",
		Timeout: 10,
	}, p.getRootDir())
	checkContains(t, result, "ClamAV")
	checkContains(t, result, "potentially malicious")
}

func TestRunShellTool_BlacklistClean(t *testing.T) {
	p := setupProvider(t)
	os.WriteFile(filepath.Join(p.rootDir, "clean.go"), []byte("package main\n"), 0644)
	p.fileBlacklist = []string{"os/exec"}

	result := p.runShellTool(context.Background(), model.ShellTool{
		Command: "echo clean_pass",
		Timeout: 10,
	}, p.getRootDir())
	if strings.Contains(result, "ClamAV") {
		t.Errorf("expected no ClamAV block for clean file:\n%s", result)
	}
	checkContains(t, result, "clean_pass")
}

func TestRunShellTool_BlacklistEmpty(t *testing.T) {
	p := setupProvider(t)
	os.WriteFile(filepath.Join(p.rootDir, "bad.go"), []byte("os/exec"), 0644)
	p.fileBlacklist = []string{}

	result := p.runShellTool(context.Background(), model.ShellTool{
		Command: "echo should_run",
		Timeout: 10,
	}, p.getRootDir())
	if strings.Contains(result, "ClamAV") {
		t.Errorf("expected no ClamAV block when blacklist is empty")
	}
	checkContains(t, result, "should_run")
}

func TestRunShellTool_Timeout(t *testing.T) {
	p := setupProvider(t)
	p.fileBlacklist = []string{}

	cmd := "ping -n 10 127.0.0.1"
	if runtime.GOOS != "windows" {
		cmd = "ping -c 10 127.0.0.1"
	}
	result := p.runShellTool(context.Background(), model.ShellTool{
		Command: cmd,
		Timeout: 1,
	}, p.getRootDir())
	checkContains(t, result, "Exit Code")
}

func TestRunShellTool_IpynoredNames(t *testing.T) {
	p := setupProvider(t)
	os.MkdirAll(filepath.Join(p.rootDir, ".trash_can", "bad"), 0755)
	os.WriteFile(filepath.Join(p.rootDir, ".trash_can", "bad", "mal.go"), []byte("os/exec\n"), 0644)
	p.fileBlacklist = []string{"os/exec"}

	result := p.runShellTool(context.Background(), model.ShellTool{
		Command: "echo should_pass",
		Timeout: 10,
	}, p.getRootDir())
	if strings.Contains(result, "ClamAV") {
		t.Errorf("expected no ClamAV block for files in .trash_can:\n%s", result)
	}
}

func TestRunShellTool_RuntimeIgnored(t *testing.T) {
	p := setupProvider(t)
	os.MkdirAll(filepath.Join(p.rootDir, "_runtime"), 0755)
	os.WriteFile(filepath.Join(p.rootDir, "_runtime", "bad.go"), []byte("os/exec\n"), 0644)
	p.fileBlacklist = []string{"os/exec"}

	result := p.runShellTool(context.Background(), model.ShellTool{
		Command: "echo ok",
		Timeout: 10,
	}, p.getRootDir())
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

	result := p.runShellTool(context.Background(), model.ShellTool{
		Command: "echo should_run",
		Timeout: 10,
	}, p.getRootDir())
	if strings.Contains(result, "ClamAV") {
		t.Errorf("expected no ClamAV block for large file that is skipped:\n%s", result)
	}
}

func TestRunShellTool_StderrCapture(t *testing.T) {
	p := setupProvider(t)
	p.fileBlacklist = []string{}

	cmd := `cmd /c "echo stdout_text & echo stderr_text 1>&2"`
	if runtime.GOOS != "windows" {
		cmd = "echo stdout_text; echo stderr_text 1>&2"
	}
	result := p.runShellTool(context.Background(), model.ShellTool{
		Command: cmd,
		Timeout: 10,
	}, p.getRootDir())
	checkContains(t, result, "stdout_text")
	checkContains(t, result, "stderr_text")
	checkContains(t, result, "--- Stdout ---")
	checkContains(t, result, "--- Stderr ---")
}

func TestShellOutputString_WindowsGB18030(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows shell output decoding is only used on Windows")
	}

	got := encoding.DecodeGB18030([]byte{0xc4, 0xe3, 0xba, 0xc3, 0xca, 0xc0, 0xbd, 0xe7})
	if got != "你好世界" {
		t.Fatalf("expected decoded Chinese output, got %q", got)
	}
}

func TestRunShellTool_RawShellSkipsBlacklist(t *testing.T) {
	p := setupProvider(t)
	os.WriteFile(filepath.Join(p.rootDir, "bad.go"), []byte("contains os/exec import\n"), 0644)
	p.fileBlacklist = []string{"os/exec"}
	if runtime.GOOS == "windows" {
		p.rawShell = &model.RawShellConfig{Enabled: true, Shell: []string{"cmd.exe", "/c"}, Preamble: "$original"}
	} else {
		p.rawShell = &model.RawShellConfig{Enabled: true, Shell: []string{"sh", "-c"}, Preamble: "$original"}
	}

	result := p.runShellTool(context.Background(), model.ShellTool{
		Command: "echo raw_shell_pass",
		Timeout: 10,
	}, p.getRootDir())
	if strings.Contains(result, "ClamAV") {
		t.Errorf("raw_shell should skip blacklist, but got ClamAV block:\n%s", result)
	}
	checkContains(t, result, "raw_shell_pass")
}

func TestRunShellTool_RawShellSetsRootDir(t *testing.T) {
	p := setupProvider(t)
	p.fileBlacklist = []string{}
	if runtime.GOOS == "windows" {
		p.rawShell = &model.RawShellConfig{Enabled: true, Shell: []string{"cmd.exe", "/c"}, Preamble: "$original"}
	} else {
		p.rawShell = &model.RawShellConfig{Enabled: true, Shell: []string{"sh", "-c"}, Preamble: "$original"}
	}

	os.WriteFile(filepath.Join(p.rootDir, "sandbox_only.txt"), []byte("sandbox"), 0644)

	marker := "raw_shell_cwd_marker.txt"
	os.WriteFile(filepath.Join(p.rootDir, marker), []byte("cwd_content"), 0644)

	command := "type " + marker
	if runtime.GOOS != "windows" {
		command = "cat " + marker
	}
	result := p.runShellTool(context.Background(), model.ShellTool{
		Command: command,
		Timeout: 10,
	}, p.getRootDir())
	checkContains(t, result, "cwd_content")
}

func TestRunShellTool_WithoutRawShellSetsRootDir(t *testing.T) {
	p := setupProvider(t)
	p.fileBlacklist = []string{}

	os.WriteFile(filepath.Join(p.rootDir, "sandbox_only.txt"), []byte("sandbox_content"), 0644)

	command := "type sandbox_only.txt"
	if runtime.GOOS != "windows" {
		command = "cat sandbox_only.txt"
	}
	result := p.runShellTool(context.Background(), model.ShellTool{
		Command: command,
		Timeout: 10,
	}, p.getRootDir())
	checkContains(t, result, "sandbox_content")
}

func TestRunShellTool_RelativeOverwriteFalseUsesCurrentDir(t *testing.T) {
	p := setupProvider(t)
	p.fileBlacklist = []string{}
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	marker := "relative_overwrite_current_marker.txt"
	markerPath := filepath.Join(currentDir, marker)
	if err := os.WriteFile(markerPath, []byte("current-dir-ok"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	defer os.Remove(markerPath)
	relativeOverwrite := false
	command := "type " + marker
	if runtime.GOOS != "windows" {
		command = "cat " + marker
	}

	result := p.runShellTool(context.Background(), model.ShellTool{
		Command:           command,
		Timeout:           10,
		RelativeOverwrite: &relativeOverwrite,
	}, p.getRootDir())
	checkContains(t, result, "current-dir-ok")
}

func TestRunShellTool_InterruptKillsProcess(t *testing.T) {
	p := setupProvider(t)
	p.fileBlacklist = []string{}

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "ping 127.0.0.1 -n 60"
	} else {
		cmd = "ping 127.0.0.1 -c 60"
	}

	ctx, cancel := context.WithCancel(context.Background())

	start := time.Now()
	done := make(chan string, 1)
	go func() {
		done <- p.runShellTool(ctx, model.ShellTool{
			Command: cmd,
			Timeout: 30,
		}, p.getRootDir())
	}()

	time.Sleep(time.Second)
	cancel()

	result := <-done
	elapsed := time.Since(start)

	if elapsed > 8*time.Second {
		t.Errorf("interrupt should kill process quickly, took %v", elapsed)
	}

	if !strings.Contains(result, "Exit Code") {
		t.Errorf("expected Exit Code in result, got: %s", result)
	}

	t.Logf("killed after %v, exit info: %s", elapsed, result)
}
