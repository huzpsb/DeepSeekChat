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

// maxNewTokens 是转发给服务端的最大生成 token 数（128K），
// 避免部分服务端默认值过小导致长回答被截断。
const maxNewTokens = 128 * 1024

type StreamEvent struct {
	Type     string `json:"type"`
	Content  string `json:"content,omitempty"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Args     string `json:"arguments,omitempty"`
	ToolType string `json:"tool_type,omitempty"`
	Error    string `json:"error,omitempty"`
	// PromptTokens is set on Type=="usage" events: the input token count of
	// the request as reported by the server's usage field.
	PromptTokens int `json:"prompt_tokens,omitempty"`
}

type Client struct {
	apiKey     string
	httpClient *http.Client
	endpoint   string
	model      string
}

func NewClient(endpoint, apiKey, model string) *Client {
	// No total http.Client.Timeout: it covers the streaming body read, so a
	// long generation (a model emitting tens of thousands of reasoning tokens
	// at ~200 tok/s needs minutes; 128K max_new_tokens needs more) would be
	// cut off mid-stream — and a truncated body used to be reported by
	// parseSSE as a normal "done". Only the wait for response headers is
	// bounded; a broken or truncated stream is surfaced as an error by
	// parseSSE, and mid-stream liveness is left to ctx cancellation (user
	// interrupt).
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 2 * time.Minute
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{Transport: transport},
		endpoint:   endpoint,
		model:      model,
	}
}

// Endpoint and Model expose the client's fixed configuration (captured at
// construction; a config change means a new Client).
func (c *Client) Endpoint() string { return c.endpoint }
func (c *Client) Model() string    { return c.model }

func (c *Client) StreamChat(ctx context.Context, messages []model.Message, tools []model.ToolDef, onEvent func(StreamEvent)) error {
	messages = maybeReplaceSystemPrompt(messages)
	apiMessages := buildAPIMessages(messages)
	if isDeepSeekModel(c.model) {
		padDeepSeekAssistantReasoning(apiMessages)
	}
	reqBody := map[string]any{
		"model":            c.model,
		"messages":         apiMessages,
		"thinking":         map[string]string{"type": "enabled"},
		"reasoning_effort": "high",
		"max_new_tokens":   maxNewTokens,
		"stream":           true,
		// Ask OpenAI-compatible servers to attach a final usage chunk to the
		// stream so we can record the actual input token count. Servers that
		// don't support it simply ignore the option.
		"stream_options": map[string]bool{"include_usage": true},
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
		req.Header.Set("User-Agent", "DsChat/v1")

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

	if err := parseSSE(resp.Body, onEvent); err != nil {
		return fmt.Errorf("stream interrupted: %w", err)
	}
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

// isDeepSeekModel reports whether the configured model name contains
// "deepseek" (case-insensitive).
func isDeepSeekModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "deepseek")
}

// padDeepSeekAssistantReasoning ensures every assistant message sent to
// DeepSeek has a non-empty reasoning_content field. DeepSeek requires this
// for tool-call requests, but we must not persist the padding back into
// stored messages because other providers (e.g. Kimi) prefer no reasoning
// content. This only mutates the in-memory request maps.
func padDeepSeekAssistantReasoning(apiMessages []map[string]any) {
	for _, m := range apiMessages {
		if role, _ := m["role"].(string); role != "assistant" {
			continue
		}
		if rc, ok := m["reasoning_content"].(string); !ok || rc == "" {
			m["reasoning_content"] = " "
		}
	}
}

// parseSSE consumes the event stream until [DONE] or EOF. A read error
// (connection dropped, body truncated, line over the buffer limit) is
// returned as an error — it must NOT be mistaken for a clean completion,
// or a truncated generation would look like a finished assistant message.
func parseSSE(body io.Reader, onEvent func(StreamEvent)) error {
	scanner := bufio.NewScanner(body)
	// SSE lines are usually tiny, but a server may pack large deltas into a
	// single data line; allow up to 4MB before erroring out.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
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
			return nil
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
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		// The usage chunk (usually the last one, with empty choices) carries
		// the token accounting of the whole request.
		if chunk.Usage != nil && chunk.Usage.PromptTokens > 0 {
			onEvent(StreamEvent{Type: "usage", PromptTokens: chunk.Usage.PromptTokens})
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

	if err := scanner.Err(); err != nil {
		return err
	}
	onEvent(StreamEvent{Type: "done"})
	return nil
}
