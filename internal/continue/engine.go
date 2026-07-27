package cont

import (
	"context"
	"encoding/json"
	"fmt"

	"hschat/internal/deepseek"
	"hschat/internal/log"
	"hschat/internal/model"
)

type Engine struct {
	deepseekClient        *deepseek.Client
	mode                  string
	toolExecutor          ToolExecutor
	saveFunc              func()
	ContinueOnInvalidArgs bool
}

type ToolExecutor interface {
	IsToolApproved(fullName string) (approved, manuallyApproved bool)
	ToolExists(fullName string) bool
	ExecuteTool(fullName string, arguments string) (*model.ToolResult, error)
	GetAllowedTools() []model.ToolDef
	GetToolDef(name string) *model.ToolDef
}

type ContinueEvent struct {
	Type       string          `json:"type"`
	Content    string          `json:"content,omitempty"`
	ToolCall   *model.ToolCall `json:"tool_call,omitempty"`
	ToolResult *toolResultEvt  `json:"tool_result,omitempty"`
	Error      *ErrorDetail    `json:"error,omitempty"`
	Message    *model.Message  `json:"message,omitempty"`
}

type toolResultEvt struct {
	Message model.Message `json:"message"`
}

type ErrorDetail struct {
	Type   string   `json:"type"`
	Detail string   `json:"detail,omitempty"`
	IDs    []string `json:"ids,omitempty"`
}

func NewEngine(client *deepseek.Client, mode string, executor ToolExecutor, saveFunc func()) *Engine {
	return &Engine{
		deepseekClient: client,
		mode:           mode,
		toolExecutor:   executor,
		saveFunc:       saveFunc,
	}
}

func (e *Engine) SetMode(mode string) {
	e.mode = mode
}

func (e *Engine) Continue(ctx context.Context, chat *model.Chat, input string, autoContinue bool, emit func(ContinueEvent), interrupted func() bool) {
	logContinue("continue_start mode=%s input_len=%d auto_continue=%v messages=%d last=%s", e.mode, len(input), autoContinue, len(chat.Messages), describeLastMessage(chat))
	if input != "" {
		msg := model.Message{
			Role:         "user",
			Content:      input,
			SendToServer: true,
		}
		chat.Messages = append(chat.Messages, msg)
		logContinue("user_added input_len=%d messages=%d last=%s", len(input), len(chat.Messages), describeLastMessage(chat))
		emit(ContinueEvent{Type: "user_added", Content: input})
	}

	e.doContinue(ctx, chat, autoContinue, emit, interrupted)
	logContinue("continue_end messages=%d last=%s", len(chat.Messages), describeLastMessage(chat))
}

func (e *Engine) doContinue(ctx context.Context, chat *model.Chat, autoContinue bool, emit func(ContinueEvent), interrupted func() bool) {
	if len(chat.Messages) == 0 {
		logContinue("do_continue_return reason=empty_chat")
		return
	}
	if interrupted() {
		logContinue("do_continue_return reason=interrupted messages=%d last=%s", len(chat.Messages), describeLastMessage(chat))
		return
	}

	lastIdx := len(chat.Messages) - 1
	last := chat.Messages[lastIdx]
	logContinue("do_continue_route last_idx=%d last_role=%s auto_continue=%v last=%s", lastIdx, last.Role, autoContinue, describeMessage(lastIdx, last))

	switch last.Role {
	case "user", "system":
		logContinue("route_stream_deepseek from_role=%s", last.Role)
		if err := e.streamDeepSeek(ctx, chat, emit, interrupted); err != nil {
			logContinue("route_stream_deepseek_error err=%q", err.Error())
			return
		}
		if autoContinue && len(chat.Messages) > 0 && chat.Messages[len(chat.Messages)-1].Role != "user" {
			e.checkAutoContinue(ctx, chat, autoContinue, emit, interrupted)
		}

	case "assistant":
		logContinue("route_continue_assistant idx=%d tool_calls=%d", lastIdx, len(last.ToolCalls))
		e.continueAssistant(ctx, chat, autoContinue, lastIdx, emit, interrupted)

	case "tool":
		logContinue("route_continue_tool idx=%d tool_call_id=%q name=%q", lastIdx, last.ToolCallID, last.Name)
		e.continueTool(ctx, chat, autoContinue, lastIdx, emit, interrupted)

	default:
		logContinue("do_continue_return reason=unknown_role role=%q idx=%d", last.Role, lastIdx)
	}
}

