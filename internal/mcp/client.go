package mcp

import (
	"context"

	"hschat/internal/model"
)

type Client interface {
	Initialize() error
	ListTools() ([]model.ToolDef, error)
	CallTool(ctx context.Context, name string, arguments map[string]any) (*model.ToolResult, error)
	Close() error
	Name() string
}
