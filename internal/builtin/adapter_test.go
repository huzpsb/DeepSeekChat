package builtin

import (
	"testing"

	"hschat/internal/model"
)

type mockProvider struct {
	name        string
	initErr     error
	tools       []model.ToolDef
	callErr     error
	callResult  *model.ToolResult
	closeErr    error
	initialized bool
	closed      bool
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Initialize(_ string) error {
	m.initialized = true
	return m.initErr
}
func (m *mockProvider) Tools() []model.ToolDef { return m.tools }
func (m *mockProvider) CallTool(_ string, _ map[string]any) (*model.ToolResult, error) {
	return m.callResult, m.callErr
}
func (m *mockProvider) Close() error {
	m.closed = true
	return m.closeErr
}

func TestAdaptClient_Name(t *testing.T) {
	p := &mockProvider{name: "TestMCP"}
	a := AdaptClient(p)
	if a.Name() != "TestMCP" {
		t.Errorf("expected 'TestMCP', got '%s'", a.Name())
	}
}

func TestAdaptClient_Type(t *testing.T) {
	a := AdaptClient(&mockProvider{})
	if a.Type() != "builtin" {
		t.Errorf("expected 'builtin', got '%s'", a.Type())
	}
}

func TestAdaptClient_IsConnected(t *testing.T) {
	a := AdaptClient(&mockProvider{})
	if !a.IsConnected() {
		t.Errorf("expected true")
	}
}

func TestAdaptClient_Initialize(t *testing.T) {
	a := AdaptClient(&mockProvider{})
	if err := a.Initialize(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestAdaptClient_Close(t *testing.T) {
	p := &mockProvider{}
	a := AdaptClient(p)
	if err := a.Close(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if !p.closed {
		t.Errorf("expected provider.Close to be called")
	}
}

func TestAdaptClient_CloseError(t *testing.T) {
	p := &mockProvider{closeErr: assertErr()}
	a := AdaptClient(p)
	if err := a.Close(); err == nil {
		t.Errorf("expected error")
	}
}

func TestAdaptClient_ListTools(t *testing.T) {
	expected := []model.ToolDef{
		{Name: "tool_a", Description: "desc a"},
		{Name: "tool_b", Description: "desc b"},
	}
	p := &mockProvider{tools: expected}
	a := AdaptClient(p)

	tools, err := a.ListTools()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "tool_a" {
		t.Errorf("expected 'tool_a', got '%s'", tools[0].Name)
	}
	if tools[1].Name != "tool_b" {
		t.Errorf("expected 'tool_b', got '%s'", tools[1].Name)
	}
}

func TestAdaptClient_CallTool(t *testing.T) {
	expected := &model.ToolResult{
		Content: []model.ToolContent{{Type: "text", Text: "result"}},
	}
	p := &mockProvider{callResult: expected}
	a := AdaptClient(p)

	result, err := a.CallTool("any", map[string]any{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != expected {
		t.Errorf("expected result to be returned")
	}
}

func TestAdaptClient_CallToolError(t *testing.T) {
	p := &mockProvider{callErr: assertErr()}
	a := AdaptClient(p)

	_, err := a.CallTool("any", nil)
	if err == nil {
		t.Errorf("expected error")
	}
}

func assertErr() error { return &testError{} }

type testError struct{}

func (e *testError) Error() string { return "test error" }
