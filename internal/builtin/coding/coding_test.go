package coding

import (
	"os"
	"path/filepath"
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
		extBlacklist:  []string{},
	}

	// create some test files
	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("Hello World\nLine 2\nLine 3"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "subdir", "data.txt"), []byte("alpha\nbeta\ngamma\ndelta"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "multi.txt"), []byte("foo\nbar\nfoo\nbaz\nfoo"), 0644)

	return p
}

func TestName(t *testing.T) {
	p := &Provider{}
	if p.Name() != "CodingMCP" {
		t.Errorf("expected 'CodingMCP', got '%s'", p.Name())
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

	builtinNames := []string{
		"tree", "search_name", "search_content", "read_content",
		"replace_content", "create_dir", "create_file", "rm", "move", "rewrite_file",
	}
	for _, name := range builtinNames {
		found := false
		for _, tool := range tools {
			if tool.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected builtin tool '%s' in Tools()", name)
		}
	}

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

	// 10 builtin + 1 shell = 11
	if len(tools) != 11 {
		t.Errorf("expected 11 tools, got %d", len(tools))
	}
}

func TestTools_NoShellTools(t *testing.T) {
	p := setupProvider(t)
	tools := p.Tools()
	if len(tools) != 10 {
		t.Errorf("expected 10 builtin tools, got %d", len(tools))
	}
}

// --- getSafePath tests ---

func TestGetSafePath_Normal(t *testing.T) {
	p := setupProvider(t)
	result, err := p.getSafePath("readme.txt")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.HasSuffix(result, "readme.txt") {
		t.Errorf("expected path ending with 'readme.txt', got '%s'", result)
	}
}

func TestGetSafePath_Traversal(t *testing.T) {
	p := setupProvider(t)
	_, err := p.getSafePath("../../../etc/passwd")
	if err == nil {
		t.Errorf("expected error for path traversal")
	}
}

func TestGetSafePath_AbsoluteStripped(t *testing.T) {
	p := setupProvider(t)
	result, err := p.getSafePath("/readme.txt")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.HasSuffix(result, "readme.txt") {
		t.Errorf("expected path ending with 'readme.txt', got '%s'", result)
	}
}

// --- isIgnoredName tests ---

func TestIsIgnoredName(t *testing.T) {
	if !isIgnoredName(".trash_can") {
		t.Errorf("expected .trash_can to be ignored")
	}
	if !isIgnoredName("_runtime") {
		t.Errorf("expected _runtime to be ignored")
	}
	if isIgnoredName("normal") {
		t.Errorf("expected 'normal' not to be ignored")
	}
}

// --- moveToTrash tests ---

func TestMoveToTrash_File(t *testing.T) {
	p := setupProvider(t)
	fpath := filepath.Join(p.rootDir, "readme.txt")

	err := p.moveToTrash(fpath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// file should no longer exist
	if _, err := os.Stat(fpath); !os.IsNotExist(err) {
		t.Errorf("expected file to be removed")
	}
	// should be in trash
	entries, _ := os.ReadDir(filepath.Join(p.rootDir, ".trash_can"))
	if len(entries) != 1 {
		t.Errorf("expected 1 file in trash, got %d", len(entries))
	}
}

func TestMoveToTrash_Directory(t *testing.T) {
	p := setupProvider(t)
	dpath := filepath.Join(p.rootDir, "emptydir")
	os.MkdirAll(dpath, 0755)

	err := p.moveToTrash(dpath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// directory should be removed (not trashed)
	if _, err := os.Stat(dpath); !os.IsNotExist(err) {
		t.Errorf("expected directory to be removed")
	}
}

func TestMoveToTrash_NotExist(t *testing.T) {
	p := setupProvider(t)
	err := p.moveToTrash(filepath.Join(p.rootDir, "nonexistent.txt"))
	if err == nil {
		t.Errorf("expected error for nonexistent file")
	}
}

// --- CallTool tests ---

func checkSuccess(t *testing.T, result *model.ToolResult, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
}

func checkContains(t *testing.T, text, sub string) {
	t.Helper()
	if !strings.Contains(text, sub) {
		t.Errorf("expected text to contain '%s', got:\n%s", sub, text)
	}
}

// --- tree ---

func TestTree_Default(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("tree", map[string]any{})
	checkSuccess(t, result, err)
	text := result.Content[0].Text
	checkContains(t, text, "readme.txt")
	checkContains(t, text, "subdir")
}

func TestTree_EmptyDir(t *testing.T) {
	p := setupProvider(t)
	emptyDir := filepath.Join(p.rootDir, "empty")
	os.MkdirAll(emptyDir, 0755)

	// change root to empty dir
	p.rootDir = emptyDir
	result, err := p.CallTool("tree", map[string]any{"dir": "/"})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "Empty directory")
}

func TestTree_NonExistent(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("tree", map[string]any{"dir": "nonexistent"})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "non-exist")
}

func TestTree_DepthLimit(t *testing.T) {
	p := setupProvider(t)
	os.MkdirAll(filepath.Join(p.rootDir, "a", "b", "c"), 0755)
	os.WriteFile(filepath.Join(p.rootDir, "a", "b", "c", "deep.txt"), []byte("deep"), 0644)

	result, err := p.CallTool("tree", map[string]any{"depth": float64(1)})
	checkSuccess(t, result, err)
	// depth 2 means 0 children depth
	text := result.Content[0].Text
	// should show a but not a\b
	if !strings.Contains(text, "a") {
		t.Errorf("expected 'a' in tree output:\n%s", text)
	}
}

// --- search_name ---

func TestSearchName_Found(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("search_name", map[string]any{"keyword": "readme"})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "readme.txt")
}

