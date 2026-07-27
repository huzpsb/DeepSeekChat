package builtin

import (
	"context"

	"hschat/internal/model"
)

type clientAdapter struct {
	p Provider
}

func AdaptClient(p Provider) *clientAdapter {
	return &clientAdapter{p: p}
}

func (a *clientAdapter) Initialize() error {
	return nil
}

func (a *clientAdapter) ListTools() ([]model.ToolDef, error) {
	return a.p.Tools(), nil
}

func (a *clientAdapter) CallTool(ctx context.Context, name string, args map[string]any) (*model.ToolResult, error) {
	return a.p.CallTool(ctx, name, args)
}

func (a *clientAdapter) Close() error {
	return a.p.Close()
}

func (a *clientAdapter) Name() string {
	return a.p.Name()
}

func (a *clientAdapter) Type() string {
	return "builtin"
}

func (a *clientAdapter) IsConnected() bool {
	return true
}
