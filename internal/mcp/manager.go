package mcp

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"hschat/internal/model"
	"hschat/internal/storage"
)

type ToolStatus struct {
	MCPName     string `json:"mcp_name"`
	ToolName    string `json:"tool_name"`
	Status      string `json:"status"`
	Available   bool   `json:"available"`
	InputSchema any    `json:"input_schema,omitempty"`
}

type Manager struct {
	mu              sync.RWMutex
	config          *model.MCPConfig
	clients         map[string]Client
	allTools        map[string][]model.ToolDef
	unapprovedTools []string
}

func NewManager() *Manager {
	return &Manager{
		clients:         make(map[string]Client),
		allTools:        make(map[string][]model.ToolDef),
		unapprovedTools: []string{},
	}
}

func (m *Manager) LoadAndConnect() error {
	cfg, err := storage.LoadConfig()
	if err != nil {
		return err
	}
	m.config = cfg

	for _, srv := range m.config.MCPServers {
		if err := m.connectServer(srv); err != nil {
			log.Printf("MCP [%s] connection failed: %v", srv.Name, err)
		}
	}

	m.reconcileTools()
	return nil
}

func (m *Manager) Reload() error {
	m.mu.Lock()
	for name, c := range m.clients {
		c.Close()
		delete(m.clients, name)
	}
	m.allTools = make(map[string][]model.ToolDef)
	m.unapprovedTools = []string{}
	m.mu.Unlock()

	return m.LoadAndConnect()
}

func (m *Manager) connectServer(srv model.MCPServer) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clients[srv.Name]; exists {
		return fmt.Errorf("MCP with name '%s' already exists", srv.Name)
	}

	var client Client
	switch srv.Type {
	case "sse":
		client = NewSSEClient(srv.Name, srv.URL)
	case "stdio":
		client = NewStdioClient(srv.Name, srv.Command)
	default:
		return fmt.Errorf("unknown MCP type: %s", srv.Type)
	}

	if err := client.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize %s MCP '%s': %w", srv.Type, srv.Name, err)
	}

	tools, err := client.ListTools()
	if err != nil {
		client.Close()
		return fmt.Errorf("failed to list tools for '%s': %w", srv.Name, err)
	}

	if len(tools) == 0 {
		client.Close()
		return fmt.Errorf("MCP '%s' returned empty tool list", srv.Name)
	}

	m.clients[srv.Name] = client
	m.allTools[srv.Name] = tools
	log.Printf("MCP [%s] connected with %d tools", srv.Name, len(tools))
	return nil
}

func (m *Manager) reconcileTools() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.unapprovedTools = nil

	removed := m.removeConflictingApproved()
	if removed > 0 {
		storage.SaveConfig(m.config)
	}

	var cleanedApproved []string
	for _, fullName := range m.config.ApprovedTools {
		mcpName, toolName := splitToolName(fullName)
		if m.isToolAvailable(mcpName, toolName) || !m.isMCPConnected(mcpName) {
			cleanedApproved = append(cleanedApproved, fullName)
		}
	}
	m.config.ApprovedTools = cleanedApproved

	var cleanedManual []string
	for _, fullName := range m.config.ManuallyApprovedTools {
		mcpName, toolName := splitToolName(fullName)
		if m.isToolAvailable(mcpName, toolName) || !m.isMCPConnected(mcpName) {
			cleanedManual = append(cleanedManual, fullName)
		}
	}
	m.config.ManuallyApprovedTools = cleanedManual

	existingApproved := make(map[string]bool)
	for _, t := range m.config.ApprovedTools {
		existingApproved[t] = true
	}
	existingManual := make(map[string]bool)
	for _, t := range m.config.ManuallyApprovedTools {
		existingManual[t] = true
	}

	for mcpName, tools := range m.allTools {
		for _, tool := range tools {
			fullName := mcpName + "::" + tool.Name
			if !existingApproved[fullName] && !existingManual[fullName] {
				m.unapprovedTools = append(m.unapprovedTools, fullName)
			}
		}
	}

	storage.SaveConfig(m.config)
}

func (m *Manager) removeConflictingApproved() int {
	manual := make(map[string]bool)
	for _, t := range m.config.ManuallyApprovedTools {
		manual[t] = true
	}
	var filtered []string
	removed := 0
	for _, t := range m.config.ApprovedTools {
		if manual[t] {
			removed++
		} else {
			filtered = append(filtered, t)
		}
	}
	m.config.ApprovedTools = filtered
	return removed
}

func (m *Manager) isToolAvailable(mcpName, toolName string) bool {
	tools, ok := m.allTools[mcpName]
	if !ok {
		return false
	}
	for _, t := range tools {
		if t.Name == toolName {
			return true
		}
	}
	return false
}

func (m *Manager) isMCPConnected(name string) bool {
	_, ok := m.clients[name]
	return ok
}