func (e *Engine) continueAssistant(ctx context.Context, chat *model.Chat, autoContinue bool, idx int, emit func(ContinueEvent), interrupted func() bool) {
	if interrupted() {
		logContinue("continue_assistant_return reason=interrupted idx=%d", idx)
		return
	}

	msg := chat.Messages[idx]
	logContinue("continue_assistant_start idx=%d mode=%s tool_calls=%d approved=%v content_len=%d reasoning_len=%d", idx, e.mode, len(msg.ToolCalls), msg.Approved, len(msg.Content), len(msg.ReasoningContent))

	if len(msg.ToolCalls) == 0 {
		if e.mode == "sudo" {
			logContinue("continue_assistant_no_tools_sudo_stream idx=%d", idx)
			if err := e.streamDeepSeek(ctx, chat, emit, interrupted); err != nil {
				logContinue("continue_assistant_sudo_stream_error idx=%d err=%q", idx, err.Error())
				return
			}
			if autoContinue && len(chat.Messages) > 0 && chat.Messages[len(chat.Messages)-1].Role != "user" {
				e.checkAutoContinue(ctx, chat, autoContinue, emit, interrupted)
			}
		}
		logContinue("continue_assistant_return reason=no_tool_calls idx=%d mode=%s", idx, e.mode)
		return
	}

	invalidIDs := e.findInvalidToolCalls(msg)
	if len(invalidIDs) > 0 {
		logContinue("continue_assistant_invalid_tools idx=%d mode=%s invalid_ids=%v", idx, e.mode, invalidIDs)
		if e.mode == "readonly" {
			e.markInvalidAsNotFound(chat, msg, invalidIDs, emit)
			if err := e.streamDeepSeek(ctx, chat, emit, interrupted); err != nil {
				logContinue("continue_assistant_invalid_stream_error idx=%d err=%q", idx, err.Error())
				return
			}
			if autoContinue && len(chat.Messages) > 0 && chat.Messages[len(chat.Messages)-1].Role != "user" {
				e.checkAutoContinue(ctx, chat, autoContinue, emit, interrupted)
			}
			return
		}
		emit(ContinueEvent{
			Type: "error",
			Error: &ErrorDetail{
				Type:   "invalid_tool_calls",
				Detail: fmt.Sprintf("Invalid tool calls found: %v", invalidIDs),
				IDs:    invalidIDs,
			},
		})
		return
	}

	for _, tc := range msg.ToolCalls {
		if tc.Function.Name == "ask_user" && e.toolExecutor != nil && e.toolExecutor.ToolExists("ask_user") {
			logContinue("continue_assistant_return reason=ask_user idx=%d", idx)
			return
		}
	}

	allApproved, needsApproval := e.checkApproval(&msg)
	if msg.Approved {
		allApproved = true
		needsApproval = false
	}
	logContinue("continue_assistant_approval idx=%d all_approved=%v needs_approval=%v msg_approved=%v", idx, allApproved, needsApproval, msg.Approved)

	if allApproved && !needsApproval {
		e.executeToolCall(ctx, chat, autoContinue, idx, 0, emit, interrupted)
	} else if needsApproval {
		logContinue("continue_assistant_return reason=needs_approval idx=%d", idx)
		return
	} else {
		logContinue("continue_assistant_error reason=unapproved_state idx=%d", idx)
		emit(ContinueEvent{
			Type: "error",
			Error: &ErrorDetail{
				Type:   "unapproved_state",
				Detail: "Unexpected unapproved state",
			},
		})
	}
}