func TestSearchName_NotFound(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("search_name", map[string]any{"keyword": "nonexistent_xxx"})
	checkSuccess(t, result, err)
	text := result.Content[0].Text
	if strings.TrimSpace(text) != "" {
		t.Errorf("expected empty result, got '%s'", text)
	}
}

func TestSearchName_Limit(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("search_name", map[string]any{"keyword": "t", "limit_file": float64(1)})
	checkSuccess(t, result, err)
}

// --- search_content ---

func TestSearchContent_Found(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("search_content", map[string]any{"keyword": "Hello"})
	checkSuccess(t, result, err)
	text := result.Content[0].Text
	checkContains(t, text, "readme.txt")
	checkContains(t, text, "Hello")
}

func TestSearchContent_NotFound(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("search_content", map[string]any{"keyword": "nonexistent_xyzzy"})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "No matches found")
}

func TestSearchContent_MultipleMatches(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("search_content", map[string]any{"keyword": "foo", "limit_file": float64(10)})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "multi.txt")
}

// --- read_content ---

func TestReadContent_Normal(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("read_content", map[string]any{"file": "readme.txt"})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "Hello World")
}

func TestReadContent_StartAndLength(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("read_content", map[string]any{
		"file": "readme.txt", "start": float64(1), "length": float64(1),
	})
	checkSuccess(t, result, err)
	text := result.Content[0].Text
	if text != "Line 2" {
		t.Errorf("expected 'Line 2', got '%s'", text)
	}
}

func TestReadContent_InvalidStart(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("read_content", map[string]any{"file": "readme.txt", "start": float64(-1)})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "Error")
}

func TestReadContent_InvalidLength(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("read_content", map[string]any{"file": "readme.txt", "length": float64(0)})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "Error")
}

func TestReadContent_FileNotExist(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("read_content", map[string]any{"file": "missing.txt"})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "Error")
}

func TestReadContent_StartBeyondFile(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("read_content", map[string]any{"file": "readme.txt", "start": float64(100)})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "Error")
}

func TestReadContent_ExtBlacklist(t *testing.T) {
	p := setupProvider(t)
	p.extBlacklist = []string{"txt"}
	result, err := p.CallTool("read_content", map[string]any{"file": "readme.txt"})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "binary file")
}

// --- replace_content ---

func TestReplaceContent_Single(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("replace_content", map[string]any{
		"file": "readme.txt", "original": "Hello", "new": "Hi",
	})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "Success")

	data, _ := os.ReadFile(filepath.Join(p.rootDir, "readme.txt"))
	if !strings.Contains(string(data), "Hi World") {
		t.Errorf("expected 'Hi World' in file, got '%s'", string(data))
	}
}

func TestReplaceContent_NotFound(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("replace_content", map[string]any{
		"file": "readme.txt", "original": "nonexistent", "new": "x",
	})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "original content not found")
}

func TestReplaceContent_EmptyOriginal(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("replace_content", map[string]any{
		"file": "readme.txt", "original": "", "new": "x",
	})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "cannot be empty")
}

func TestReplaceContent_MultipleWithoutBatch(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("replace_content", map[string]any{
		"file": "multi.txt", "original": "foo", "new": "bar",
	})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "multiple hits")
}

func TestReplaceContent_Batch(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("replace_content", map[string]any{
		"file": "multi.txt", "original": "foo", "new": "bar", "allow_batch": true,
	})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "Success")

	data, _ := os.ReadFile(filepath.Join(p.rootDir, "multi.txt"))
	if strings.Contains(string(data), "foo") {
		t.Errorf("expected no 'foo' after batch replace:\n%s", string(data))
	}
}

// --- create_dir ---

