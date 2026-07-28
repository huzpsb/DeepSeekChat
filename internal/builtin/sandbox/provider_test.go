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

func TestSearchName_GlobDefault(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "foo.go"), nil, 0644)
	os.WriteFile(filepath.Join(root, "bar.txt"), nil, 0644)
	os.WriteFile(filepath.Join(root, "foo_test.go"), nil, 0644)

	p := &Provider{rootDir: root}

	// glob: *.go matches both .go files
	result := p.searchName(map[string]any{"keyword": "*.go", "dir": root})
	if !strings.Contains(result, "foo.go") {
		t.Fatalf("expected foo.go, got %q", result)
	}
	if !strings.Contains(result, "foo_test.go") {
		t.Fatalf("expected foo_test.go, got %q", result)
	}
	if strings.Contains(result, "bar.txt") {
		t.Fatalf("expected bar.txt NOT to match *.go, got %q", result)
	}

	// glob: f??.go matches foo.go but not foo_test.go
	result = p.searchName(map[string]any{"keyword": "f??.go", "dir": root})
	if !strings.Contains(result, "foo.go") {
		t.Fatalf("expected foo.go, got %q", result)
	}
	if strings.Contains(result, "foo_test.go") {
		t.Fatalf("expected foo_test.go NOT to match f??.go, got %q", result)
	}

	// glob: ?ar.txt matches bar.txt
	result = p.searchName(map[string]any{"keyword": "?ar.txt", "dir": root})
	if !strings.Contains(result, "bar.txt") {
		t.Fatalf("expected bar.txt, got %q", result)
	}
}

func TestSearchName_Regex(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "foo.go"), nil, 0644)
	os.WriteFile(filepath.Join(root, "bar.txt"), nil, 0644)
	os.WriteFile(filepath.Join(root, "foo_test.go"), nil, 0644)

	p := &Provider{rootDir: root}

	// regex: \.go$ matches both .go files
	result := p.searchName(map[string]any{"keyword": `\.go$`, "type": "regex", "dir": root})
	if !strings.Contains(result, "foo.go") {
		t.Fatalf("expected foo.go, got %q", result)
	}
	if !strings.Contains(result, "foo_test.go") {
		t.Fatalf("expected foo_test.go, got %q", result)
	}
	if strings.Contains(result, "bar.txt") {
		t.Fatalf("expected bar.txt NOT to match, got %q", result)
	}

	// regex: ^foo\.[^_]
	result = p.searchName(map[string]any{"keyword": `^foo\.[^_]`, "type": "regex", "dir": root})
	if !strings.Contains(result, "foo.go") {
		t.Fatalf("expected foo.go, got %q", result)
	}
	if strings.Contains(result, "foo_test.go") {
		t.Fatalf("expected foo_test.go NOT to match, got %q", result)
	}
}

func TestSearchName_InvalidRegex(t *testing.T) {
	root := t.TempDir()
	p := &Provider{rootDir: root}

	result := p.searchName(map[string]any{"keyword": `[unclosed`, "type": "regex", "dir": root})
	if !strings.Contains(result, "Error: invalid regex") {
		t.Fatalf("expected invalid regex error, got %q", result)
	}
}

func TestSearchContent_GlobDefault(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello world\nfoo bar\nbaz qux"), 0644)

	p := &Provider{rootDir: root}

	// glob: hello* matches "hello world"
	result := p.searchContent(map[string]any{"keyword": "hello*", "dir": root})
	if !strings.Contains(result, "hello world") {
		t.Fatalf("expected 'hello world', got %q", result)
	}

	// glob: *bar* matches "foo bar"
	result = p.searchContent(map[string]any{"keyword": "*bar*", "dir": root})
	if !strings.Contains(result, "foo bar") {
		t.Fatalf("expected 'foo bar', got %q", result)
	}

	// glob: baz ??? matches "baz qux"
	result = p.searchContent(map[string]any{"keyword": "baz ???", "dir": root})
	if !strings.Contains(result, "baz qux") {
		t.Fatalf("expected 'baz qux', got %q", result)
	}
}

func TestSearchContent_Regex(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello world\nfoo bar\nbaz qux"), 0644)

	p := &Provider{rootDir: root}

	// regex: \b\w{3}\b matches "foo", "bar", "baz", "qux"
	result := p.searchContent(map[string]any{"keyword": `\b\w{3}\b`, "type": "regex", "dir": root})
	if !strings.Contains(result, "foo bar") {
		t.Fatalf("expected 'foo bar', got %q", result)
	}
	if !strings.Contains(result, "baz qux") {
		t.Fatalf("expected 'baz qux', got %q", result)
	}

	// regex: ^hello (only line 1 matches, context shows surrounding lines)
	result = p.searchContent(map[string]any{"keyword": `^hello`, "type": "regex", "dir": root})
	if !strings.Contains(result, "hello world") {
		t.Fatalf("expected 'hello world', got %q", result)
	}
	// context may include surrounding lines; that's fine — just verify the hit is there
}

func TestSearchContent_InvalidRegex(t *testing.T) {
	root := t.TempDir()
	p := &Provider{rootDir: root}

	result := p.searchContent(map[string]any{"keyword": `[unclosed`, "type": "regex", "dir": root})
	if !strings.Contains(result, "Error: invalid regex") {
		t.Fatalf("expected invalid regex error, got %q", result)
	}
}