func (e *Engine) continueTool(ctx context.Context, chat *model.Chat, autoContinue bool, toolIdx int, emit func(ContinueEvent), interrupted func() bool) {
	if interrupted() {
		logContinue("continue_tool_return reason=interrupted tool_idx=%d", toolIdx)
		return
	}
	logContinue("continue_tool_start tool_idx=%d message=%s", toolIdx, describeMessage(toolIdx, chat.Messages[toolIdx]))

	origIdx := e.backtrackToAssistant(chat, toolIdx)
	if origIdx < 0 {
		logContinue("continue_tool_error reason=orphan_no_assistant tool_idx=%d", toolIdx)
		emit(ContinueEvent{
			Type: "error",
			Error: &ErrorDetail{
				Type:   "orphan_tool",
				Detail: "No originating assistant found for tool message",
			},
		})
		return
	}

	backtrack := chat.Messages[origIdx+1 : toolIdx+1]
	logContinue("continue_tool_backtrack orig_idx=%d tool_idx=%d backtrack_len=%d assistant_tool_calls=%d", origIdx, toolIdx, len(backtrack), len(chat.Messages[origIdx].ToolCalls))
	for i, m := range backtrack {
		if m.Role == "tool" {
			for _, other := range backtrack[i+1:] {
				if other.Role == "tool" && other.ToolCallID == m.ToolCallID {
					logContinue("continue_tool_error reason=duplicate_tool_id tool_call_id=%q", m.ToolCallID)
					emit(ContinueEvent{
						Type: "error",
						Error: &ErrorDetail{
							Type:   "duplicate_tool_id",
							Detail: fmt.Sprintf("Duplicate tool_call_id in backtrack: %s", m.ToolCallID),
							IDs:    []string{m.ToolCallID},
						},
					})
					return
				}
			}
		}
	}

	assistant := chat.Messages[origIdx]
	found := false
	for _, tc := range assistant.ToolCalls {
		if tc.ID == chat.Messages[toolIdx].ToolCallID {
			found = true
			break
		}
	}
	if !found {
		logContinue("continue_tool_error reason=tool_id_not_in_assistant tool_idx=%d tool_call_id=%q orig_idx=%d", toolIdx, chat.Messages[toolIdx].ToolCallID, origIdx)
		emit(ContinueEvent{
			Type: "error",
			Error: &ErrorDetail{
				Type:   "orphan_tool",
				Detail: fmt.Sprintf("tool_call_id %s not in originating assistant", chat.Messages[toolIdx].ToolCallID),
				IDs:    []string{chat.Messages[toolIdx].ToolCallID},
			},
		})
		return
	}

	invalidIDs := e.findInvalidToolCalls(assistant)
	if len(invalidIDs) > 0 {
		logContinue("continue_tool_invalid_tools orig_idx=%d invalid_ids=%v mode=%s", origIdx, invalidIDs, e.mode)
		if e.mode == "readonly" {
			e.markInvalidAsNotFound(chat, assistant, invalidIDs, emit)
			if err := e.streamDeepSeek(ctx, chat, emit, interrupted); err != nil {
				logContinue("continue_tool_invalid_stream_error orig_idx=%d err=%q", origIdx, err.Error())
				return
			}
			if autoContinue && len(chat.Messages) > 0 && chat.Messages[len(chat.Messages)-1].Role != "user" {
				e.checkAutoContinue(ctx, chat, autoContinue, emit, interrupted)
			}
			return
		}
		emit(ContinueEvent{
			Type: "error",
			Error: &ErrorDetail{
				Type:   "invalid_tool_calls",
				Detail: fmt.Sprintf("Invalid tool calls in assistant: %v", invalidIDs),
				IDs:    invalidIDs,
			},
		})
		return
	}

	executed := e.collectExecutedToolIDs(chat, origIdx)
	allApproved, needsApproval := e.checkApproval(&assistant)
	if assistant.Approved {
		allApproved = true
		needsApproval = false
	}
	logContinue("continue_tool_executed orig_idx=%d executed=%v all_approved=%v needs_approval=%v msg_approved=%v", origIdx, executed, allApproved, needsApproval, assistant.Approved)

	nextIdx := -1
	for i, tc := range assistant.ToolCalls {
		if _, done := executed[tc.ID]; !done {
			nextIdx = i
			break
		}
	}

	if nextIdx >= 0 {
		logContinue("continue_tool_next_tool orig_idx=%d next_tool_idx=%d tool_call_id=%q", origIdx, nextIdx, assistant.ToolCalls[nextIdx].ID)
		if allApproved && !needsApproval {
			e.executeToolCall(ctx, chat, autoContinue, origIdx, nextIdx, emit, interrupted)
		} else if needsApproval {
			logContinue("continue_tool_error reason=needs_approval orig_idx=%d", origIdx)
			emit(ContinueEvent{
				Type: "error",
				Error: &ErrorDetail{
					Type:   "unapproved_tools",
					Detail: "There are unapproved tool calls",
				},
			})
		} else {
			logContinue("continue_tool_error reason=unapproved_state orig_idx=%d", origIdx)
			emit(ContinueEvent{
				Type: "error",
				Error: &ErrorDetail{
					Type:   "unapproved_state",
					Detail: "Unexpected unapproved state",
				},
			})
		}
	} else {
		logContinue("continue_tool_all_done_stream orig_idx=%d tool_idx=%d", origIdx, toolIdx)
		if err := e.streamDeepSeek(ctx, chat, emit, interrupted); err != nil {
			logContinue("continue_tool_stream_error orig_idx=%d err=%q", origIdx, err.Error())
			return
		}
		if autoContinue && len(chat.Messages) > 0 && chat.Messages[len(chat.Messages)-1].Role != "user" {
			e.checkAutoContinue(ctx, chat, autoContinue, emit, interrupted)
		}
	}
}

func (e *Engine) executeToolCall(ctx context.Context, chat *model.Chat, autoContinue bool, assistantIdx int, toolCallIdx int, emit func(ContinueEvent), interrupted func() bool) {
	if interrupted() {
		logContinue("execute_tool_return reason=interrupted assistant_idx=%d tool_call_idx=%d", assistantIdx, toolCallIdx)
		return
	}

	assistant := chat.Messages[assistantIdx]
	if toolCallIdx >= len(assistant.ToolCalls) {
		logContinue("execute_tool_return reason=tool_call_idx_out_of_range assistant_idx=%d tool_call_idx=%d tool_calls=%d", assistantIdx, toolCallIdx, len(assistant.ToolCalls))
		return
	}
	tc := assistant.ToolCalls[toolCallIdx]
	logContinue("execute_tool_start assistant_idx=%d tool_call_idx=%d tool_call_id=%q name=%q args_len=%d", assistantIdx, toolCallIdx, tc.ID, tc.Function.Name, len(tc.Function.Arguments))

	emit(ContinueEvent{
		Type:     "tool_execute",
		ToolCall: &tc,
	})

	if e.toolExecutor != nil {
		argsErr := e.validateArgs(tc.Function.Name, tc.Function.Arguments)
		if argsErr != "" {
			logContinue("execute_tool_invalid_args tool_call_id=%q name=%q err=%q", tc.ID, tc.Function.Name, argsErr)
			if !e.ContinueOnInvalidArgs {
				emit(ContinueEvent{
					Type: "error",
					Error: &ErrorDetail{
						Type:   "invalid_args",
						Detail: argsErr,
					},
				})
				return
			}
			content := "Error: " + argsErr
			toolMsg := model.Message{
				Role:         "tool",
				ToolCallID:   tc.ID,
				Name:         tc.Function.Name,
				Content:      content,
				SendToServer: true,
			}
			chat.Messages = append(chat.Messages, toolMsg)
			emit(ContinueEvent{
				Type: "tool_result",
				ToolResult: &toolResultEvt{
					Message: toolMsg,
				},
			})
			if e.saveFunc != nil {
				e.saveFunc()
			}
			if autoContinue {
				e.checkAutoContinue(ctx, chat, autoContinue, emit, interrupted)
			}
			return
		}

		result, err := e.toolExecutor.ExecuteTool(tc.Function.Name, tc.Function.Arguments)
		var content string
		if err != nil {
			content = "Error executing tool: " + err.Error()
			logContinue("execute_tool_error tool_call_id=%q name=%q err=%q", tc.ID, tc.Function.Name, err.Error())
		} else if result != nil && len(result.Content) > 0 {
			content = result.Content[0].Text
		} else {
			content = "Tool executed with no output"
		}

		toolMsg := model.Message{
			Role:         "tool",
			ToolCallID:   tc.ID,
			Name:         tc.Function.Name,
			Content:      content,
			SendToServer: true,
		}
		chat.Messages = append(chat.Messages, toolMsg)
		logContinue("execute_tool_appended tool_call_id=%q name=%q result_len=%d messages=%d last=%s", tc.ID, tc.Function.Name, len(content), len(chat.Messages), describeLastMessage(chat))

		emit(ContinueEvent{
			Type: "tool_result",
			ToolResult: &toolResultEvt{
				Message: toolMsg,
			},
		})

		if e.saveFunc != nil {
			logContinue("execute_tool_save tool_call_id=%q", tc.ID)
			e.saveFunc()
		}

		if autoContinue {
			e.checkAutoContinue(ctx, chat, autoContinue, emit, interrupted)
		}
	}
}

