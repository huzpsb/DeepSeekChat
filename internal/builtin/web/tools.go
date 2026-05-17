package web

import (
	"fmt"
	"strings"
	"time"

	"hschat/internal/builtin/web/jsrt"
	"hschat/internal/model"
)

func (p *Provider) Tools() []model.ToolDef {
	return []model.ToolDef{
		{
			Name:        "webjs_execute",
			Description: "Execute a .js file using the custom webjs runtime. WARNING: webjs is NOT Node.js and only supports ES5.1. Use webjs_help first to check available APIs.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_name": map[string]any{"type": "string", "description": "JavaScript file path."},
					"time_out":  map[string]any{"type": "integer", "description": "Timeout in seconds (max: 1800).", "default": 60},
				},
				"required": []string{"file_name"},
			},
		},
		{
			Name:        "webjs_help",
			Description: "Display the API documentation and capabilities of the webjs runtime environment.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
	}
}

func (p *Provider) CallTool(name string, args map[string]any) (*model.ToolResult, error) {
	var result string

	switch name {
	case "webjs_execute":
		result = p.execute(args)
	case "webjs_help":
		result = p.webHelp(args)
	default:
		result = "Error: Unknown tool"
	}

	return &model.ToolResult{
		Content: []model.ToolContent{
			{Type: "text", Text: result},
		},
	}, nil
}

func (p *Provider) execute(args map[string]any) string {
	name, ok := args["file_name"].(string)
	if !ok {
		return "Error: missing or invalid 'file_name' argument"
	}
	if !strings.HasSuffix(name, ".js") {
		return "Error: only .js files can be executed"
	}

	timeout := 60
	if t, ok := args["time_out"]; ok {
		switch v := t.(type) {
		case float64:
			timeout = int(v)
		case int:
			timeout = v
		case int64:
			timeout = int(v)
		}
		if timeout < 1 {
			timeout = 1
		}
		if timeout > 1800 {
			timeout = 1800
		}
	}

	jsCfg := &jsrt.Config{
		RootDir: p.rootDir,
		Headers: p.headers,
		Proxy:   p.proxy,
	}

	runtime := jsrt.New(jsCfg)
	defer runtime.Close()
	consoleOut, retVal, elapsed, execErr := runtime.Execute(name, time.Duration(timeout)*time.Second)

	var result strings.Builder
	if consoleOut != "" {
		result.WriteString(consoleOut)
	}
	if execErr != nil {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		result.WriteString("--- error ---\n")
		result.WriteString(execErr.Error())
	} else {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		result.WriteString("--- return ---\n")
		result.WriteString(retVal)
	}
	result.WriteString(fmt.Sprintf("\n--- elapsed ---\n%.3fs", elapsed.Seconds()))

	return result.String()
}

func (p *Provider) webHelp(_ map[string]any) string {
	return `JavaScript Sandbox Functions
=====================

IMPORTANT: This sandbox runs ES5.1. ES6+ features (arrow functions, let/const, template
literals, etc.) are NOT supported.

web_fetch(url: string, headers?: object) -> {status: int, body: string|Uint8Array, headers: map[string]string}
  Performs an HTTP GET request. Returns an object with status code, response body, and
  response headers. The body is a string for text responses (text/*, application/json, etc.)
  and a Uint8Array for binary responses (images, application/octet-stream, etc.).
  The optional headers argument is an object of custom request headers.
  Basic headers (User-Agent, etc.) are set automatically by the runtime, with Chrome's
  TLS fingerprint.
  May throw: invalid URL, fetch failure.

webjs_tree(dir?: string, depth?: int) -> []string
  Lists directory contents recursively. Returns an array of strings, each representing one
  file or directory entry as a relative path. The optional dir parameter specifies the starting directory.
  depth controls recursion depth (1-10, default 2).
  May throw: invalid directory, path traversal.

webjs_read(path: string) -> string
  Reads a file and returns its contents as a string.
  May throw: invalid path, file not found, path traversal.

webjs_write(path: string, content: string) -> void
  Writes content to a file, creating it if it doesn't exist, overwriting if it does.
  May throw: invalid path, write failure, path traversal.

webjs_delete(path: string) -> void
  Deletes a file or empty directory. Non-empty directories cannot be deleted.
  May throw: invalid path, file not found, directory not empty, path traversal.

webjs_create_folder(path: string) -> void
  Creates a new directory, including any necessary parent directories.
  May throw: invalid path, creation failure, path traversal.

console.log(...args: any) -> void
  Prints arguments to the script's console output buffer (100KB limit).
  May throw: buffer overflow (100KB limit).`
}
