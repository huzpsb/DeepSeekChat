package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"hschat/internal/encoding"
	"hschat/internal/model"
)

func (p *Provider) Tools() []model.ToolDef {
	tools := []model.ToolDef{
		{Name: "tree", Description: "Recursively list directory tree",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"depth": map[string]any{"type": "integer", "default": 2},
					"dir":   map[string]any{"type": "string", "default": "/"},
					"limit": map[string]any{"type": "integer", "default": 1000},
				},
			},
		},
		{Name: "search_name", Description: "Search files or folders by name (name only, path excluded). Default uses plain substring match. Set type=\"glob\" for glob (matches WHOLE name), e.g. \"*.go\". Set type=\"regex\" for regex (substring by default, use ^/$ to anchor), e.g. \"_test\\.go$\".",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":      map[string]any{"type": "string"},
					"type":       map[string]any{"type": "string", "default": "plain", "enum": []string{"plain", "glob", "regex"}},
					"limit_file": map[string]any{"type": "integer", "default": 20},
					"dir":        map[string]any{"type": "string", "default": "/"},
				},
				"required": []string{"query"},
			},
		},
		{Name: "search_content_plaintext", Description: "Search files with keyword. Uses plain substring match. Optionally filter by filename with file_glob (glob).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword":         map[string]any{"type": "string"},
					"file_glob":       map[string]any{"type": "string", "default": "*"},
					"limit_file":      map[string]any{"type": "integer", "default": 20},
					"limit_occurence": map[string]any{"type": "integer", "default": 5},
					"dir":             map[string]any{"type": "string", "default": "/"},
				},
				"required": []string{"keyword"},
			},
		},
		{Name: "search_content_advanced", Description: "Search files with query. Set type=\"glob\" for glob (matches WHOLE line; *2* matches \"123\" but *2 does NOT), e.g. \"*depth :=*\". Set type=\"regex\" for regex (substring by default, use ^/$ to anchor), e.g. \"depth\\s*:=\". Optionally filter by filename with file_glob (glob).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":           map[string]any{"type": "string"},
					"type":            map[string]any{"type": "string", "default": "glob", "enum": []string{"glob", "regex"}},
					"file_glob":       map[string]any{"type": "string", "default": "*"},
					"limit_file":      map[string]any{"type": "integer", "default": 20},
					"limit_occurence": map[string]any{"type": "integer", "default": 5},
					"dir":             map[string]any{"type": "string", "default": "/"},
				},
				"required": []string{"query"},
			},
		},
		{Name: "read_content", Description: "Read file content",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file":   map[string]any{"type": "string"},
					"start":  map[string]any{"type": "integer", "default": 0},
					"length": map[string]any{"type": "integer", "default": 2000},
				},
				"required": []string{"file"},
			},
		},
		{Name: "replace_content", Description: "Replace file content",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file":        map[string]any{"type": "string"},
					"original":    map[string]any{"type": "string"},
					"new":         map[string]any{"type": "string"},
					"allow_batch": map[string]any{"type": "boolean"},
				},
				"required": []string{"file", "original", "new"},
			},
		},
		{Name: "create_dir", Description: "Create a directory",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"dir": map[string]any{"type": "string"},
				},
				"required": []string{"dir"},
			},
		},
		{Name: "create_file", Description: "Create a file (use allow_rewrite=true to overwrite existing)",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file":          map[string]any{"type": "string"},
					"content":       map[string]any{"type": "string"},
					"allow_rewrite": map[string]any{"type": "boolean", "default": false},
				},
				"required": []string{"file"},
			},
		},
		{Name: "rm", Description: "Remove a file or empty folder",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file": map[string]any{"type": "string"},
				},
				"required": []string{"file"},
			},
		},
		{Name: "move", Description: "Move or copy a file",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"src":           map[string]any{"type": "string"},
					"dst":           map[string]any{"type": "string"},
					"keep_original": map[string]any{"type": "boolean"},
				},
				"required": []string{"src", "dst"},
			},
		},
	}

	return tools
}

