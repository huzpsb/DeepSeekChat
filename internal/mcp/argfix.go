package mcp

import "hschat/internal/model"

// ArgFix records one in-place argument rename applied by AutoFixArgs.
type ArgFix struct {
	Field string // canonical field name that was populated
	From  string // alias key the value was taken from
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
