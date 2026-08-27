package argfix

import (
	"testing"

	"hschat/internal/model"
)

func TestRenameAliases_RenamesAlias(t *testing.T) {
	tool := &model.ToolDef{
		Name:       "replace_content",
		ArgAliases: map[string][]string{"original": {"old"}},
	}
	args := map[string]any{"file": "a.txt", "old": "x", "new": "y"}

	fixes := RenameAliases(tool, args)

	if len(fixes) != 1 || fixes[0].Field != "original" || fixes[0].From != "old" {
		t.Fatalf("unexpected fixes: %v", fixes)
	}
	if args["original"] != "x" {
		t.Errorf("expected original=%q, got %v", "x", args["original"])
	}
	if _, ok := args["old"]; ok {
		t.Error("alias key 'old' should have been deleted")
	}
}

func TestRenameAliases_CanonicalWins(t *testing.T) {
	tool := &model.ToolDef{
		Name:       "replace_content",
		ArgAliases: map[string][]string{"original": {"old"}},
	}
	args := map[string]any{"original": "canon", "old": "alias"}

	fixes := RenameAliases(tool, args)

	if len(fixes) != 0 {
		t.Errorf("expected no fixes, got %v", fixes)
	}
	if args["original"] != "canon" {
		t.Errorf("canonical value was modified: %v", args["original"])
	}
	if _, ok := args["old"]; !ok {
		t.Error("untouched alias should be left as-is")
	}
}

func TestRenameAliases_FirstDeclaredAliasWins(t *testing.T) {
	tool := &model.ToolDef{
		Name:       "read_content",
		ArgAliases: map[string][]string{"length": {"limit", "max_lines"}},
	}
	args := map[string]any{"max_lines": 10.0, "limit": 5.0}

	fixes := RenameAliases(tool, args)

	if len(fixes) != 1 || fixes[0].From != "limit" {
		t.Fatalf("unexpected fixes: %v", fixes)
	}
	if args["length"] != 5.0 {
		t.Errorf("expected length=5, got %v", args["length"])
	}
	if _, ok := args["max_lines"]; !ok {
		t.Error("unconsumed alias should be left as-is")
	}
}

func TestRenameAliases_NoAliasPresent(t *testing.T) {
	tool := &model.ToolDef{
		Name:       "read_content",
		ArgAliases: map[string][]string{"length": {"limit"}},
	}
	args := map[string]any{"file": "a.txt"}

	if fixes := RenameAliases(tool, args); len(fixes) != 0 {
		t.Errorf("expected no fixes, got %v", fixes)
	}
	if _, ok := args["length"]; ok {
		t.Error("nothing should have been added")
	}
}

func TestRenameAliases_NoopCases(t *testing.T) {
	tool := &model.ToolDef{
		Name:       "read_content",
		ArgAliases: map[string][]string{"length": {"limit"}},
	}
	if fixes := RenameAliases(nil, map[string]any{"limit": 1.0}); fixes != nil {
		t.Errorf("nil tool: expected nil fixes, got %v", fixes)
	}
	if fixes := RenameAliases(tool, nil); fixes != nil {
		t.Errorf("nil args: expected nil fixes, got %v", fixes)
	}
	plain := &model.ToolDef{Name: "tree"}
	if fixes := RenameAliases(plain, map[string]any{"limit": 1.0}); fixes != nil {
		t.Errorf("no alias table: expected nil fixes, got %v", fixes)
	}
}

func schemaTool() *model.ToolDef {
	return &model.ToolDef{
		Name: "read_content",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file":   map[string]any{"type": "string"},
				"start":  map[string]any{"type": "integer", "default": 0},
				"length": map[string]any{"type": "integer", "default": 2000},
				"flag":   map[string]any{"type": "boolean"},
				"tags":   map[string]any{"type": "array"},
				"opts":   map[string]any{"type": "object"},
			},
			"required": []string{"file"},
		},
	}
}

func TestCoerce_StringToInteger(t *testing.T) {
	args := map[string]any{"file": "a.txt", "length": "30"}
	fixes := Coerce(schemaTool(), args)

	if len(fixes) != 1 || fixes[0].Field != "length" || fixes[0].From != "string->integer" {
		t.Fatalf("unexpected fixes: %v", fixes)
	}
	if args["length"] != 30.0 {
		t.Errorf("expected length=30.0 (float64), got %#v", args["length"])
	}
}

func TestCoerce_ScalarConversions(t *testing.T) {
	args := map[string]any{
		"file": 42.0,      // number -> string
		"flag": "True",    // string -> boolean
		"tags": `["a"]`,   // string -> array
		"opts": `{"k":1}`, // string -> object
	}
	fixes := Coerce(schemaTool(), args)

	if len(fixes) != 4 {
		t.Fatalf("expected 4 fixes, got %v", fixes)
	}
	if args["file"] != "42" {
		t.Errorf("file: expected \"42\", got %#v", args["file"])
	}
	if args["flag"] != true {
		t.Errorf("flag: expected true, got %#v", args["flag"])
	}
	if arr, ok := args["tags"].([]any); !ok || len(arr) != 1 || arr[0] != "a" {
		t.Errorf("tags: expected [a], got %#v", args["tags"])
	}
	if obj, ok := args["opts"].(map[string]any); !ok || obj["k"] != 1.0 {
		t.Errorf("opts: expected map[k:1], got %#v", args["opts"])
	}
}

func TestCoerce_LeavesValidAndUnconvertibleAlone(t *testing.T) {
	args := map[string]any{
		"file":    "a.txt", // already string
		"length":  50.0,    // already numeric
		"start":   "abc",   // not a number -> untouched
		"flag":    "yes",   // not true/false -> untouched
		"unknown": "30",    // not in schema -> untouched
	}
	fixes := Coerce(schemaTool(), args)

	if len(fixes) != 0 {
		t.Errorf("expected no fixes, got %v", fixes)
	}
	if args["start"] != "abc" || args["flag"] != "yes" || args["unknown"] != "30" {
		t.Errorf("unconvertible/unknown args were modified: %v", args)
	}
}

func TestCoerce_NoopCases(t *testing.T) {
	if fixes := Coerce(nil, map[string]any{"x": "1"}); fixes != nil {
		t.Errorf("nil tool: expected nil fixes, got %v", fixes)
	}
	noSchema := &model.ToolDef{Name: "t"}
	if fixes := Coerce(noSchema, map[string]any{"x": "1"}); fixes != nil {
		t.Errorf("no schema: expected nil fixes, got %v", fixes)
	}
	if fixes := Coerce(schemaTool(), nil); fixes != nil {
		t.Errorf("nil args: expected nil fixes, got %v", fixes)
	}
}

// FixArgs must rename first, then coerce: {"limit": "30"} with
// length<-limit alias and length:integer schema ends up as length=30.0.
func TestFixArgs_RenameThenCoerce(t *testing.T) {
	tool := schemaTool()
	tool.ArgAliases = map[string][]string{"length": {"limit"}}
	args := map[string]any{"file": "a.txt", "limit": "30"}

	fixes := FixArgs(tool, args)

	if len(fixes) != 2 {
		t.Fatalf("expected 2 fixes, got %v", fixes)
	}
	if args["length"] != 30.0 {
		t.Errorf("expected length=30.0 (float64), got %#v", args["length"])
	}
	if _, ok := args["limit"]; ok {
		t.Error("alias key 'limit' should have been deleted")
	}
}