func (p *Provider) CallTool(ctx context.Context, name string, args map[string]any) (*model.ToolResult, error) {
	if dir, ok := RootDirFromContext(ctx); ok {
		p = p.withRootDir(dir)
	}
	var result string

	switch name {
	case "tree":
		result = p.tree(ctx, args)
	case "search_name":
		result = p.searchName(ctx, args)
	case "search_content_plaintext":
		result = p.searchContentPlaintext(ctx, args)
	case "search_content_advanced":
		result = p.searchContentAdvanced(ctx, args)
	case "read_content":
		result = p.readContent(args)
	case "replace_content":
		result = p.replaceContent(args)
	case "create_dir":
		result = p.createDir(args)
	case "create_file":
		result = p.createFile(args)
	case "rm":
		result = p.rm(args)
	case "move":
		result = p.moveFile(args)
	default:
		result = "Error: Unknown tool"
	}

	return &model.ToolResult{
		Content: []model.ToolContent{
			{Type: "text", Text: result},
		},
	}, nil
}

func stripBOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func unifyNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

// walkInterrupted reports whether err came from a context cancellation
// propagated out of filepath.Walk.
func walkInterrupted(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() != nil
}

func (p *Provider) tree(ctx context.Context, args map[string]any) string {
	depth := 2
	if v, ok := args["depth"].(float64); ok {
		depth = int(v)
	}
	dirStr := "/"
	if v, ok := args["dir"].(string); ok {
		dirStr = v
	}
	limit := 1000
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}

	path, err := p.getSafePath(dirStr)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "Directory non-exist. (Hint: the base directory is ALREADY configured. Just create files in current directory unless otherwise specified by user.)"
	}

	var out strings.Builder
	lineCount := 0
	walkErr := filepath.Walk(path, func(fp string, info os.FileInfo, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(path, fp)
		if rel == "." {
			return nil
		}
		if IsIgnoredName(info.Name()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if len(strings.Split(rel, string(os.PathSeparator))) > depth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		lineCount++
		if lineCount <= limit {
			out.WriteString(rel + "\n")
		}
		return nil
	})

	if walkInterrupted(ctx, walkErr) {
		return "Error: tree interrupted (cancelled)"
	}
	if lineCount > limit {
		return fmt.Sprintf("line limit exceeded (%d > %d)", lineCount, limit)
	}

	result := out.String()
	if result == "" {
		result = "Empty directory"
	}

	if !p.sandboxDisabled && strings.HasPrefix(dirStr, "/") {
		result += "\nWARNING: Please use relative path."
	}
	return result
}

// stripRegexPrefix removes a leading "regex:" prefix (case-insensitive)
// that callers frequently prepend to queries instead of setting
// type="regex". It reports whether the prefix was present.
func stripRegexPrefix(query string) (string, bool) {
	const prefix = "regex:"
	if len(query) > len(prefix) && strings.EqualFold(query[:len(prefix)], prefix) {
		return query[len(prefix):], true
	}
	return query, false
}

func (p *Provider) searchName(ctx context.Context, args map[string]any) string {
	query, _ := args["query"].(string)
	matchType := "plain"
	typeExplicit := false
	if v, ok := args["type"].(string); ok && v != "" {
		matchType = v
		typeExplicit = true
	}
	var hasRegexPrefix bool
	query, hasRegexPrefix = stripRegexPrefix(query)
	if hasRegexPrefix && !typeExplicit {
		matchType = "regex"
	}
	globWarn := false
	if matchType == "plain" && strings.Contains(query, "*") {
		// "*" cannot be part of a file name on Windows; the query is
		// almost certainly intended as a glob pattern.
		matchType = "glob"
		globWarn = true
	}
	dirStr := "/"
	if v, ok := args["dir"].(string); ok {
		dirStr = v
	}
	limit := 20
	if v, ok := args["limit_file"].(float64); ok {
		limit = int(v)
	}

	var re *regexp.Regexp
	if matchType == "regex" {
		var err error
		re, err = regexp.Compile(query)
		if err != nil {
			return fmt.Sprintf("Error: invalid regex: %v", err)
		}
	}

	path, err := p.getSafePath(dirStr)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	var out strings.Builder
	count := 0
	walkErr := filepath.Walk(path, func(fp string, info os.FileInfo, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			return nil
		}
		if count >= limit {
			return filepath.SkipAll
		}
		if IsIgnoredName(info.Name()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		var match bool
		switch matchType {
		case "regex":
			match = re.MatchString(info.Name())
		case "glob":
			match, _ = filepath.Match(query, info.Name())
		default:
			match = strings.Contains(info.Name(), query)
		}
		if match {
			rel, _ := filepath.Rel(p.getRootDir(), fp)
			out.WriteString(rel + "\n")
			count++
		}
		return nil
	})
	if walkInterrupted(ctx, walkErr) {
		return "Error: search interrupted (cancelled)"
	}
	result := out.String()
	if result == "" {
		result = "No matches found."
	}
	if globWarn {
		result += "\nWARNING: query contains \"*\" which cannot be part of a file name; treated as glob. Set type=\"glob\" explicitly to silence this warning."
	}
	return result
}