func (m *Manager) GetTools() []ToolStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	approvedMap := make(map[string]bool)
	for _, t := range m.config.ApprovedTools {
		approvedMap[t] = true
	}
	manualMap := make(map[string]bool)
	for _, t := range m.config.ManuallyApprovedTools {
		manualMap[t] = true
	}

	result := make([]ToolStatus, 0)
	for mcpName, tools := range m.allTools {
		for _, tool := range tools {
			fullName := mcpName + "::" + tool.Name
			status := "unapproved"
			if approvedMap[fullName] {
				status = "approved"
			} else if manualMap[fullName] {
				status = "manually_approved"
			}
			result = append(result, ToolStatus{
				MCPName:     mcpName,
				ToolName:    tool.Name,
				Status:      status,
				Available:   true,
				InputSchema: tool.InputSchema,
			})
		}
	}

	for _, t := range m.config.ApprovedTools {
		mcpName, toolName := splitToolName(t)
		if !m.isToolAvailable(mcpName, toolName) && m.isMCPConnected(mcpName) {
			continue
		}
		found := false
		for _, r := range result {
			if r.MCPName == mcpName && r.ToolName == toolName {
				found = true
				break
			}
		}
		if !found {
			result = append(result, ToolStatus{
				MCPName:   mcpName,
				ToolName:  toolName,
				Status:    "approved",
				Available: false,
			})
		}
	}
	for _, t := range m.config.ManuallyApprovedTools {
		mcpName, toolName := splitToolName(t)
		found := false
		for _, r := range result {
			if r.MCPName == mcpName && r.ToolName == toolName {
				found = true
				break
			}
		}
		if !found {
			result = append(result, ToolStatus{
				MCPName:   mcpName,
				ToolName:  toolName,
				Status:    "manually_approved",
				Available: false,
			})
		}
	}

	return result
}

func (m *Manager) SetToolStatus(mcpName, toolName, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullName := mcpName + "::" + toolName

	var newApproved []string
	for _, t := range m.config.ApprovedTools {
		if t != fullName {
			newApproved = append(newApproved, t)
		}
	}
	var newManual []string
	for _, t := range m.config.ManuallyApprovedTools {
		if t != fullName {
			newManual = append(newManual, t)
		}
	}

	switch status {
	case "approved":
		newApproved = append(newApproved, fullName)
	case "manually_approved":
		newManual = append(newManual, fullName)
	case "unapproved":
	default:
		return fmt.Errorf("invalid status: %s", status)
	}

	conflicting := make(map[string]bool)
	for _, t := range newManual {
		conflicting[t] = true
	}
	var finalApproved []string
	for _, t := range newApproved {
		if conflicting[t] {
			continue
		}
		finalApproved = append(finalApproved, t)
	}

	m.config.ApprovedTools = finalApproved
	m.config.ManuallyApprovedTools = newManual

	return storage.SaveConfig(m.config)
}

func (m *Manager) IsToolApproved(toolFullName string) (approved bool, manuallyApproved bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, t := range m.config.ApprovedTools {
		if t == toolFullName {
			return true, false
		}
	}
	for _, t := range m.config.ManuallyApprovedTools {
		if t == toolFullName {
			return false, true
		}
	}

	if !strings.Contains(toolFullName, "::") {
		for _, t := range m.config.ApprovedTools {
			if strings.HasSuffix(t, "::"+toolFullName) {
				return true, false
			}
		}
		for _, t := range m.config.ManuallyApprovedTools {
			if strings.HasSuffix(t, "::"+toolFullName) {
				return false, true
			}
		}
	}

	return false, false
}

func (m *Manager) IsToolApprovedByName(mcpName, toolName string) (approved bool, manuallyApproved bool) {
	return m.IsToolApproved(mcpName + "::" + toolName)
}

func (m *Manager) ToolExists(name string) bool {
	mcpName, toolName := splitToolName(name)
	if toolName != "" {
		return m.isToolAvailable(mcpName, toolName)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, tools := range m.allTools {
		for _, t := range tools {
			if t.Name == name {
				return true
			}
		}
	}
	return false
}

func (m *Manager) GetAllowedTools() []model.ToolDef {
	m.mu.RLock()
	defer m.mu.RUnlock()

	approvedMap := make(map[string]bool)
	for _, t := range m.config.ApprovedTools {
		approvedMap[t] = true
	}
	manualMap := make(map[string]bool)
	for _, t := range m.config.ManuallyApprovedTools {
		manualMap[t] = true
	}

	var result []model.ToolDef
	for mcpName, tools := range m.allTools {
		for _, tool := range tools {
			fullName := mcpName + "::" + tool.Name
			if approvedMap[fullName] || manualMap[fullName] {
				result = append(result, tool)
			}
		}
	}
	return result
}

func (m *Manager) ExecuteTool(fullName string, arguments string) (*model.ToolResult, error) {
	mcpName, toolName := splitToolName(fullName)

	if toolName == "" {
		m.mu.RLock()
		for name, tools := range m.allTools {
			for _, t := range tools {
				if t.Name == fullName {
					mcpName = name
					toolName = fullName
				}
			}
		}
		m.mu.RUnlock()
	}

	m.mu.RLock()
	client, ok := m.clients[mcpName]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("MCP '%s' not connected", mcpName)
	}

	var args map[string]any
	if arguments != "" {
		json.Unmarshal([]byte(arguments), &args)
	}
	if args == nil {
		args = map[string]any{}
	}

	return client.CallTool(toolName, args)
}

func splitToolName(fullName string) (mcpName, toolName string) {
	parts := strings.SplitN(fullName, "::", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return fullName, ""
}
