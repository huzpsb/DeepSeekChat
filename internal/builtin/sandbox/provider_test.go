package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafePathWithWarningAllowsAbsolutePathInsideRoot(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "file.txt")

	got, warn, err := SafePathWithWarning(root, want)
	if err != nil {
		t.Fatalf("expected absolute path inside root to be allowed: %v", err)
	}
	if !warn {
		t.Fatalf("expected absolute path warning")
	}
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestSafePathWithWarningRejectsAbsolutePathOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "file.txt")

	if _, _, err := SafePathWithWarning(root, outside); err == nil {
		t.Fatalf("expected absolute path outside root to be rejected")
	}
}

func TestCreateFileAllowsAbsolutePathInsideRootWithWarning(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "created.txt")
	p := &Provider{rootDir: root}

	result := p.createFile(map[string]any{"file": file, "content": "hello"})
	if !strings.Contains(result, "Success") {
		t.Fatalf("expected success, got %q", result)
	}
	if !strings.Contains(result, "WARNING: Please use relative path.") {
		t.Fatalf("expected warning, got %q", result)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("expected file to be created: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("expected content %q, got %q", "hello", string(data))
	}
}
