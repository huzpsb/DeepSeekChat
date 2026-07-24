package deepseek

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"hschat/internal/model"
)

const deepseekURL = "https://api.deepseek.com/chat/completions"

type StreamEvent struct {
	Type     string `json:"type"`
	Content  string `json:"content,omitempty"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Args     string `json:"arguments,omitempty"`
	ToolType string `json:"tool_type,omitempty"`
	Error    string `json:"error,omitempty"`
}

type Client struct {
	apiKey     string
	httpClient *http.Client
	endpoint   string
	model      string
}

func NewClient(apiKey string, thirdParty model.ThirdPartyConfig) *Client {
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}
	endpoint := deepseekURL
	modelName := "deepseek-v4-pro"
	if thirdParty.Enabled {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		endpoint = thirdParty.Endpoint
		modelName = thirdParty.Model
	}
	return &Client{
		apiKey:     apiKey,
		httpClient: client,
		endpoint:   endpoint,
		model:      modelName,
	}
}

func (c *Client) SetAPIKey(key string) {
	c.apiKey = key
}

func (c *Client) StreamChat(ctx context.Context, messages []model.Message, tools []model.ToolDef, onEvent func(StreamEvent)) error {
	apiMessages := buildAPIMessages(messages)
	reqBody := map[string]any{
		"model":            c.model,
		"messages":         apiMessages,
		"thinking":         map[string]string{"type": "enabled"},
		"reasoning_effort": "high",
		"stream":           true,
	}

	if len(tools) > 0 {
		var dsTools []map[string]any
		for _, t := range tools {
			params := t.InputSchema
			if params == nil {
				params = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			dsTools = append(dsTools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  params,
				},
			})
		}
		reqBody["tools"] = dsTools
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	parseSSE(resp.Body, onEvent)
	return nil
}

func buildAPIMessages(messages []model.Message) []map[string]any {
	var result []map[string]any
	for _, msg := range messages {
		if !msg.SendToServer {
			continue
		}
		m := map[string]any{
			"role": msg.Role,
		}
		if msg.Content != "" || msg.Role == "tool" {
			m["content"] = msg.Content
		}
		if msg.ReasoningContent != "" {
			m["reasoning_content"] = msg.ReasoningContent
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			var tcs []map[string]any
			for _, tc := range msg.ToolCalls {
				tcs = append(tcs, map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Function.Name,
						"arguments": tc.Function.Arguments,
					},
				})
			}
			m["tool_calls"] = tcs
		}
		if msg.Role == "tool" {
			m["tool_call_id"] = msg.ToolCallID
		}
		result = append(result, m)
	}
	return result
}

func parseSSE(body io.Reader, onEvent func(StreamEvent)) {
	scanner := bufio.NewScanner(body)
	var currentToolCall struct {
		ID   string
		Name string
		Args string
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			onEvent(StreamEvent{Type: "done"})
			return
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Role             string `json:"role"`
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta

		if delta.Content != "" {
			onEvent(StreamEvent{Type: "delta", Content: delta.Content})
		}
		if delta.ReasoningContent != "" {
			onEvent(StreamEvent{Type: "reasoning_delta", Content: delta.ReasoningContent})
		}
		for _, tc := range delta.ToolCalls {
			if tc.ID != "" {
				currentToolCall.ID = tc.ID
				currentToolCall.Name = ""
				currentToolCall.Args = ""
			}
			if tc.Function.Name != "" {
				currentToolCall.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				currentToolCall.Args += tc.Function.Arguments
			}
			if currentToolCall.ID != "" && currentToolCall.Name != "" {
				onEvent(StreamEvent{
					Type: "tool_call",
					ID:   currentToolCall.ID,
					Name: currentToolCall.Name,
					Args: currentToolCall.Args,
				})
			}
		}
	}

	onEvent(StreamEvent{Type: "done"})
}
