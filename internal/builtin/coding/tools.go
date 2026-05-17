package coding

import "hschat/internal/model"

func (p *Provider) Tools() []model.ToolDef {
	var tools []model.ToolDef

	for name, tool := range p.shellTools {
		tools = append(tools, model.ToolDef{
			Name:        name,
			Description: tool.Description,
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		})
	}

	return tools
}

func (p *Provider) CallTool(name string, args map[string]any) (*model.ToolResult, error) {
	var result string

	if tool, ok := p.shellTools[name]; ok {
		result = p.runShellTool(tool)
	} else {
		result = "Error: Unknown tool"
	}

	return &model.ToolResult{
		Content: []model.ToolContent{
			{Type: "text", Text: result},
		},
	}, nil
}
