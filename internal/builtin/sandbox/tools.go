package sandbox

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
				},
			},
		},
		{Name: "search_name", Description: "Search files or folders by name",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword":    map[string]any{"type": "string"},
					"limit_file": map[string]any{"type": "integer", "default": 20},
					"dir":        map[string]any{"type": "string", "default": "/"},
				},
				"required": []string{"keyword"},
			},
		},
		{Name: "search_content", Description: "Search files containing keyword",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword":         map[string]any{"type": "string"},
					"limit_file":      map[string]any{"type": "integer", "default": 20},
					"limit_occurence": map[string]any{"type": "integer", "default": 5},
					"dir":             map[string]any{"type": "string", "default": "/"},
				},
				"required": []string{"keyword"},
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
		{Name: "create_file", Description: "Create a file",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file":    map[string]any{"type": "string"},
					"content": map[string]any{"type": "string"},
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
		{Name: "rewrite_file", Description: "Rewrite file content",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file":    map[string]any{"type": "string"},
					"content": map[string]any{"type": "string"},
				},
				"required": []string{"file", "content"},
			},
		},
	}

	return tools
}

func (p *Provider) CallTool(name string, args map[string]any) (*model.ToolResult, error) {
	var result string

	switch name {
	case "tree":
		result = p.tree(args)
	case "search_name":
		result = p.searchName(args)
	case "search_content":
		result = p.searchContent(args)
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
	case "rewrite_file":
		result = p.rewriteFile(args)
	default:
		result = "Error: Unknown tool"
	}

	return &model.ToolResult{
		Content: []model.ToolContent{
			{Type: "text", Text: result},
		},
	}, nil
}

func (p *Provider) tree(args map[string]any) string {
	depth := 2
	if v, ok := args["depth"].(float64); ok {
		depth = int(v)
	}
	dirStr := "/"
	if v, ok := args["dir"].(string); ok {
		dirStr = v
	}

	path, err := p.getSafePath(dirStr)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "Directory non-exist. (Hint: the base directory is ALREADY configured. Just create files in current directory unless otherwise specified by user.)"
	}

	var out strings.Builder
	_ = filepath.Walk(path, func(fp string, info os.FileInfo, err error) error {
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
		out.WriteString(rel + "\n")
		return nil
	})
	result := out.String()
	if result == "" {
		result = "Empty directory"
	}

	if strings.HasPrefix(dirStr, "/") {
		result += "\nWARNING: Please use relative path."
	}
	return result
}

func (p *Provider) searchName(args map[string]any) string {
	keyword, _ := args["keyword"].(string)
	dirStr := "/"
	if v, ok := args["dir"].(string); ok {
		dirStr = v
	}
	limit := 20
	if v, ok := args["limit_file"].(float64); ok {
		limit = int(v)
	}

	path, err := p.getSafePath(dirStr)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	var out strings.Builder
	count := 0
	_ = filepath.Walk(path, func(fp string, info os.FileInfo, err error) error {
		if err != nil || count >= limit {
			return nil
		}
		if IsIgnoredName(info.Name()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.Contains(info.Name(), keyword) {
			rel, _ := filepath.Rel(p.rootDir, fp)
			out.WriteString(rel + "\n")
			count++
		}
		return nil
	})
	return out.String()
}

func (p *Provider) searchContent(args map[string]any) string {
	keyword, _ := args["keyword"].(string)
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

	path, err := p.getSafePath(dirStr)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	type span struct{ start, end int }
	var buf strings.Builder
	filesFound := 0

	_ = filepath.Walk(path, func(fp string, info os.FileInfo, err error) error {
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

		lines := strings.Split(string(data), "\n")
		hits := 0
		var spans []span

		for i, line := range lines {
			if hits >= limitOccur {
				break
			}
			if strings.Contains(line, keyword) {
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
			rel, _ := filepath.Rel(p.rootDir, fp)
			buf.WriteString(fmt.Sprintf("==> %s\n", rel))
			for _, s := range spans {
				buf.WriteString(fmt.Sprintf("-- Lines %d-%d --\n", s.start+1, s.end))
				buf.WriteString(strings.Join(lines[s.start:s.end], "\n") + "\n")
			}
		}
		return nil
	})

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

	lines := strings.Split(string(data), "\n")
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

	content := string(data)
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
	if warn {
		result += "\nWARNING: Please use relative path."
	}
	return result
}

func (p *Provider) createFile(args map[string]any) string {
	file, _ := args["file"].(string)
	content, _ := args["content"].(string)
	path, warn, err := p.getSafePathWithWarning(file)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return "Error: parent directory does not exist"
	}
	if _, err := os.Stat(path); err == nil {
		return "Error: file already exists, create_file cannot overwrite existing files"
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	result := "Success"
	if warn {
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
	if err := p.moveToTrash(path); err != nil {
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

func (p *Provider) rewriteFile(args map[string]any) string {
	file, _ := args["file"].(string)
	content, _ := args["content"].(string)

	path, err := p.getSafePath(file)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	stat, err := os.Stat(path)
	if err != nil {
		return "Error: file does not exist, rewrite_file cannot create new files"
	}
	if stat.Size() > 1024*1024 {
		return "Error: file is larger than 1MB"
	}

	_ = p.moveToTrash(path)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Sprintf("Error: %v", err)
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
