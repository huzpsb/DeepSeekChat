package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafePathAllowsAbsolutePathInsideRoot(t *testing.T) {
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

func TestSafePathRejectsAbsolutePathOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "file.txt")

	if _, _, err := SafePathWithWarning(root, outside); err == nil {
		t.Fatalf("expected absolute path outside root to be rejected")
	}
}

func TestCreateFileWithAbsolutePath(t *testing.T) {
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

func TestRmWithAbsolutePathTrashesFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "to_delete.txt")
	os.WriteFile(file, []byte("delete me"), 0644)

	p := &Provider{rootDir: root}
	result := p.rm(map[string]any{"file": file})
	if !strings.Contains(result, "Success") {
		t.Fatalf("expected Success, got %q", result)
	}

	if _, err := os.Stat(file); err == nil {
		t.Fatalf("expected file to be removed from original location")
	}

	trashDir := filepath.Join(root, ".trash_can")
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		t.Fatalf("expected trashcan directory to exist: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_to_delete.txt") {
			found = true
			data, _ := os.ReadFile(filepath.Join(trashDir, e.Name()))
			if string(data) != "delete me" {
				t.Fatalf("expected trash content 'delete me', got %q", string(data))
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected file in trashcan, got entries: %v", entries)
	}
}

func TestRmWithAbsolutePathOutsideRootRejected(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	os.WriteFile(outside, []byte("danger"), 0644)

	p := &Provider{rootDir: root}
	result := p.rm(map[string]any{"file": outside})
	if !strings.Contains(result, "access denied") {
		t.Fatalf("expected access denied, got %q", result)
	}

	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("expected outside file to still exist: %v", err)
	}
}
