package builtin

import (
	"context"

	"hschat/internal/model"
)

type Provider interface {
	Name() string
	Initialize(configPath string) error
	Tools() []model.ToolDef
	CallTool(ctx context.Context, name string, args map[string]any) (*model.ToolResult, error)
	Close() error
}