func TestCreateDir(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("create_dir", map[string]any{"dir": "newdir"})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "Success")

	stat, err := os.Stat(filepath.Join(p.rootDir, "newdir"))
	if err != nil || !stat.IsDir() {
		t.Errorf("expected directory to be created")
	}
}

func TestCreateDir_Nested(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("create_dir", map[string]any{"dir": "a/b/c"})
	checkSuccess(t, result, err)

	stat, err := os.Stat(filepath.Join(p.rootDir, "a", "b", "c"))
	if err != nil || !stat.IsDir() {
		t.Errorf("expected nested directory to be created")
	}
}

// --- create_file ---

func TestCreateFile(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("create_file", map[string]any{
		"file": "newfile.txt", "content": "hello world",
	})
	checkSuccess(t, result, err)

	data, _ := os.ReadFile(filepath.Join(p.rootDir, "newfile.txt"))
	if string(data) != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", string(data))
	}
}

func TestCreateFile_EmptyContent(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("create_file", map[string]any{"file": "empty.txt"})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "Success")

	data, _ := os.ReadFile(filepath.Join(p.rootDir, "empty.txt"))
	if len(data) != 0 {
		t.Errorf("expected empty file, got '%s'", string(data))
	}
}

// --- rm ---

func TestRm_File(t *testing.T) {
	p := setupProvider(t)
	fpath := filepath.Join(p.rootDir, "readme.txt")
	result, err := p.CallTool("rm", map[string]any{"file": "readme.txt"})
	checkSuccess(t, result, err)

	if _, err := os.Stat(fpath); !os.IsNotExist(err) {
		t.Errorf("expected file to be removed")
	}
}

func TestRm_NotExist(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("rm", map[string]any{"file": "nonexistent.txt"})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "Error")
}

// --- move ---

func TestMove_Rename(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("move", map[string]any{
		"src": "readme.txt", "dst": "renamed.txt",
	})
	checkSuccess(t, result, err)

	// src should be gone
	if _, err := os.Stat(filepath.Join(p.rootDir, "readme.txt")); !os.IsNotExist(err) {
		t.Errorf("expected src to be removed")
	}
	// dst should exist
	if _, err := os.Stat(filepath.Join(p.rootDir, "renamed.txt")); err != nil {
		t.Errorf("expected dst to exist")
	}
}

func TestMove_Copy(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("move", map[string]any{
		"src": "readme.txt", "dst": "copy.txt", "keep_original": true,
	})
	checkSuccess(t, result, err)

	// src should still exist
	if _, err := os.Stat(filepath.Join(p.rootDir, "readme.txt")); err != nil {
		t.Errorf("expected src to still exist")
	}
	// dst should exist
	if _, err := os.Stat(filepath.Join(p.rootDir, "copy.txt")); err != nil {
		t.Errorf("expected dst to exist")
	}
}

func TestMove_SecurityError(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("move", map[string]any{
		"src": "../../etc/passwd", "dst": "passwd",
	})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "security error")
}

// --- rewrite_file ---

func TestRewriteFile(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("rewrite_file", map[string]any{
		"file": "readme.txt", "content": "totally new content",
	})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "Success")

	data, _ := os.ReadFile(filepath.Join(p.rootDir, "readme.txt"))
	if string(data) != "totally new content" {
		t.Errorf("expected 'totally new content', got '%s'", string(data))
	}
	// old file should be trashed
	entries, _ := os.ReadDir(filepath.Join(p.rootDir, ".trash_can"))
	if len(entries) != 1 {
		t.Errorf("expected 1 file in trash, got %d", len(entries))
	}
}

func TestRewriteFile_NonExistent(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("rewrite_file", map[string]any{
		"file": "new_file.txt", "content": "new file content",
	})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "Success")

	data, _ := os.ReadFile(filepath.Join(p.rootDir, "new_file.txt"))
	if string(data) != "new file content" {
		t.Errorf("expected 'new file content', got '%s'", string(data))
	}
}

// --- unknown tool ---

func TestCallTool_Unknown(t *testing.T) {
	p := setupProvider(t)
	result, err := p.CallTool("unknown_tool_name", map[string]any{})
	checkSuccess(t, result, err)
	checkContains(t, result.Content[0].Text, "Unknown tool")
}

// --- shell tool via CallTool ---

func TestCallTool_ShellTool(t *testing.T) {
	p := setupProvider(t)
	p.shellTools = map[string]model.ShellTool{
		"echo_test": {Description: "echo", Command: "echo hello", Timeout: 10},
	}

	result, err := p.CallTool("echo_test", map[string]any{})
	checkSuccess(t, result, err)
	// On Windows, cmd /c echo hello should work
	text := result.Content[0].Text
	checkContains(t, text, "hello")
}
