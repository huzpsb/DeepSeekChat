package model

type MCPConfig struct {
	Port                  int             `json:"port"`
	Sandbox               SandboxConfig   `json:"sandbox"`
	EnableCodingTools     bool            `json:"enable_coding_tools"`
	MCPServers            []MCPServer     `json:"mcp_servers"`
	DefaultPrompt         string          `json:"default_prompt"`
	ApprovedTools         []string        `json:"approved_tools"`
	ManuallyApprovedTools []string        `json:"manually_approved_tools"`
	ModelProviders        []ModelProvider `json:"model_providers"`
	Provider              string          `json:"provider"`
	Model                 string          `json:"model"`
}

type ModelProvider struct {
	Name     string   `json:"name"`
	Endpoint string   `json:"endpoint"`
	APIKey   string   `json:"api_key,omitempty"`
	Models   []string `json:"models"`
}

func (c *MCPConfig) FindProvider(name string) *ModelProvider {
	for i := range c.ModelProviders {
		if c.ModelProviders[i].Name == name {
			return &c.ModelProviders[i]
		}
	}
	return nil
}

func (c *MCPConfig) SelectedProvider() *ModelProvider {
	if p := c.FindProvider(c.Provider); p != nil {
		return p
	}
	if len(c.ModelProviders) > 0 {
		return &c.ModelProviders[0]
	}
	return nil
}

func (c *MCPConfig) ResolveModel() (endpoint, apiKey, modelName string) {
	p := c.SelectedProvider()
	if p == nil {
		return "", "", ""
	}
	endpoint = p.Endpoint
	apiKey = p.APIKey
	modelName = c.Model
	found := false
	for _, m := range p.Models {
		if m == modelName {
			found = true
			break
		}
	}
	if !found {
		if len(p.Models) > 0 {
			modelName = p.Models[0]
		} else {
			modelName = ""
		}
	}
	return endpoint, apiKey, modelName
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
