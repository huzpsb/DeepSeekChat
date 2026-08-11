package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"hschat/internal/model"
)

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

func NewClient(endpoint, apiKey, model string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		endpoint: endpoint,
		model:    model,
	}
}

func (c *Client) StreamChat(ctx context.Context, messages []model.Message, tools []model.ToolDef, onEvent func(StreamEvent)) error {
	messages = maybeReplaceSystemPrompt(messages)
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

	// Retry immediately when the server responds with 429 (rate limited),
	// up to maxRetries times. If the request still fails with 429 after all
	// retries, give up and return the error.
	const maxRetries = 5

	var resp *http.Response
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, strings.NewReader(string(body)))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Accept", "text/event-stream")

		resp, err = c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}

		if resp.StatusCode != http.StatusTooManyRequests || attempt >= maxRetries {
			break
		}
		// 429: drain and close the body before retrying immediately.
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	parseSSE(resp.Body, onEvent)
	return nil
}

var (
	systemPromptOnce  sync.Once
	systemPromptCache string // empty = file not found or not yet loaded
)

// maybeReplaceSystemPrompt silently replaces the first system message with
// system.txt content when the original is exactly the default "You are a helpful
// assistant." (whitespace-stripped, case-insensitive). The original messages
// slice is never mutated — a copy is returned on replacement.
// system.txt is read once and cached for the lifetime of the process.
func maybeReplaceSystemPrompt(messages []model.Message) []model.Message {
	if len(messages) == 0 || messages[0].Role != "system" {
		return messages
	}
	stripped := strings.TrimSpace(messages[0].Content)
	if !strings.EqualFold(stripped, "You are a helpful assistant.") {
		return messages
	}

	systemPromptOnce.Do(func() {
		data, err := os.ReadFile("system.txt")
		if err == nil {
			systemPromptCache = string(data)
		}
	})

	if systemPromptCache == "" {
		return messages
	}

	copied := make([]model.Message, len(messages))
	copy(copied, messages)
	copied[0].Content = systemPromptCache
	return copied
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
