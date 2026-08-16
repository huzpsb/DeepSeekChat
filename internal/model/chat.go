package model

type Chat struct {
	Title    string    `json:"title"`
	RootDir  string    `json:"root_dir,omitempty"`
	Messages []Message `json:"messages"`
	// ContextSize is the input (prompt) token count reported by the server
	// for the most recent request. It is updated in memory as usage arrives
	// and persisted lazily together with the next regular save.
	ContextSize int `json:"context_size,omitempty"`
}

type ChatSummary struct {
	Title   string `json:"title"`
	Running bool   `json:"running,omitempty"`
}

type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	Name             string     `json:"name,omitempty"`
	SendToServer     bool       `json:"send_to_server"`
	Approved         bool       `json:"approved,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
