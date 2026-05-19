package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"hschat/internal/model"
)

type StdioClient struct {
	name    string
	command []string
	conn    bool
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	reader  *bufio.Reader
	counter int
}

func NewStdioClient(name string, command []string) *StdioClient {
	return &StdioClient{
		name:    name,
		command: command,
		counter: 1,
	}
}

func (c *StdioClient) Name() string      { return c.name }
func (c *StdioClient) Type() string      { return "stdio" }
func (c *StdioClient) IsConnected() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.conn }

func (c *StdioClient) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.conn = false
	c.cmd = exec.Command(c.command[0], c.command[1:]...)

	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create stdin pipe: %w", err)
	}
	c.stdout, err = c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	c.reader = bufio.NewReader(c.stdout)

	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.counter,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]string{
				"name":    "DsChat",
				"version": "1.0.0",
			},
		},
	}
	c.counter++

	result, err := c.sendRequestLocked(initReq)
	if err != nil {
		c.cmd.Process.Kill()
		return fmt.Errorf("initialize: %w", err)
	}
	if result["error"] != nil {
		c.cmd.Process.Kill()
		return fmt.Errorf("initialize error: %v", result["error"])
	}

	notified := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	c.sendNotificationLocked(notified)

	c.conn = true
	return nil
}

func (c *StdioClient) ListTools() ([]model.ToolDef, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "tools/list",
		"params":  map[string]any{},
	}

	result, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}
	if result["error"] != nil {
		return nil, fmt.Errorf("list tools error: %v", result["error"])
	}

	r, ok := result["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected tools/list format")
	}

	toolsRaw, ok := r["tools"].([]any)
	if !ok {
		return nil, fmt.Errorf("tools not found in result")
	}

	var tools []model.ToolDef
	for _, t := range toolsRaw {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		tools = append(tools, model.ToolDef{
			Name:        getString(tm, "name"),
			Description: getString(tm, "description"),
			InputSchema: tm["inputSchema"],
		})
	}
	return tools, nil
}

func (c *StdioClient) CallTool(name string, arguments map[string]any) (*model.ToolResult, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	}

	result, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if result["error"] != nil {
		errData, _ := json.Marshal(result["error"])
		return &model.ToolResult{
			Content: []model.ToolContent{{Type: "text", Text: "Error: " + string(errData)}},
			IsError: true,
		}, nil
	}

	tr := &model.ToolResult{}
	if r, ok := result["result"].(map[string]any); ok {
		if content, ok := r["content"].([]any); ok {
			for _, cont := range content {
				if cm, ok := cont.(map[string]any); ok {
					tc := model.ToolContent{Type: getString(cm, "type")}
					if cm["text"] != nil {
						tc.Text = fmt.Sprint(cm["text"])
					}
					tr.Content = append(tr.Content, tc)
				}
			}
		}
		if ie, ok := r["isError"].(bool); ok {
			tr.IsError = ie
		}
	}
	return tr, nil
}

func (c *StdioClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn = false
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	return nil
}

func (c *StdioClient) nextID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.counter
	c.counter++
	return id
}

func (c *StdioClient) sendRequest(req map[string]any) (map[string]any, error) {
	return c.sendRequestLocked(req)
}

func (c *StdioClient) sendRequestLocked(req map[string]any) (map[string]any, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := c.stdin.Write(append(body, '\n')); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	lineBytes := []byte(line)
	var result map[string]any
	if err := json.Unmarshal(lineBytes, &result); err != nil {
		decoded, decErr := decodeGBK(lineBytes)
		if decErr == nil {
			if err2 := json.Unmarshal(decoded, &result); err2 == nil {
				return result, nil
			}
		}
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

func decodeGBK(data []byte) ([]byte, error) {
	reader := transform.NewReader(bytes.NewReader(data), simplifiedchinese.GB18030.NewDecoder())
	return io.ReadAll(reader)
}

func (c *StdioClient) sendNotificationLocked(req map[string]any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = c.stdin.Write(append(body, '\n'))
	return err
}
