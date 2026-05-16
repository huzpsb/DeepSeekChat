package builtin

import "hschat/internal/model"

type Provider interface {
	Name() string
	Initialize(configPath string) error
	Tools() []model.ToolDef
	CallTool(name string, args map[string]any) (*model.ToolResult, error)
	Close() error
}
