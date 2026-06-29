package askuser

import "hschat/internal/model"

type Provider struct{}

func New() *Provider {
	return &Provider{}
}

func (p *Provider) Name() string {
	return "AskUser"
}

func (p *Provider) Initialize(configPath string) error {
	return nil
}

func (p *Provider) Tools() []model.ToolDef {
	return []model.ToolDef{
		{
			Name:        "ask_user",
			Description: "Ask the user for information that is required to continue.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question": map[string]any{
						"type":        "string",
						"description": "The question to ask the user.",
					},
				},
				"required": []string{"question"},
			},
		},
	}
}

func (p *Provider) CallTool(name string, args map[string]any) (*model.ToolResult, error) {
	return &model.ToolResult{
		Content: []model.ToolContent{
			{Type: "text", Text: "Error: ask_user must be handled by the frontend."},
		},
		IsError: true,
	}, nil
}

func (p *Provider) Close() error {
	return nil
}
