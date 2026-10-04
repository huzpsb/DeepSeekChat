package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"hschat/internal/argfix"
	"hschat/internal/builtin"
	"hschat/internal/builtin/askuser"
	"hschat/internal/builtin/coding"
	"hschat/internal/builtin/sandbox"
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
	sandboxProvider *sandbox.Provider
	codingProvider  *coding.Provider
	SkipAskUser     bool
	ApproveAll      bool
}

func NewManager() *Manager {
	return &Manager{
		config:   &model.MCPConfig{},
		clients:  make(map[string]Client),
		allTools: make(map[string][]model.ToolDef),
	}
}

// Config returns a copy of the current configuration. Slice fields share
// their backing arrays with the live config, so the result must be treated
// as read-only (all writers replace slices wholesale, never mutate in
// place, so the copy is race-safe).
func (m *Manager) Config() model.MCPConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return *m.config
}

// UpdateConfig applies fn to the live config under the write lock and
// persists the result. If fn returns an error, nothing is mutated or
// saved by the Manager beyond what fn itself did before failing.
func (m *Manager) UpdateConfig(fn func(*model.MCPConfig) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := fn(m.config); err != nil {
		return err
	}
	return storage.SaveConfig(m.config)
}

func (m *Manager) LoadAndConnect() error {
	cfg, err := storage.LoadConfig()
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.config = cfg
	m.mu.Unlock()

	for _, srv := range m.config.MCPServers {
		if err := m.connectServer(srv); err != nil {
			log.Printf("MCP [%s] connection failed: %v", srv.Name, err)
		}
	}

	sandboxCfg := m.config.Sandbox
	rootDir := sandbox.ResolveRootDir(sandboxCfg.DefaultRootDir())

	sp := sandbox.New(&sandboxCfg)
	if err := m.registerBuiltin(sp); err != nil {
		log.Printf("Builtin [Sandbox] init failed: %v", err)
	}
	m.sandboxProvider = sp.(*sandbox.Provider)

	if !m.SkipAskUser {
		if err := m.registerBuiltin(askuser.New()); err != nil {
			log.Printf("Builtin [AskUser] init failed: %v", err)
		}
	}

	if m.config.EnableCodingTools {
		cp := coding.New(rootDir)
		if err := m.registerBuiltin(cp); err != nil {
			log.Printf("Builtin [Coding] init failed: %v", err)
		}
		m.codingProvider = cp.(*coding.Provider)
		cfg, err := storage.LoadCodingConfig()
		if err == nil && cfg != nil && cfg.RawShell != nil && cfg.RawShell.Enabled && m.sandboxProvider != nil {
			m.sandboxProvider.SetSandboxDisabled(true)
		}
	}

	m.reconcileTools()
	return nil
}

// SetRootDir switches the sandbox root directory at runtime for both the
// Sandbox and Coding builtin providers.
func (m *Manager) SetRootDir(dir string) error {
	m.mu.Lock()
	sp := m.sandboxProvider
	cp := m.codingProvider
	m.mu.Unlock()

	if sp != nil {
		if err := sp.SetRootDir(dir); err != nil {
			return err
		}
	}
	if cp != nil {
		cp.SetRootDir(dir)
	}
	return nil
}

func (m *Manager) registerBuiltin(p builtin.Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := p.Name()
	if _, exists := m.clients[name]; exists {
		return fmt.Errorf("builtin with name '%s' already exists", name)
	}

	if err := p.Initialize(""); err != nil {
		return fmt.Errorf("failed to initialize builtin '%s': %w", name, err)
	}

	client := builtin.AdaptClient(p)
	tools := p.Tools()
	if len(tools) == 0 {
		return fmt.Errorf("builtin '%s' returned empty tool list", name)
	}

	m.clients[name] = client
	m.allTools[name] = tools
	m.warnBareNameConflicts(name)
	log.Printf("Builtin [%s] loaded with %d tools", name, len(tools))
	return nil
}

func (m *Manager) Reload() error {
	m.mu.Lock()
	for name, c := range m.clients {
		c.Close()
		delete(m.clients, name)
	}
	m.allTools = make(map[string][]model.ToolDef)
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
	case "streamable":
		client = NewStreamableClient(srv.Name, srv.URL)
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
	m.warnBareNameConflicts(srv.Name)
	log.Printf("MCP [%s] connected with %d tools", srv.Name, len(tools))
	return nil
}

