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
			Name: "webjs_help",
			Description: "Display the API documentation and capabilities of the webjs runtime environment. " +
				"Also useful for memories and maths. Run this tool first if you are confused about your tools.",
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

webjs_list(dir?: string) -> []string
  Lists directory contents non-recursively. Returns an array of strings, each representing one
  file or directory entry by name. The optional dir parameter specifies the directory to list.
  Directories are suffixed with "/". 
  May throw: invalid directory, path traversal.

webjs_read(path: string) -> string
  Reads a file and returns its contents as a string.
  May throw: invalid path, file not found, path traversal.

webjs_write(path: string, content: string|Uint8Array) -> void
  Writes content to a file, creating it if it doesn't exist, overwriting if it does.
  Accepts a string for text content or a Uint8Array (e.g. from web_fetch) for binary data.
  May throw: invalid path, write failure, path traversal.

webjs_delete(path: string) -> void
  Deletes a file or empty directory. Non-empty directories cannot be deleted.
  May throw: invalid path, file not found, directory not empty, path traversal.

webjs_test(path: string) -> bool
  Tests whether a file or directory exists without reading its contents.
  Returns true if the path exists, false otherwise.
  May throw: path traversal.

webjs_create_folder(path: string) -> void
  Creates a new directory, including any necessary parent directories.
  May throw: invalid path, creation failure, path traversal.

webjs_move(src: string, dst: string) -> void
  Moves a file or directory from src to dst path.
  May throw: invalid path, move failure, path traversal.

webjs_clean_tmp(dir: string) -> int
  Deletes all .tmp files in the specified directory (non-recursive).
  Returns the number of files deleted.
  May throw: invalid directory, path traversal.

webjs_batch_download_append(url: string, dir: string, retries?: int, filename?: string) -> void
  Registers a batch download task and returns immediately. The file is downloaded
  to the specified directory using a multi-threaded worker pool. Use webjs_test to check
  whether the file has been downloaded successfully.
  If filename is omitted, it is derived from the URL path.
  May throw: invalid directory, path traversal, pool cleared.

webjs_batch_download_remaining() -> int
  Returns the number of tasks still pending (not yet succeeded or failed).
  The lifecycle of batch_download is the same as the JS interpreter. You MUST loop and wait on webjs_batch_download_remaining until it returns 0, otherwise the downloads will not proceed as expected.

webjs_batch_download_clear() -> void
  Immediately cancels all pending and in-flight download tasks.

console.log(...args: any) -> void
  Prints arguments to the script's console output buffer (100KB limit).
  May throw: buffer overflow (100KB limit).

Best Practices for Large Scraping Tasks:
Because of execution timeouts, web scraping operations involving large amounts of pages or files should be optimized with the batch download API.
1. Recommended workflow: Always run a single-threaded test on 3-5 items first to verify that your download and data extraction logic works correctly before using the batch API.
2. Task Queue Management: BEFORE starting any batch downloads, gather the target URLs and MUST SAVE the task list to disk. If the task list already exists on disk, MUST LOAD it directly instead of re-fetching the index. This prevents issues if the source CMS updates file names during the process, avoiding duplication or failure.
3. For HTML pages:
   - Load or build the task queue as described above.
   - Run batch downloads to a temporary folder, saving the files with a ".tmp" extension (create the folder if it does not exist).
   - Loop and wait (using webjs_batch_download_remaining) until tasks log as complete.
   - Once finished, read the downloaded ".tmp" files, parse/extract the required JSON or structured data, and group them into appropriate files (e.g., 1k items per JSON).
   - Finally, clean up the temporary files using webjs_clean_tmp. NEVER permanently save a large bunch of raw HTML files.
4. For binary files:
   - Load or build the task queue and use the batch download API.
   - If a directory tree structure is required, ensure all necessary output directories are created using webjs_create_folder before submitting the tasks.
5. Resumability for >= 500 files:
   - For large scales, consider potential mid-way failures. Resuming is very simple: the batch submitter automatically skips files that have already been fully downloaded, so there is no need to call webjs_test manually.
   - Just ensure that across multiple runs, the target folder and file names remain strictly consistent so resuming naturally skips completed items.
6. Keep logs as concise as possible to prevent execution being killed due to console buffer overflow.
7. Even with multi-threaded batch downloads, setting a long execution time_out is often helpful to ensure all download tasks have enough time to finish.

General Best Practices for webjs:
1. webjs is designed not just for scraping, but also for document and workspace management.
2. Before starting a scrape, check your workspace directories. You might have already completed or partially completed the scrape.
3. During scraping, use semantic names for directories (Good: "aaai 2022 papers", Bad: "scraper_dir").
4. After scraping, generate artifacts like "readme.md" and "searcher.js" so you can easily search/filter data later by just reading the readme and running the searcher, without needing to write new test scripts from scratch.
5. You should maintain documents like MEMORY.MD or PROJECTS.MD in the root directory for cross-conversation memory management.
6. Keep the root directory clean. If you need to store temporary files or junk, create a directory like "./tmp" for them.
`
}