func (e *Engine) streamDeepSeek(ctx context.Context, chat *model.Chat, emit func(ContinueEvent), interrupted func() bool) error {
	if interrupted() {
		logContinue("stream_deepseek_return reason=interrupted_before_start messages=%d last=%s", len(chat.Messages), describeLastMessage(chat))
		return nil
	}

	var tools []model.ToolDef
	if e.toolExecutor != nil {
		tools = e.toolExecutor.GetAllowedTools()
	}
	logContinue("stream_deepseek_start messages=%d tools=%d last=%s", len(chat.Messages), len(tools), describeLastMessage(chat))

	assistantIdx := len(chat.Messages)
	chat.Messages = append(chat.Messages, model.Message{
		Role:         "assistant",
		SendToServer: true,
	})
	logContinue("stream_deepseek_assistant_appended assistant_idx=%d messages=%d", assistantIdx, len(chat.Messages))

	var streamErr error
	err := e.deepseekClient.StreamChat(ctx, chat.Messages[:assistantIdx], tools, func(evt deepseek.StreamEvent) {
		switch evt.Type {
		case "delta":
			chat.Messages[assistantIdx].Content += evt.Content
			logContinue("deepseek_event type=delta assistant_idx=%d delta_len=%d content_len=%d", assistantIdx, len(evt.Content), len(chat.Messages[assistantIdx].Content))
			emit(ContinueEvent{Type: "delta", Content: evt.Content})
		case "reasoning_delta":
			chat.Messages[assistantIdx].ReasoningContent += evt.Content
			logContinue("deepseek_event type=reasoning_delta assistant_idx=%d delta_len=%d reasoning_len=%d", assistantIdx, len(evt.Content), len(chat.Messages[assistantIdx].ReasoningContent))
			emit(ContinueEvent{Type: "reasoning_delta", Content: evt.Content})
		case "tool_call":
			tc := model.ToolCall{
				ID: evt.ID,
				Function: model.FunctionCall{
					Name:      evt.Name,
					Arguments: evt.Args,
				},
			}
			found := false
			for i := range chat.Messages[assistantIdx].ToolCalls {
				if chat.Messages[assistantIdx].ToolCalls[i].ID == tc.ID {
					chat.Messages[assistantIdx].ToolCalls[i] = tc
					found = true
					break
				}
			}
			if !found {
				chat.Messages[assistantIdx].ToolCalls = append(chat.Messages[assistantIdx].ToolCalls, tc)
			}
			logContinue("deepseek_event type=tool_call assistant_idx=%d tool_call_id=%q name=%q args_len=%d found=%v total_tool_calls=%d", assistantIdx, tc.ID, tc.Function.Name, len(tc.Function.Arguments), found, len(chat.Messages[assistantIdx].ToolCalls))
			emit(ContinueEvent{
				Type:     "tool_call",
				ToolCall: &tc,
			})
		case "done":
			logContinue("deepseek_event type=done assistant_idx=%d content_len=%d reasoning_len=%d tool_calls=%d", assistantIdx, len(chat.Messages[assistantIdx].Content), len(chat.Messages[assistantIdx].ReasoningContent), len(chat.Messages[assistantIdx].ToolCalls))
		}
	})

	if err != nil {
		streamErr = err
		logContinue("stream_deepseek_error assistant_idx=%d err=%q interrupted=%v", assistantIdx, err.Error(), interrupted())
	}

	if streamErr != nil {
		if interrupted() {
			logContinue("stream_deepseek_interrupted assistant_idx=%d content_len=%d reasoning_len=%d tool_calls=%d", assistantIdx, len(chat.Messages[assistantIdx].Content), len(chat.Messages[assistantIdx].ReasoningContent), len(chat.Messages[assistantIdx].ToolCalls))
			emit(ContinueEvent{Type: "assistant_done"})
			return nil
		}
		chat.Messages = chat.Messages[:assistantIdx]
		logContinue("stream_deepseek_rollback assistant_idx=%d messages=%d", assistantIdx, len(chat.Messages))
		emit(ContinueEvent{
			Type: "error",
			Error: &ErrorDetail{
				Type:   "deepseek_error",
				Detail: streamErr.Error(),
			},
		})
		return streamErr
	}

	msg := chat.Messages[assistantIdx]
	if msg.Content == "" && msg.ReasoningContent == "" && len(msg.ToolCalls) == 0 {
		chat.Messages = chat.Messages[:assistantIdx]
		logContinue("stream_deepseek_remove_empty_assistant assistant_idx=%d messages=%d", assistantIdx, len(chat.Messages))
	}
	logContinue("stream_deepseek_done assistant_idx=%d messages=%d last=%s", assistantIdx, len(chat.Messages), describeLastMessage(chat))
	emit(ContinueEvent{Type: "assistant_done"})
	if e.saveFunc != nil {
		logContinue("stream_deepseek_save assistant_idx=%d", assistantIdx)
		e.saveFunc()
	}
	return nil
}