func (p *Provider) searchContentPlaintext(ctx context.Context, args map[string]any) string {
	keyword, _ := args["keyword"].(string)
	// plaintext stays plain substring matching; just tolerate the prefix.
	keyword, _ = stripRegexPrefix(keyword)
	return p.searchContentImpl(ctx, keyword, "plain", args)
}

func (p *Provider) searchContentAdvanced(ctx context.Context, args map[string]any) string {
	query, _ := args["query"].(string)
	matchType := "glob"
	typeExplicit := false
	if v, ok := args["type"].(string); ok && v != "" {
		matchType = v
		typeExplicit = true
	}
	var hasRegexPrefix bool
	query, hasRegexPrefix = stripRegexPrefix(query)
	if hasRegexPrefix && !typeExplicit {
		matchType = "regex"
	}
	if matchType != "glob" && matchType != "regex" {
		return "Error: type must be \"glob\" or \"regex\""
	}
	return p.searchContentImpl(ctx, query, matchType, args)
}

func (p *Provider) searchContentImpl(ctx context.Context, query, matchType string, args map[string]any) string {
	fileGlob := "*"
	if v, ok := args["file_glob"].(string); ok {
		fileGlob = v
	}
	limitFile := 20
	if v, ok := args["limit_file"].(float64); ok {
		limitFile = int(v)
	}
	limitOccur := 5
	if v, ok := args["limit_occurence"].(float64); ok {
		limitOccur = int(v)
	}
	dirStr := "/"
	if v, ok := args["dir"].(string); ok {
		dirStr = v
	}

	var re *regexp.Regexp
	if matchType == "regex" {
		var err error
		re, err = regexp.Compile(query)
		if err != nil {
			return fmt.Sprintf("Error: invalid regex: %v", err)
		}
	}

	path, err := p.getSafePath(dirStr)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	type span struct{ start, end int }
	var buf strings.Builder
	filesFound := 0

	walkErr := filepath.Walk(path, func(fp string, info os.FileInfo, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			return nil
		}
		if IsIgnoredName(info.Name()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		match, _ := filepath.Match(fileGlob, info.Name())
		if !match {
			return nil
		}
		if filesFound >= limitFile {
			return filepath.SkipAll
		}
		if info.Size() > 1024*1024 {
			return nil
		}

		data, err := os.ReadFile(fp)
		if err != nil {
			return nil
		}
		data = stripBOM(data)

		lines := strings.Split(unifyNewlines(encoding.DecodeGB18030(data)), "\n")
		hits := 0
		var spans []span

		for i, line := range lines {
			if hits >= limitOccur {
				break
			}
			var match bool
			switch matchType {
			case "regex":
				match = re.MatchString(line)
			case "glob":
				match, _ = filepath.Match(query, line)
			default:
				match = strings.Contains(line, query)
			}
			if match {
				hits++
				start := i - 3
				if start < 0 {
					start = 0
				}
				end := i + 4
				if end > len(lines) {
					end = len(lines)
				}
				if len(spans) > 0 && spans[len(spans)-1].end >= start {
					spans[len(spans)-1].end = end
				} else {
					spans = append(spans, span{start, end})
				}
			}
		}

		if len(spans) > 0 {
			filesFound++
			rel, _ := filepath.Rel(p.getRootDir(), fp)
			buf.WriteString(fmt.Sprintf("==> %s\n", rel))
			for _, s := range spans {
				buf.WriteString(fmt.Sprintf("-- Lines %d-%d --\n", s.start+1, s.end))
				buf.WriteString(strings.Join(lines[s.start:s.end], "\n") + "\n")
			}
		}
		return nil
	})

	if walkInterrupted(ctx, walkErr) {
		return "Error: search interrupted (cancelled)"
	}
	result := buf.String()
	if result == "" {
		result = "No matches found."
	}
	return result
}

