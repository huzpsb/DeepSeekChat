package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hschat/internal/model"
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

func TestSearchName_PlainDefault(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "foo.go"), nil, 0644)
	os.WriteFile(filepath.Join(root, "bar.txt"), nil, 0644)
	os.WriteFile(filepath.Join(root, "foo_test.go"), nil, 0644)

	p := &Provider{rootDir: root}

	// plain: substring "foo" matches both foo.go and foo_test.go
	result := p.searchName(map[string]any{"query": "foo", "dir": root})
	if !strings.Contains(result, "foo.go") {
		t.Fatalf("expected foo.go, got %q", result)
	}
	if !strings.Contains(result, "foo_test.go") {
		t.Fatalf("expected foo_test.go, got %q", result)
	}
	if strings.Contains(result, "bar.txt") {
		t.Fatalf("expected bar.txt NOT to match, got %q", result)
	}
}

func TestSearchName_Glob(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "foo.go"), nil, 0644)
	os.WriteFile(filepath.Join(root, "bar.txt"), nil, 0644)
	os.WriteFile(filepath.Join(root, "foo_test.go"), nil, 0644)

	p := &Provider{rootDir: root}

	// glob: *.go matches both .go files
	result := p.searchName(map[string]any{"query": "*.go", "type": "glob", "dir": root})
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
	result = p.searchName(map[string]any{"query": "f??.go", "type": "glob", "dir": root})
	if !strings.Contains(result, "foo.go") {
		t.Fatalf("expected foo.go, got %q", result)
	}
	if strings.Contains(result, "foo_test.go") {
		t.Fatalf("expected foo_test.go NOT to match f??.go, got %q", result)
	}

	// glob: ?ar.txt matches bar.txt
	result = p.searchName(map[string]any{"query": "?ar.txt", "type": "glob", "dir": root})
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
	result := p.searchName(map[string]any{"query": `\.go$`, "type": "regex", "dir": root})
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
	result = p.searchName(map[string]any{"query": `^foo\.[^_]`, "type": "regex", "dir": root})
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

	result := p.searchName(map[string]any{"query": `[unclosed`, "type": "regex", "dir": root})
	if !strings.Contains(result, "Error: invalid regex") {
		t.Fatalf("expected invalid regex error, got %q", result)
	}
}

func TestSearchContentPlaintext_UsesKeyword(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello world\nfoo bar\nbaz qux"), 0644)

	p := &Provider{rootDir: root}

	// plaintext uses keyword and plain substring matching.
	result := p.searchContentPlaintext(map[string]any{"keyword": "hello", "dir": root})
	if !strings.Contains(result, "hello world") {
		t.Fatalf("expected 'hello world', got %q", result)
	}

	result = p.searchContentPlaintext(map[string]any{"keyword": "bar", "dir": root})
	if !strings.Contains(result, "foo bar") {
		t.Fatalf("expected 'foo bar', got %q", result)
	}
}

func TestSearchContentPlaintext_NoRegexHint(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello world"), 0644)

	p := &Provider{rootDir: root}

	// special characters are plain substrings now; no regex/glob hint.
	result := p.searchContentPlaintext(map[string]any{"keyword": "hello.*", "dir": root})
	if !strings.Contains(result, "No matches found.") {
		t.Fatalf("expected 'No matches found.', got %q", result)
	}
	if strings.Contains(result, "Hint:") {
		t.Fatalf("plaintext search should not emit a regex/glob hint, got %q", result)
	}
}

func TestSearchContentAdvanced_Glob(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello world\nfoo bar\nbaz qux"), 0644)

	p := &Provider{rootDir: root}

	// glob: hello* matches whole line "hello world"
	result := p.searchContentAdvanced(map[string]any{"query": "hello*", "type": "glob", "dir": root})
	if !strings.Contains(result, "hello world") {
		t.Fatalf("expected 'hello world', got %q", result)
	}

	// glob: *bar* matches whole line "foo bar"
	result = p.searchContentAdvanced(map[string]any{"query": "*bar*", "type": "glob", "dir": root})
	if !strings.Contains(result, "foo bar") {
		t.Fatalf("expected 'foo bar', got %q", result)
	}

	// glob: baz ??? matches whole line "baz qux"
	result = p.searchContentAdvanced(map[string]any{"query": "baz ???", "type": "glob", "dir": root})
	if !strings.Contains(result, "baz qux") {
		t.Fatalf("expected 'baz qux', got %q", result)
	}
}

