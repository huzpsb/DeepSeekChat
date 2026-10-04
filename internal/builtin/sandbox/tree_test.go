package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeTree builds root/d000..d0NN each containing one file, so a listing at
// depth=2 has 2*NN entries and depth=1 has NN entries.
func makeTree(t *testing.T, root string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		dir := filepath.Join(root, fmt.Sprintf("d%03d", i))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func linesOf(result string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(result), "\n") {
		if l != "" && !strings.HasPrefix(l, "WARNING") {
			out = append(out, l)
		}
	}
	return out
}

// treeArgs mirrors the JSON-unmarshaled shape the provider actually
// receives: all numbers are float64, so the arg assertions in tree() hit.
func treeArgs(depth, limit int) map[string]any {
	return map[string]any{"dir": ".", "depth": float64(depth), "limit": float64(limit)}
}

// Fits at the requested depth: no warning, all entries present.
func TestTree_NoDegradation_NoWarning(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, 3)
	p := &Provider{rootDir: root}

	result := p.tree(context.Background(), treeArgs(2, 100))
	if strings.Contains(result, "WARNING") {
		t.Fatalf("unexpected warning: %q", result)
	}
	if got := len(linesOf(result)); got != 6 { // 3 dirs + 3 files
		t.Fatalf("expected 6 entries, got %d: %q", got, result)
	}
}

// Exceeds at depth=2, fits at depth=1: listing degrades to depth=1 and the
// output must carry a depth-reduction warning naming both depths.
func TestTree_DepthReduced_Warns(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, 5) // depth=2 -> 10 entries, depth=1 -> 5
	p := &Provider{rootDir: root}

	result := p.tree(context.Background(), treeArgs(2, 6))
	if !strings.Contains(result, "reduced to depth=1") {
		t.Fatalf("expected depth-reduction warning, got %q", result)
	}
	lines := linesOf(result)
	if len(lines) != 5 {
		t.Fatalf("expected 5 entries (depth=1), got %d: %q", len(lines), result)
	}
	for _, l := range lines {
		if strings.Contains(l, string(os.PathSeparator)) || strings.Contains(l, "/") {
			t.Fatalf("depth-1 listing must not contain nested paths: %q", result)
		}
	}
}

// Exceeds even at depth=1: first limit entries returned with a truncation
// warning (never a bare error).
func TestTree_TruncatedAtDepth1_Warns(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, 8) // depth=1 -> 8 entries
	p := &Provider{rootDir: root}

	result := p.tree(context.Background(), treeArgs(2, 3))
	if !strings.Contains(result, "more than 3 entries even at depth=1") {
		t.Fatalf("expected truncation warning, got %q", result)
	}
	if !strings.Contains(result, "first 3 are shown") {
		t.Fatalf("expected 'first N' warning, got %q", result)
	}
	lines := linesOf(result)
	if len(lines) != 3 {
		t.Fatalf("expected exactly 3 entries, got %d: %q", len(lines), result)
	}
	// filepath.Walk is lexical: the first 3 are d000, d001, d002.
	for i, want := range []string{"d000", "d001", "d002"} {
		if lines[i] != want {
			t.Fatalf("line %d: want %q, got %q (full: %q)", i, want, lines[i], result)
		}
	}
}

// Requested depth already 1 and still over limit: truncation warning, no
// depth-reduction mention.
func TestTree_TruncatedAtDepth1_NoDepthReductionMention(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, 5)
	p := &Provider{rootDir: root}

	result := p.tree(context.Background(), treeArgs(1, 2))
	if !strings.Contains(result, "even at depth=1") {
		t.Fatalf("expected truncation warning, got %q", result)
	}
	if strings.Contains(result, "reduced to depth=") {
		t.Fatalf("no depth reduction happened (depth=1 requested), got %q", result)
	}
	if got := len(linesOf(result)); got != 2 {
		t.Fatalf("expected 2 entries, got %d: %q", got, result)
	}
}

// Exact fit at the boundary (count == limit) is NOT an exceed: no warning.
func TestTree_ExactLimit_NoWarning(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, 3)
	p := &Provider{rootDir: root}

	result := p.tree(context.Background(), treeArgs(2, 6))
	if strings.Contains(result, "WARNING") {
		t.Fatalf("exact fit must not warn: %q", result)
	}
	if got := len(linesOf(result)); got != 6 {
		t.Fatalf("expected 6 entries, got %d", got)
	}
}

// Degradation must not be triggered by entries hidden by depth pruning or
// ignore rules: 10 dirs at depth 2 with a huge ignored dir (e.g. .git)
// inside must still fit at depth=2.
func TestTree_IgnoredDirsDoNotCount(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, 2)
	ignored := filepath.Join(root, ".git")
	os.MkdirAll(filepath.Join(ignored, "a", "b", "c"), 0755)
	p := &Provider{rootDir: root}

	result := p.tree(context.Background(), treeArgs(2, 4))
	if strings.Contains(result, "WARNING") {
		t.Fatalf("ignored entries must not trigger degradation: %q", result)
	}
	if strings.Contains(result, ".git") {
		t.Fatalf("ignored dir must not be listed: %q", result)
	}
	if got := len(linesOf(result)); got != 4 {
		t.Fatalf("expected 4 entries, got %d: %q", got, result)
	}
}
