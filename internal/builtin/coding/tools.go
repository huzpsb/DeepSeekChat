package coding

import "hschat/internal/model"

func (p *Provider) Tools() []model.ToolDef {
	var tools []model.ToolDef

	for name, tool := range p.shellTools {
		desc := tool.Description
		if p.rawShell != nil && p.rawShell.Enabled {
			desc += " (" + p.rawShell.Shell[0] + ")"
		}
		tools = append(tools, model.ToolDef{
			Name:        name,
			Description: desc,
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		})
	}

	if p.rawShell != nil && p.rawShell.Enabled {
		tools = append(tools, model.ToolDef{
			Name:        "run",
			Description: "Run a shell command (" + p.rawShell.Shell[0] + ")",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "The command to execute",
					},
				},
				"required": []string{"command"},
			},
		})
	}

	return tools
}

func (p *Provider) CallTool(name string, args map[string]any) (*model.ToolResult, error) {
	var result string

	if name == "run" && p.rawShell != nil && p.rawShell.Enabled {
		cmd, _ := args["command"].(string)
		if cmd == "" {
			cmd = "echo 'no command provided'"
		}
		tool := model.ShellTool{
			Command: cmd,
			Timeout: 120,
		}
		result = p.runShellTool(tool)
	} else if tool, ok := p.shellTools[name]; ok {
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