func TestSearchContentAdvanced_GlobDefault(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello world"), 0644)

	p := &Provider{rootDir: root}

	// type omitted should default to glob, never plain.
	result := p.searchContentAdvanced(map[string]any{"query": "hello*", "dir": root})
	if !strings.Contains(result, "hello world") {
		t.Fatalf("expected glob default to match 'hello world', got %q", result)
	}
}

func TestSearchContentAdvanced_Regex(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello world\nfoo bar\nbaz qux"), 0644)

	p := &Provider{rootDir: root}

	// regex: \b\w{3}\b matches "foo", "bar", "baz", "qux"
	result := p.searchContentAdvanced(map[string]any{"query": `\b\w{3}\b`, "type": "regex", "dir": root})
	if !strings.Contains(result, "foo bar") {
		t.Fatalf("expected 'foo bar', got %q", result)
	}
	if !strings.Contains(result, "baz qux") {
		t.Fatalf("expected 'baz qux', got %q", result)
	}

	// regex: ^hello (only line 1 matches, context shows surrounding lines)
	result = p.searchContentAdvanced(map[string]any{"query": `^hello`, "type": "regex", "dir": root})
	if !strings.Contains(result, "hello world") {
		t.Fatalf("expected 'hello world', got %q", result)
	}
	// context may include surrounding lines; that's fine -- just verify the hit is there
}

func TestSearchContentAdvanced_InvalidRegex(t *testing.T) {
	root := t.TempDir()
	p := &Provider{rootDir: root}

	result := p.searchContentAdvanced(map[string]any{"query": `[unclosed`, "type": "regex", "dir": root})
	if !strings.Contains(result, "Error: invalid regex") {
		t.Fatalf("expected invalid regex error, got %q", result)
	}
}

func TestSearchContentAdvanced_RejectsPlain(t *testing.T) {
	root := t.TempDir()
	p := &Provider{rootDir: root}

	result := p.searchContentAdvanced(map[string]any{"query": "hello", "type": "plain", "dir": root})
	if !strings.Contains(result, "Error: type must be") {
		t.Fatalf("expected plain type to be rejected, got %q", result)
	}
}

func TestTools_SearchContentSplitAndTreeDefaultLimit(t *testing.T) {
	p := &Provider{}
	tools := p.Tools()

	byName := map[string]model.ToolDef{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}

	if _, ok := byName["search_content"]; ok {
		t.Fatalf("old search_content tool should no longer exist")
	}

	plain, ok := byName["search_content_plaintext"]
	if !ok {
		t.Fatalf("expected search_content_plaintext tool")
	}
	plainSchema, ok := plain.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("expected plaintext InputSchema to be a map, got %#v", plain.InputSchema)
	}
	props := plainSchema["properties"].(map[string]any)
	if _, hasType := props["type"]; hasType {
		t.Fatalf("search_content_plaintext should not expose a type parameter")
	}
	if _, hasKeyword := props["keyword"]; !hasKeyword {
		t.Fatalf("search_content_plaintext should expose keyword")
	}

	advanced, ok := byName["search_content_advanced"]
	if !ok {
		t.Fatalf("expected search_content_advanced tool")
	}
	advancedSchema, ok := advanced.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("expected advanced InputSchema to be a map, got %#v", advanced.InputSchema)
	}
	advProps := advancedSchema["properties"].(map[string]any)
	advType := advProps["type"].(map[string]any)
	enum, _ := advType["enum"].([]string)
	if len(enum) != 2 || enum[0] != "glob" || enum[1] != "regex" {
		t.Fatalf("advanced type enum should be [glob regex], got %#v", enum)
	}

	tree, ok := byName["tree"]
	if !ok {
		t.Fatalf("expected tree tool")
	}
	treeSchema, ok := tree.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("expected tree InputSchema to be a map, got %#v", tree.InputSchema)
	}
	treeProps := treeSchema["properties"].(map[string]any)
	limit := treeProps["limit"].(map[string]any)
	if limit["default"] != 1000 {
		t.Fatalf("tree default limit should be 1000, got %#v", limit["default"])
	}
}