func (e *Engine) checkAutoContinue(ctx context.Context, chat *model.Chat, autoContinue bool, emit func(ContinueEvent), interrupted func() bool) {
	if autoContinue && !interrupted() {
		logContinue("auto_continue_enter messages=%d last=%s", len(chat.Messages), describeLastMessage(chat))
		e.doContinue(ctx, chat, true, emit, interrupted)
	} else {
		logContinue("auto_continue_skip auto_continue=%v interrupted=%v messages=%d last=%s", autoContinue, interrupted(), len(chat.Messages), describeLastMessage(chat))
	}
}

func (e *Engine) backtrackToAssistant(chat *model.Chat, idx int) int {
	for i := idx - 1; i >= 0; i-- {
		msg := chat.Messages[i]
		if msg.Role == "assistant" {
			return i
		}
		if msg.Role != "tool" {
			return -1
		}
	}
	return -1
}

func (e *Engine) findInvalidToolCalls(msg model.Message) []string {
	var invalid []string
	for _, tc := range msg.ToolCalls {
		if tc.ID == "" || tc.Function.Name == "" {
			invalid = append(invalid, tc.ID)
			continue
		}
		if e.toolExecutor != nil && !e.toolExecutor.ToolExists(tc.Function.Name) {
			invalid = append(invalid, tc.ID)
		}
	}
	if len(msg.ToolCalls) > 0 {
		seen := map[string]bool{}
		for _, tc := range msg.ToolCalls {
			if seen[tc.ID] {
				invalid = append(invalid, tc.ID)
			}
			seen[tc.ID] = true
		}
	}
	return invalid
}

func (e *Engine) markInvalidAsNotFound(chat *model.Chat, msg model.Message, invalidIDs []string, emit func(ContinueEvent)) {
	invalidSet := make(map[string]bool, len(invalidIDs))
	for _, id := range invalidIDs {
		invalidSet[id] = true
	}
	for _, tc := range msg.ToolCalls {
		if invalidSet[tc.ID] {
			toolMsg := model.Message{
				Role:         "tool",
				ToolCallID:   tc.ID,
				Name:         tc.Function.Name,
				Content:      "tool not found",
				SendToServer: true,
			}
			chat.Messages = append(chat.Messages, toolMsg)
			emit(ContinueEvent{
				Type: "tool_result",
				ToolResult: &toolResultEvt{
					Message: toolMsg,
				},
			})
		}
	}
	if e.saveFunc != nil {
		e.saveFunc()
	}
}

func (e *Engine) checkApproval(msg *model.Message) (allApproved bool, needsApproval bool) {
	if e.toolExecutor == nil {
		return true, false
	}
	allApproved = true
	for _, tc := range msg.ToolCalls {
		if tc.Function.Name == "ask_user" {
			continue
		}
		approved, manually := e.toolExecutor.IsToolApproved(tc.Function.Name)
		if manually {
			needsApproval = true
			allApproved = false
		} else if !approved {
			allApproved = false
		}
	}
	return
}

type ValidationError struct {
	MessageIndex int    `json:"message_index"`
	Type         string `json:"type"`
	ToolCallID   string `json:"tool_call_id,omitempty"`
	Detail       string `json:"detail,omitempty"`
	Expected     int    `json:"expected,omitempty"`
	Actual       int    `json:"actual,omitempty"`
}

type ToolDefLookup func(name string) (exists bool)

