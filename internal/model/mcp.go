package model

type MCPConfig struct {
	APIKey                string      `json:"api_key"`
	EnableCodingTools     bool        `json:"enable_coding_tools"`
	MCPServers            []MCPServer `json:"mcp_servers"`
	ApprovedTools         []string    `json:"approved_tools"`
	ManuallyApprovedTools []string    `json:"manually_approved_tools"`
}

type MCPServer struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	URL     string   `json:"url,omitempty"`
	Command []string `json:"command,omitempty"`
}

type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}
