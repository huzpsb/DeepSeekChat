package model

type MCPConfig struct {
	APIKey                string           `json:"api_key"`
	Sandbox               SandboxConfig    `json:"sandbox"`
	EnableCodingTools     bool             `json:"enable_coding_tools"`
	EnableWebTools        bool             `json:"enable_web_tools"`
	MCPServers            []MCPServer      `json:"mcp_servers"`
	DefaultPrompt         string           `json:"default_prompt"`
	ApprovedTools         []string         `json:"approved_tools"`
	ManuallyApprovedTools []string         `json:"manually_approved_tools"`
	ThirdParty            ThirdPartyConfig `json:"3rd_party"`
}

type ThirdPartyConfig struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
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
