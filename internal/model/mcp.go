package model

import "fmt"

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
	// CheckPicture selects the vision sub-agent used by the Coding
	// check_picture tool. Endpoint and API key are NOT stored here — they
	// are resolved by provider name against ModelProviders, so the
	// sub-agent shares the main registry. Empty Provider means the
	// "deepseek" provider, empty Model that provider's first model
	// (deepseek-flash in the default registry). Serialized even when
	// empty so users discover the knob in config.json.
	CheckPicture CheckPictureConfig `json:"check_picture"`
	// AuthPassword optionally protects the whole HTTP surface; clients
	// authenticate through the PoW-gated /api/login flow and its cookie
	// (Basic Auth was removed — a pow-free guessing oracle). Empty (the
	// default) means no auth at all. Serialized even when empty so users
	// discover the knob in config.json.
	AuthPassword string `json:"auth_password"`
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

type CheckPictureConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// DefaultCheckPictureProvider is the registry name ResolveCheckPicture
// falls back to when no check_picture provider is configured.
const DefaultCheckPictureProvider = "deepseek"

// ResolveCheckPicture resolves the check_picture vision sub-agent against
// the ModelProviders registry, mirroring ResolveModel: an empty provider
// falls back to "deepseek", an unconfigured or stale model to the
// provider's first model. An error is returned when the provider itself
// cannot be resolved — the caller must surface it, not silently send the
// picture somewhere else.
func (c *MCPConfig) ResolveCheckPicture() (endpoint, apiKey, modelName string, err error) {
	providerName := c.CheckPicture.Provider
	if providerName == "" {
		providerName = DefaultCheckPictureProvider
	}
	p := c.FindProvider(providerName)
	if p == nil {
		return "", "", "", fmt.Errorf("check_picture provider %q not found in model_providers", providerName)
	}
	modelName = c.CheckPicture.Model
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
	return p.Endpoint, p.APIKey, modelName, nil
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
	// ArgAliases declares argument auto-fix rules as data: canonical field
	// name -> alias names. ExecuteTool renames an alias key in place when
	// the canonical key is absent (see mcp.AutoFixArgs). Not serialized:
	// it is a server-side concern and must never reach the wire.
	ArgAliases map[string][]string `json:"-"`
}

type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}
