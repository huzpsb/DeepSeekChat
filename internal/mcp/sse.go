package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"hschat/internal/model"
)

type SSEClient struct {
	name    string
	url     string
	conn    bool
	mu      sync.Mutex
	session string
}

func NewSSEClient(name, url string) *SSEClient {
	return &SSEClient{name: name, url: url}
}

func (c *SSEClient) Name() string      { return c.name }
func (c *SSEClient) Type() string      { return "sse" }
func (c *SSEClient) IsConnected() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.conn }

func (c *SSEClient) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.conn = false
	c.session = ""

	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
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

	body, err := json.Marshal(initReq)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequest("POST", c.url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("initialize POST: %w", err)
	}
	defer resp.Body.Close()

	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.session = sid
	}

	result, err := parseSSEResponse(resp.Body)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if result["error"] != nil {
		return fmt.Errorf("initialize error: %v", result["error"])
	}

	c.conn = true

	notified := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	c.sendJSONRPC(context.Background(), notified)

	return nil
}

func (c *SSEClient) ListTools() ([]model.ToolDef, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	}

	result, err := c.sendJSONRPC(context.Background(), req)
	if err != nil {
		return nil, err
	}

	if result["error"] != nil {
		return nil, fmt.Errorf("list tools error: %v", result["error"])
	}

	r, ok := result["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected tools/list result format")
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
			Name:        tm["name"].(string),
			Description: getString(tm, "description"),
			InputSchema: tm["inputSchema"],
		})
	}

	return tools, nil
}

func (c *SSEClient) CallTool(ctx context.Context, name string, arguments map[string]any) (*model.ToolResult, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	}

	result, err := c.sendJSONRPC(ctx, req)
	if err != nil {
		return nil, err
	}

	if result["error"] != nil {
		errData, _ := json.Marshal(result["error"])
		return &model.ToolResult{
			Content: []model.ToolContent{{
				Type: "text",
				Text: "Error: " + string(errData),
			}},
			IsError: true,
		}, nil
	}

	tr := &model.ToolResult{}
	if r, ok := result["result"].(map[string]any); ok {
		if content, ok := r["content"].([]any); ok {
			for _, c := range content {
				if cm, ok := c.(map[string]any); ok {
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

func (c *SSEClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn = false
	return nil
}

func (c *SSEClient) sendJSONRPC(ctx context.Context, req map[string]any) (map[string]any, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.url, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	if c.session != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.session)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return parseSSEResponse(resp.Body)
}

func parseSSEResponse(r io.Reader) (map[string]any, error) {
	var buf strings.Builder
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("read SSE response: %w", err)
		}
		line = strings.TrimSpace(line)

		if line == "" {
			if err == io.EOF {
				break
			}
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var result map[string]any
			if err := json.Unmarshal([]byte(data), &result); err != nil {
				return nil, fmt.Errorf("parse SSE data: %w (body: %s)", err, data)
			}
			return result, nil
		}

		buf.WriteString(line)
		if err == io.EOF {
			break
		}
	}

	if buf.Len() == 0 {
		return nil, fmt.Errorf("read SSE response: EOF before data")
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(buf.String()), &result); err != nil {
		return nil, fmt.Errorf("parse response: %w (body: %s)", err, buf.String())
	}
	return result, nil
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