// warnBareNameConflicts logs every tool of the just-registered server whose
// bare name is also exposed by another connected MCP. The LLM only ever
// sees bare tool names, so two same-named tools cannot coexist in one
// tools array: GetAllowedTools deterministically keeps the lowest-named
// MCP's copy (see resolveBareTool) and shadows the other. We hard-fail on
// nothing here — a conflicting server is still fully usable via its
// "MCP::tool" full names — but the shadowing must be loud, not silent.
// Callers must hold m.mu.
func (m *Manager) warnBareNameConflicts(name string) {
	for _, t := range m.allTools[name] {
		for _, other := range sortedToolServers(m.allTools) {
			if other == name {
				continue
			}
			if !hasBareTool(m.allTools[other], t.Name) {
				continue
			}
			winner, loser := other, name
			if name < other {
				winner, loser = name, other
			}
			log.Printf("MCP tool name conflict: '%s' exists on both [%s] and [%s]; "+
				"[%s::%s] wins by sorted-name priority, [%s::%s] is shadowed "+
				"(not exposed to the LLM by bare name '%s')",
				t.Name, name, other, winner, t.Name, loser, t.Name, t.Name)
		}
	}
}

func hasBareTool(tools []model.ToolDef, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

func (m *Manager) reconcileTools() {
	m.mu.Lock()
	defer m.mu.Unlock()

	removed := m.removeConflictingApproved()
	if removed > 0 {
		storage.SaveConfig(m.config)
	}

	var cleanedApproved []string
	for _, fullName := range m.config.ApprovedTools {
		if isAskUserTool(fullName) {
			removed++
			continue
		}
		mcpName, toolName := SplitToolName(fullName)
		if m.isToolAvailable(mcpName, toolName) || !m.isMCPConnected(mcpName) {
			cleanedApproved = append(cleanedApproved, fullName)
		}
	}
	m.config.ApprovedTools = cleanedApproved

	var cleanedManual []string
	for _, fullName := range m.config.ManuallyApprovedTools {
		mcpName, toolName := SplitToolName(fullName)
		if m.isToolAvailable(mcpName, toolName) || !m.isMCPConnected(mcpName) {
			cleanedManual = append(cleanedManual, fullName)
		}
	}
	m.config.ManuallyApprovedTools = cleanedManual

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
	// Iterate MCP names in sorted order: Go map iteration is re-randomized
	// on every range, so iterating m.allTools directly would shuffle this
	// list per call (and per restart).
	for _, mcpName := range sortedToolServers(m.allTools) {
		tools := m.allTools[mcpName]
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
		mcpName, toolName := SplitToolName(t)
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
		mcpName, toolName := SplitToolName(t)
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

	if mcpName == "AskUser" && toolName == "ask_user" && status == "approved" {
		return fmt.Errorf("ask_user cannot be approved")
	}

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

func isAskUserTool(fullName string) bool {
	mcpName, toolName := SplitToolName(fullName)
	return mcpName == "AskUser" && toolName == "ask_user"
}

func (m *Manager) IsToolApproved(toolFullName string) (approved bool, manuallyApproved bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// LLM 只能看到裸工具名（见 GetAllowedTools），所以除了精确匹配
	// "MCP::tool" 外，裸名输入还要做 "::tool" 后缀匹配。
	bare := !strings.Contains(toolFullName, "::")
	match := func(list []string) bool {
		for _, t := range list {
			if t == toolFullName || (bare && strings.HasSuffix(t, "::"+toolFullName)) {
				return true
			}
		}
		return false
	}

	if match(m.config.ApprovedTools) {
		return true, false
	}
	if match(m.config.ManuallyApprovedTools) {
		if m.ApproveAll {
			// headless 模式（CLI runner）：无法交互确认，manual 升级为自动批准
			return true, false
		}
		return false, true
	}
	return false, false
}

func (m *Manager) IsToolApprovedByName(mcpName, toolName string) (approved bool, manuallyApproved bool) {
	return m.IsToolApproved(mcpName + "::" + toolName)
}

func (m *Manager) ToolExists(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mcpName, toolName := SplitToolName(name)
	if toolName != "" {
		return m.isToolAvailable(mcpName, toolName)
	}
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

	// The returned slice is serialized verbatim as the tools array of every
	// LLM request. Providers with prefix-based prompt caching (DeepSeek
	// Context Caching, Kimi, ...) match the cache on the serialized request
	// prefix, and Go map iteration is re-randomized on every range — so
	// iterating m.allTools directly would reshuffle the tools array per
	// request and turn every follow-up turn (and every restart) into a
	// cache miss, re-billing the full input prefix. Sorted MCP names keep
	// the array byte-identical across calls, reloads and restarts.
	//
	// Bare names must also be unique in the array: the LLM only sees bare
	// tool names, so two MCPs exposing the same name would leave the tool
	// call parser with no way to tell them apart. On a collision the
	// lowest-named MCP wins (matching resolveBareTool) and the other def
	// is deterministically shadowed — never silently sent as a duplicate.
	seen := make(map[string]bool)
	var result []model.ToolDef
	for _, mcpName := range sortedToolServers(m.allTools) {
		for _, tool := range m.allTools[mcpName] {
			fullName := mcpName + "::" + tool.Name
			if !(approvedMap[fullName] || manualMap[fullName]) {
				continue
			}
			if seen[tool.Name] {
				continue // shadowed by a lower-named MCP's same-named tool
			}
			seen[tool.Name] = true
			result = append(result, tool)
		}
	}
	return result
}

// resolveBareTool resolves a bare tool name (no "::") to its owning MCP and
// tool def, deterministically. Callers must hold m.mu.
//
// Same-name tools on different MCPs cannot both be exposed to the LLM (it
// sees bare names only), so GetAllowedTools deduplicates by sorted
// MCP-name priority. This resolver applies the same rule so a bare call
// always dispatches to the exact tool the model saw:
//
//  1. the lowest-named MCP whose same-named tool is approved or manually
//     approved wins;
//  2. if no candidate is allowed (approval changed after the request, or a
//     non-gated caller such as a unit test), the lowest-named MCP that
//     merely has the tool wins — still deterministic, never map-order.
func (m *Manager) resolveBareTool(bare string) (mcpName string, def *model.ToolDef) {
	allowed := make(map[string]bool)
	for _, t := range m.config.ApprovedTools {
		allowed[t] = true
	}
	for _, t := range m.config.ManuallyApprovedTools {
		allowed[t] = true
	}

	var fbName string
	var fbDef *model.ToolDef
	for _, name := range sortedToolServers(m.allTools) {
		for _, t := range m.allTools[name] {
			if t.Name != bare {
				continue
			}
			td := t // copy: must stay valid after the lock is released
			if allowed[name+"::"+bare] {
				return name, &td
			}
			if fbDef == nil {
				fbName, fbDef = name, &td
			}
		}
	}
	return fbName, fbDef
}

// sortedToolServers returns the keys of a tools-by-server map in sorted
// order, so any consumer that leaks the order into output (UI lists, LLM
// request bodies) is deterministic. Registration order is NOT a substitute:
// it depends on connect timing (a server that failed at startup and only
// connects on Reload would jump position).
func sortedToolServers(all map[string][]model.ToolDef) []string {
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (m *Manager) GetToolDef(name string) *model.ToolDef {
	mcpName, toolName := SplitToolName(name)
	m.mu.RLock()
	defer m.mu.RUnlock()
	if toolName != "" {
		if tools, ok := m.allTools[mcpName]; ok {
			for _, t := range tools {
				if t.Name == toolName {
					return &t
				}
			}
		}
		return nil
	}
	// Bare name: same deterministic resolution as ExecuteTool so schema
	// validation (arg auto-fix, required-args checks) looks at the exact
	// tool a bare call would dispatch to.
	_, def := m.resolveBareTool(name)
	return def
}

func (m *Manager) ExecuteTool(ctx context.Context, fullName string, arguments string) (*model.ToolResult, error) {
	mcpName, toolName := SplitToolName(fullName)

	// Resolve the tool definition (a copy, so it stays valid after the
	// lock is released) alongside the MCP name.
	var toolDef *model.ToolDef
	m.mu.RLock()
	if toolName == "" {
		// Bare tool name (the form the LLM calls): resolve the owning MCP
		// deterministically — see resolveBareTool.
		name, def := m.resolveBareTool(fullName)
		if def != nil {
			mcpName, toolName, toolDef = name, fullName, def
		}
	} else {
		for _, t := range m.allTools[mcpName] {
			if t.Name == toolName {
				td := t
				toolDef = &td
				break
			}
		}
	}
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

	// Protocol-layer auto-fix: rename aliased argument keys and coerce
	// value types in place (driven by the tool's declared ArgAliases and
	// InputSchema) before dispatch.
	if fixes := argfix.FixArgs(toolDef, args); len(fixes) > 0 {
		log.Printf("MCP [%s] auto-fixed args for '%s': %v", mcpName, toolName, fixes)
	}

	return client.CallTool(ctx, toolName, args)
}

func SplitToolName(fullName string) (mcpName, toolName string) {
	parts := strings.SplitN(fullName, "::", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return fullName, ""
}
