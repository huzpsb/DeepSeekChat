package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"hschat/internal/model"
)

// ArgFix records one in-place argument fix applied at the protocol layer.
type ArgFix struct {
	Field string // argument name that was fixed
	From  string // what was fixed: an alias name ("old"), or a type
	//             coercion ("string->integer")
}

// FixArgs applies all protocol-layer argument fixes in place: alias
// renaming (AutoFixArgs) followed by schema-guided type coercion
// (CoerceArgs). Renaming runs first so the coercer sees canonical names.
func FixArgs(tool *model.ToolDef, args map[string]any) []ArgFix {
	fixes := AutoFixArgs(tool, args)
	return append(fixes, CoerceArgs(tool, args)...)
}

// AutoFixArgs renames aliased argument keys in place according to the
// tool's declared ArgAliases (canonical field -> alias names).
//
// Rules:
//   - the canonical key always wins: if it is present, nothing is touched;
//   - among multiple present aliases, the first declared alias wins;
//   - a consumed alias key is deleted; untouched aliases are left as-is.
//
// Returns the applied fixes (for logging). Nil tool / empty args / empty
// alias table are all no-ops.
func AutoFixArgs(tool *model.ToolDef, args map[string]any) []ArgFix {
	if tool == nil || len(tool.ArgAliases) == 0 || len(args) == 0 {
		return nil
	}
	var fixes []ArgFix
	for canonical, aliases := range tool.ArgAliases {
		if _, ok := args[canonical]; ok {
			continue
		}
		for _, alias := range aliases {
			if v, ok := args[alias]; ok {
				args[canonical] = v
				delete(args, alias)
				fixes = append(fixes, ArgFix{Field: canonical, From: alias})
				break
			}
		}
	}
	return fixes
}

// CoerceArgs converts argument values in place to the type declared by the
// tool's InputSchema (properties.<name>.type) when the conversion is
// unambiguous:
//
//	integer/number <- string that parses as a float ("30" -> 30)
//	boolean        <- string "true"/"false" (case-insensitive)
//	string         <- float64 or bool
//	array/object   <- string containing valid JSON of that shape
//
// Values already matching the declared type, unknown argument names, and
// failed conversions are left untouched. LLMs routinely serialize numbers
// as strings inside tool arguments; the builtin tools assert concrete Go
// types (e.g. float64) and would silently fall back to defaults.
func CoerceArgs(tool *model.ToolDef, args map[string]any) []ArgFix {
	types := schemaPropTypes(tool)
	if len(types) == 0 || len(args) == 0 {
		return nil
	}
	var fixes []ArgFix
	for name, want := range types {
		v, ok := args[name]
		if !ok {
			continue
		}
		if nv, from, ok := coerceValue(v, want); ok {
			args[name] = nv
			fixes = append(fixes, ArgFix{Field: name, From: from})
		}
	}
	return fixes
}

// schemaPropTypes extracts {property name -> declared type} from the tool's
// InputSchema, defensively (InputSchema is `any` and may come from a remote
// MCP server).
func schemaPropTypes(tool *model.ToolDef) map[string]string {
	if tool == nil {
		return nil
	}
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		return nil
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil
	}
	types := make(map[string]string, len(props))
	for name, p := range props {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if t, ok := pm["type"].(string); ok && t != "" {
			types[name] = t
		}
	}
	return types
}

// coerceValue converts v to the schema-declared type want. ok=false means
// no (or no safe) conversion exists and the value must be left alone.
func coerceValue(v any, want string) (nv any, from string, ok bool) {
	switch want {
	case "integer", "number":
		switch t := v.(type) {
		case float64, int, int64:
			return v, "", false // already numeric
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
				return f, "string->" + want, true
			}
		}
	case "boolean":
		if s, isStr := v.(string); isStr {
			switch strings.ToLower(strings.TrimSpace(s)) {
			case "true":
				return true, "string->boolean", true
			case "false":
				return false, "string->boolean", true
			}
		}
	case "string":
		switch t := v.(type) {
		case float64:
			return strconv.FormatFloat(t, 'f', -1, 64), "number->string", true
		case bool:
			return strconv.FormatBool(t), "boolean->string", true
		}
	case "array":
		if s, isStr := v.(string); isStr {
			var arr []any
			if err := json.Unmarshal([]byte(s), &arr); err == nil && arr != nil {
				return arr, "string->array", true
			}
		}
	case "object":
		if s, isStr := v.(string); isStr {
			var obj map[string]any
			if err := json.Unmarshal([]byte(s), &obj); err == nil && obj != nil {
				return obj, "string->object", true
			}
		}
	}
	return nil, "", false
}

func (f ArgFix) String() string {
	return fmt.Sprintf("%s<-%s", f.Field, f.From)
}