func ValidateChat(chat *model.Chat, toolLookup ToolDefLookup) []ValidationError {
	var errs []ValidationError
	groups := buildMessageGroups(chat.Messages)

	duplicates := findDuplicateIDs(groups)
	for idx, id := range duplicates {
		errs = append(errs, ValidationError{
			MessageIndex: idx,
			Type:         "duplicate_id",
			ToolCallID:   id,
			Detail:       "Duplicate tool_call_id across assistant messages",
		})
	}

	for _, g := range groups {
		isLastGroup := g.AssistantIdx+1+len(g.ToolMessages) >= len(chat.Messages)
		if !isLastGroup {
			if len(g.ToolMessages) != len(g.AssistantMsg.ToolCalls) {
				errs = append(errs, ValidationError{
					MessageIndex: g.AssistantIdx,
					Type:         "group_size_mismatch",
					Expected:     len(g.AssistantMsg.ToolCalls),
					Actual:       len(g.ToolMessages),
					Detail:       "Tool message count mismatch with assistant tool_calls",
				})
			}
		}

		for _, tc := range g.AssistantMsg.ToolCalls {
			if toolLookup != nil && !toolLookup(tc.Function.Name) {
				errs = append(errs, ValidationError{
					MessageIndex: g.AssistantIdx,
					Type:         "invalid_tool_call",
					ToolCallID:   tc.ID,
					Detail:       "Tool not found: " + tc.Function.Name,
				})
			}
		}

		for j, tm := range g.ToolMessages {
			found := false
			var matchedTC *model.ToolCall
			for a := range g.AssistantMsg.ToolCalls {
				if g.AssistantMsg.ToolCalls[a].ID == tm.ToolCallID {
					found = true
					matchedTC = &g.AssistantMsg.ToolCalls[a]
					break
				}
			}
			if !found {
				errs = append(errs, ValidationError{
					MessageIndex: g.AssistantIdx + 1 + j,
					Type:         "orphan_tool",
					ToolCallID:   tm.ToolCallID,
					Detail:       "Tool message has no matching tool_call in assistant",
				})
			} else if matchedTC != nil && matchedTC.Function.Name != tm.Name {
				errs = append(errs, ValidationError{
					MessageIndex: g.AssistantIdx + 1 + j,
					Type:         "schema_mismatch",
					ToolCallID:   tm.ToolCallID,
					Detail:       "Tool name mismatch: tool_call expects '" + matchedTC.Function.Name + "', tool message has '" + tm.Name + "'",
				})
			}
		}

		toolIDsSeen := map[string]int{}
		for j, tm := range g.ToolMessages {
			if firstJ, ok := toolIDsSeen[tm.ToolCallID]; ok {
				errs = append(errs, ValidationError{
					MessageIndex: g.AssistantIdx + 1 + j,
					Type:         "duplicate_id",
					ToolCallID:   tm.ToolCallID,
					Detail:       "Duplicate tool_call_id within tool messages of same group",
				})
				_ = firstJ
			} else {
				toolIDsSeen[tm.ToolCallID] = j
			}
		}
	}

	return errs
}

type messageGroup struct {
	AssistantIdx int
	AssistantMsg *model.Message
	ToolMessages []model.Message
}

func buildMessageGroups(messages []model.Message) []messageGroup {
	var groups []messageGroup
	i := 0
	for i < len(messages) {
		msg := messages[i]
		if msg.Role == "assistant" {
			g := messageGroup{
				AssistantIdx: i,
				AssistantMsg: &messages[i],
			}
			j := i + 1
			for j < len(messages) && messages[j].Role == "tool" {
				g.ToolMessages = append(g.ToolMessages, messages[j])
				j++
			}
			groups = append(groups, g)
			i = j
		} else {
			i++
		}
	}
	return groups
}

func findDuplicateIDs(groups []messageGroup) map[int]string {
	seen := map[string]int{}
	dups := map[int]string{}
	for _, g := range groups {
		for _, tc := range g.AssistantMsg.ToolCalls {
			if firstIdx, ok := seen[tc.ID]; ok {
				dups[firstIdx] = tc.ID
				dups[g.AssistantIdx] = tc.ID
			} else {
				seen[tc.ID] = g.AssistantIdx
			}
		}
	}
	return dups
}

func logContinue(format string, args ...any) {
	log.Printf("[continue] "+format, args...)
}

func describeLastMessage(chat *model.Chat) string {
	if chat == nil || len(chat.Messages) == 0 {
		return "none"
	}
	return describeMessage(len(chat.Messages)-1, chat.Messages[len(chat.Messages)-1])
}

func describeMessage(idx int, msg model.Message) string {
	return fmt.Sprintf("idx=%d role=%s content_len=%d reasoning_len=%d tool_calls=%d tool_call_id=%q name=%q send=%v approved=%v", idx, msg.Role, len(msg.Content), len(msg.ReasoningContent), len(msg.ToolCalls), msg.ToolCallID, msg.Name, msg.SendToServer, msg.Approved)
}

func (e *Engine) collectExecutedToolIDs(chat *model.Chat, assistantIdx int) map[string]bool {
	executed := map[string]bool{}
	for i := assistantIdx + 1; i < len(chat.Messages); i++ {
		if chat.Messages[i].Role != "tool" {
			break
		}
		executed[chat.Messages[i].ToolCallID] = true
	}
	return executed
}

func FindOrphanToolErrors(chat *model.Chat) []ValidationError {
	return ValidateChat(chat, nil)
}