func (p *Provider) readContent(args map[string]any) string {
	file, _ := args["file"].(string)
	start := 0
	if v, ok := args["start"].(float64); ok {
		start = int(v)
	}
	length := 2000
	if v, ok := args["length"].(float64); ok {
		length = int(v)
	}

	if start < 0 || length <= 0 {
		return "Error: invalid start or length"
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(file), "."))
	for _, blExt := range p.extBlacklist {
		if ext == strings.ToLower(blExt) {
			return "Error: reading files: binary file?"
		}
	}

	path, err := p.getSafePath(file)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	if stat, err := os.Stat(path); err == nil && stat.Size() > 1024*1024 {
		return "Error: file is larger than 1MB"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	data = stripBOM(data)

	lines := strings.Split(unifyNewlines(encoding.DecodeGB18030(data)), "\n")
	if start >= len(lines) {
		return fmt.Sprintf("Error: file has only %d lines", len(lines))
	}

	end := start + length
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n")
}

func (p *Provider) replaceContent(args map[string]any) string {
	file, _ := args["file"].(string)
	orig, _ := args["original"].(string)
	newStr, _ := args["new"].(string)
	batch, _ := args["allow_batch"].(bool)

	if orig == "" {
		return "Error: original content cannot be empty"
	}

	path, err := p.getSafePath(file)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	if stat, err := os.Stat(path); err == nil && stat.Size() > 1024*1024 {
		return "Error: file is larger than 1MB"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	data = stripBOM(data)

	content := unifyNewlines(encoding.DecodeGB18030(data))
	orig = unifyNewlines(orig)
	newStr = unifyNewlines(newStr)
	count := strings.Count(content, orig)
	if count == 0 {
		return "Error: original content not found"
	}
	if count > 1 && !batch {
		return "Error: allow_batch is false but multiple hits found. No changes made."
	}

	newContent := strings.ReplaceAll(content, orig, newStr)
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return "Success"
}

func (p *Provider) createDir(args map[string]any) string {
	dir, _ := args["dir"].(string)
	path, warn, err := p.getSafePathWithWarning(dir)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	result := "Success"
	if !p.sandboxDisabled && warn {
		result += "\nWARNING: Please use relative path."
	}
	return result
}

func (p *Provider) createFile(args map[string]any) string {
	file, _ := args["file"].(string)
	content, _ := args["content"].(string)
	allowRewrite, _ := args["allow_rewrite"].(bool)
	path, warn, err := p.getSafePathWithWarning(file)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return "Error: parent directory does not exist"
	}
	if _, err := os.Stat(path); err == nil {
		if !allowRewrite {
			return "Error: file already exists. (Hint: set allow_rewrite=true to overwrite)"
		}
		_ = MoveToTrash(p.getRootDir(), path)
	}
	if err := os.WriteFile(path, []byte(unifyNewlines(content)), 0644); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	result := "Success"
	if !p.sandboxDisabled && warn {
		result += "\nWARNING: Please use relative path."
	}
	return result
}

func (p *Provider) rm(args map[string]any) string {
	file, _ := args["file"].(string)
	path, err := p.getSafePath(file)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	if err := MoveToTrash(p.getRootDir(), path); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return "Success"
}

func (p *Provider) moveFile(args map[string]any) string {
	src, _ := args["src"].(string)
	dst, _ := args["dst"].(string)
	keep, _ := args["keep_original"].(bool)

	srcPath, err1 := p.getSafePath(src)
	dstPath, err2 := p.getSafePath(dst)
	if err1 != nil || err2 != nil {
		return "Error: path security error"
	}

	if err := copyFile(srcPath, dstPath); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	if !keep {
		_ = os.Remove(srcPath)
	}
	return "Success"
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