func DeleteMessage(chat *model.Chat, index int, mode string) ([]ValidationError, error) {
	if index < 0 || index >= len(chat.Messages) {
		return nil, fmt.Errorf("index out of range")
	}
	if mode != "sudo" && mode != "writable" {
		return nil, fmt.Errorf("mode does not permit editing")
	}

	// sudo mode: plain delete, no cascading, no validation
	if mode == "sudo" {
		chat.Messages = append(chat.Messages[:index], chat.Messages[index+1:]...)
		return nil, nil
	}

	msg := chat.Messages[index]

	switch msg.Role {
	case "user", "system":
		chat.Messages = append(chat.Messages[:index], chat.Messages[index+1:]...)
		return nil, nil

	case "assistant":
		return deleteAssistant(chat, index)

	case "tool":
		return deleteTool(chat, index)
	}

	return nil, fmt.Errorf("unknown role: %s", msg.Role)
}

func deleteAssistant(chat *model.Chat, index int) ([]ValidationError, error) {
	delMsg := chat.Messages[index]
	nextIdx := index + 1
	toolMsgs := []int{index}

	for nextIdx < len(chat.Messages) && chat.Messages[nextIdx].Role == "tool" {
		toolMsgs = append(toolMsgs, nextIdx)
		nextIdx++
	}

	if nextIdx < len(chat.Messages) && chat.Messages[nextIdx].Role == "assistant" {
		if delMsg.ReasoningContent != "" {
			chat.Messages[nextIdx].ReasoningContent = delMsg.ReasoningContent + "\n" + chat.Messages[nextIdx].ReasoningContent
		}
		chat.Messages = removeIndices(chat.Messages, toolMsgs)
	} else {
		chat.Messages = removeIndices(chat.Messages, toolMsgs)
	}

	return nil, nil
}

func deleteTool(chat *model.Chat, index int) ([]ValidationError, error) {
	origIdx := backtrackToAssistantStatic(chat.Messages, index)
	if origIdx < 0 {
		chat.Messages = append(chat.Messages[:index], chat.Messages[index+1:]...)
		return nil, nil
	}

	assistant := chat.Messages[origIdx]
	delMsg := chat.Messages[index]

	hadToolCalls := len(assistant.ToolCalls) > 0

	var newToolCalls []model.ToolCall
	for _, tc := range assistant.ToolCalls {
		if tc.ID != delMsg.ToolCallID {
			newToolCalls = append(newToolCalls, tc)
		}
	}
	chat.Messages[origIdx].ToolCalls = newToolCalls

	if hadToolCalls && len(newToolCalls) == 0 {
		nextIdx := origIdx + 1
		var toolMsgs []int
		for nextIdx < len(chat.Messages) && chat.Messages[nextIdx].Role == "tool" {
			toolMsgs = append(toolMsgs, nextIdx)
			nextIdx++
		}

		if nextIdx < len(chat.Messages) && chat.Messages[nextIdx].Role == "assistant" {
			if chat.Messages[origIdx].ReasoningContent != "" {
				chat.Messages[nextIdx].ReasoningContent = chat.Messages[origIdx].ReasoningContent + "\n" + chat.Messages[nextIdx].ReasoningContent
			}
			deleteIdxs := append(toolMsgs, origIdx)
			chat.Messages = removeIndices(chat.Messages, deleteIdxs)
		} else {
			chat.Messages = removeIndices(chat.Messages, toolMsgs)
		}
	} else {
		chat.Messages = append(chat.Messages[:index], chat.Messages[index+1:]...)
	}

	return nil, nil
}

func EditMessage(chat *model.Chat, index int, newMsg *model.Message, mode string, toolExist func(name string) bool) ([]ValidationError, error) {
	if index < 0 || index >= len(chat.Messages) {
		return nil, fmt.Errorf("index out of range")
	}
	if mode != "sudo" && mode != "writable" {
		return nil, fmt.Errorf("mode does not permit editing")
	}

	oldMsg := chat.Messages[index]

	if mode == "sudo" {
		chat.Messages[index] = *newMsg
		return nil, nil
	}

	if newMsg.Role != "assistant" {
		chat.Messages[index] = *newMsg
		return nil, nil
	}

	if toolExist != nil {
		for _, tc := range newMsg.ToolCalls {
			if !toolExist(tc.Function.Name) {
				return []ValidationError{{
					MessageIndex: index,
					Type:         "invalid_tool_call",
					ToolCallID:   tc.ID,
					Detail:       "Tool not found: " + tc.Function.Name,
				}}, nil
			}
		}
	}

	oldIDs := map[string]bool{}
	for _, tc := range oldMsg.ToolCalls {
		oldIDs[tc.ID] = true
	}

	var insertedCount int
	for _, tc := range newMsg.ToolCalls {
		if !oldIDs[tc.ID] {
			toolMsg := model.Message{
				Role:         "tool",
				ToolCallID:   tc.ID,
				Name:         tc.Function.Name,
				Content:      "error",
				SendToServer: true,
			}
			chat.Messages = append(chat.Messages[:index+1+insertedCount], append([]model.Message{toolMsg}, chat.Messages[index+1+insertedCount:]...)...)
			insertedCount++
		}
	}

	chat.Messages[index] = *newMsg

	seenIDs := map[string]bool{}
	for _, tc := range newMsg.ToolCalls {
		if seenIDs[tc.ID] {
			return []ValidationError{{
				MessageIndex: index,
				Type:         "duplicate_id",
				ToolCallID:   tc.ID,
				Detail:       "Duplicate tool_call_id in edited message",
			}}, nil
		}
		seenIDs[tc.ID] = true
	}

	return nil, nil
}

func InsertMessage(chat *model.Chat, index int, newMsg *model.Message, mode string) ([]ValidationError, error) {
	if index < 0 || index > len(chat.Messages) {
		return nil, fmt.Errorf("index out of range")
	}
	if mode != "sudo" && mode != "writable" {
		return nil, fmt.Errorf("mode does not permit editing")
	}

	if mode == "sudo" {
		chat.Messages = append(chat.Messages[:index], append([]model.Message{*newMsg}, chat.Messages[index:]...)...)
		return nil, nil
	}

	nextIsTool := index < len(chat.Messages) && chat.Messages[index].Role == "tool"

	if newMsg.Role != "tool" {
		if nextIsTool {
			return []ValidationError{{
				MessageIndex: index,
				Type:         "insert_violation",
				Detail:       "Cannot insert non-tool message before a tool message",
			}}, nil
		}
		chat.Messages = append(chat.Messages[:index], append([]model.Message{*newMsg}, chat.Messages[index:]...)...)
		return nil, nil
	}

	if index == 0 {
		return []ValidationError{{
			MessageIndex: 0,
			Type:         "insert_violation",
			Detail:       "Cannot insert tool message at the beginning",
		}}, nil
	}

	origIdx := backtrackToAssistantStatic(chat.Messages, index-1)
	if origIdx < 0 {
		return []ValidationError{{
			MessageIndex: index,
			Type:         "insert_violation",
			Detail:       "No originating assistant found for tool message",
		}}, nil
	}

	assistant := chat.Messages[origIdx]

	idExistsInAssistant := false
	for _, tc := range assistant.ToolCalls {
		if tc.ID == newMsg.ToolCallID {
			idExistsInAssistant = true
			break
		}
	}

	for i := origIdx + 1; i < len(chat.Messages); i++ {
		if chat.Messages[i].Role != "tool" {
			break
		}
		if chat.Messages[i].ToolCallID == newMsg.ToolCallID {
			return []ValidationError{{
				MessageIndex: i,
				Type:         "duplicate_id",
				ToolCallID:   newMsg.ToolCallID,
				Detail:       "tool_call_id already used in this group",
			}}, nil
		}
	}

	if !idExistsInAssistant {
		newTC := model.ToolCall{
			ID: newMsg.ToolCallID,
			Function: model.FunctionCall{
				Name:      newMsg.Name,
				Arguments: "{}",
			},
		}
		chat.Messages[origIdx].ToolCalls = append(chat.Messages[origIdx].ToolCalls, newTC)
	}

	chat.Messages = append(chat.Messages[:index], append([]model.Message{*newMsg}, chat.Messages[index:]...)...)
	return nil, nil
}

func backtrackToAssistantStatic(messages []model.Message, idx int) int {
	if idx < 0 || idx >= len(messages) {
		return -1
	}
	for i := idx; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == "assistant" {
			return i
		}
		if msg.Role != "tool" {
			return -1
		}
	}
	return -1
}

func removeIndices(msgs []model.Message, indices []int) []model.Message {
	skip := map[int]bool{}
	for _, i := range indices {
		skip[i] = true
	}
	var result []model.Message
	for i, m := range msgs {
		if !skip[i] {
			result = append(result, m)
		}
	}
	return result
}

func (e *Engine) validateArgs(toolName string, arguments string) string {
	if e.toolExecutor == nil {
		return ""
	}
	def := e.toolExecutor.GetToolDef(toolName)
	if def == nil {
		return ""
	}
	schema, ok := def.InputSchema.(map[string]any)
	if !ok {
		return ""
	}

	var args map[string]any
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return "invalid JSON arguments: " + err.Error()
		}
	}
	if args == nil {
		args = map[string]any{}
	}

	props, _ := schema["properties"].(map[string]any)
	requiredSet := make(map[string]bool)
	switch r := schema["required"].(type) {
	case []any:
		for _, v := range r {
			if s, ok := v.(string); ok {
				requiredSet[s] = true
			}
		}
	case []string:
		for _, s := range r {
			requiredSet[s] = true
		}
	}

	for key := range requiredSet {
		if _, exists := args[key]; !exists {
			return fmt.Sprintf("missing required argument '%s'", key)
		}
	}

	for key, val := range args {
		if props == nil {
			break
		}
		prop, ok := props[key]
		if !ok {
			return fmt.Sprintf("unknown argument '%s'", key)
		}
		propMap, ok := prop.(map[string]any)
		if !ok {
			continue
		}
		expectedType, _ := propMap["type"].(string)
		switch expectedType {
		case "string":
			if _, ok := val.(string); !ok {
				return fmt.Sprintf("argument '%s' must be a string", key)
			}
		case "number", "integer":
			if _, ok := val.(float64); !ok {
				return fmt.Sprintf("argument '%s' must be a number", key)
			}
		case "boolean":
			if _, ok := val.(bool); !ok {
				return fmt.Sprintf("argument '%s' must be a boolean", key)
			}
		}
	}

	return ""
}
