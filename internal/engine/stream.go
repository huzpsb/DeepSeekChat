package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	cont "hschat/internal/continue"
	"hschat/internal/deepseek"
	"hschat/internal/model"
	"hschat/internal/storage"
)

type State int

const (
	StateIdle State = iota
	StateInferencing
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateInferencing:
		return "inferencing"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

type StreamEngine struct {
	mu        sync.Mutex
	state     State
	interrupt bool

	activeTitle string
	activeChat  *model.Chat
	events      []cont.ContinueEvent
	streamDone  bool
	savedPos    int

	cancelFunc context.CancelFunc

	mode     string
	mcpMgr   cont.ToolExecutor
	dsClient *deepseek.Client
}

var instance *StreamEngine

func Init(dsClient *deepseek.Client, executor cont.ToolExecutor) *StreamEngine {
	instance = &StreamEngine{
		state:    StateIdle,
		mcpMgr:   executor,
		dsClient: dsClient,
	}
	return instance
}

func Get() *StreamEngine {
	return instance
}

func (e *StreamEngine) SetMode(mode string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.mode = mode
}

func (e *StreamEngine) Mode() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.mode
}

func (e *StreamEngine) IsInferencing() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state == StateInferencing
}

func (e *StreamEngine) IsInferencingWith(title string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state == StateInferencing && e.activeTitle == title
}

func (e *StreamEngine) ActiveTitle() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.activeTitle
}

func (e *StreamEngine) IsInterrupted() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.interrupt
}

func (e *StreamEngine) RequestInterrupt() {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Printf("[stream] interrupt_requested state=%s active_title=%q events=%d saved_pos=%d stream_done=%v interrupt=%v\n", e.state, e.activeTitle, len(e.events), e.savedPos, e.streamDone, e.interrupt)
	if e.state == StateInferencing {
		e.interrupt = true
		if e.cancelFunc != nil {
			fmt.Printf("[stream] interrupt_cancel active_title=%q\n", e.activeTitle)
			e.cancelFunc()
		}
	}
}

func (e *StreamEngine) WaitForIdle() {
	start := time.Now()
	logged := false
	for {
		e.mu.Lock()
		if e.state == StateIdle {
			fmt.Printf("[stream] wait_for_idle_done elapsed_ms=%d active_title=%q events=%d saved_pos=%d stream_done=%v interrupt=%v\n", time.Since(start).Milliseconds(), e.activeTitle, len(e.events), e.savedPos, e.streamDone, e.interrupt)
			e.mu.Unlock()
			return
		}
		if !logged || time.Since(start) > time.Second {
			fmt.Printf("[stream] wait_for_idle_waiting elapsed_ms=%d state=%s active_title=%q events=%d saved_pos=%d stream_done=%v interrupt=%v\n", time.Since(start).Milliseconds(), e.state, e.activeTitle, len(e.events), e.savedPos, e.streamDone, e.interrupt)
			logged = true
		}
		e.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *StreamEngine) StartInference(title, input string, autoContinue bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Printf("[stream] start_inference_request title=%q input_len=%d auto_continue=%v state=%s active_title=%q events=%d saved_pos=%d stream_done=%v\n", title, len(input), autoContinue, e.state, e.activeTitle, len(e.events), e.savedPos, e.streamDone)

	if e.state == StateInferencing {
		fmt.Printf("[stream] start_inference_reject title=%q reason=already_inferencing active_title=%q\n", title, e.activeTitle)
		return fmt.Errorf("another chat is being processed")
	}

	chat, err := storage.GetChat(title)
	if err != nil {
		fmt.Printf("[stream] start_inference_reject title=%q reason=load_chat_error err=%q\n", title, err.Error())
		return fmt.Errorf("chat not found: %w", err)
	}
	fmt.Printf("[stream] start_inference_loaded title=%q messages=%d last=%s\n", title, len(chat.Messages), describeLastMessage(chat))

	e.state = StateInferencing
	e.interrupt = false
	e.activeTitle = title
	e.activeChat = chat
	e.events = nil
	e.savedPos = 0
	e.streamDone = false
	fmt.Printf("[stream] start_inference_state_set title=%q state=%s events=%d saved_pos=%d stream_done=%v\n", title, e.state, len(e.events), e.savedPos, e.streamDone)

	ctx, cancel := context.WithCancel(context.Background())
	e.cancelFunc = cancel

	go func() {
		fmt.Printf("[stream] goroutine_start title=%q messages=%d last=%s\n", title, len(chat.Messages), describeLastMessage(chat))
		emit := func(evt cont.ContinueEvent) {
			e.mu.Lock()
			e.events = append(e.events, evt)
			eventCount := len(e.events)
			savedPos := e.savedPos
			e.mu.Unlock()
			fmt.Printf("[stream] emit title=%q event=%s event_count=%d saved_pos=%d %s\n", title, evt.Type, eventCount, savedPos, describeEvent(evt))
		}

		defer cancel()

		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[stream] goroutine_panic title=%q panic=%v\n", title, r)
				emit(cont.ContinueEvent{
					Type: "error",
					Error: &cont.ErrorDetail{
						Type:   "internal_error",
						Detail: fmt.Sprintf("internal panic: %v", r),
					},
				})
			}

			fmt.Printf("[stream] defer_save_start title=%q messages=%d last=%s\n", title, len(chat.Messages), describeLastMessage(chat))
			if err := storage.SaveChat(chat); err != nil {
				fmt.Printf("[stream] defer_save_error title=%q err=%q\n", title, err.Error())
			} else {
				fmt.Printf("[stream] defer_save_ok title=%q messages=%d last=%s\n", title, len(chat.Messages), describeLastMessage(chat))
			}

			e.mu.Lock()
			e.savedPos = len(e.events)
			e.streamDone = true
			e.state = StateIdle
			fmt.Printf("[stream] goroutine_done title=%q state=%s events=%d saved_pos=%d stream_done=%v interrupt=%v\n", title, e.state, len(e.events), e.savedPos, e.streamDone, e.interrupt)
			e.mu.Unlock()
			cancel()
		}()

		save := func() {
			fmt.Printf("[stream] save_func_start title=%q messages=%d last=%s\n", title, len(chat.Messages), describeLastMessage(chat))
			if err := storage.SaveChat(chat); err != nil {
				fmt.Printf("[stream] save_func_error title=%q err=%q\n", title, err.Error())
			} else {
				fmt.Printf("[stream] save_func_ok title=%q messages=%d last=%s\n", title, len(chat.Messages), describeLastMessage(chat))
			}
			e.mu.Lock()
			e.savedPos = len(e.events)
			fmt.Printf("[stream] save_func_mark_saved title=%q events=%d saved_pos=%d stream_done=%v\n", title, len(e.events), e.savedPos, e.streamDone)
			e.mu.Unlock()
		}

		engine := cont.NewEngine(e.dsClient, e.mode, e.mcpMgr, save)

		interrupted := func() bool {
			e.mu.Lock()
			defer e.mu.Unlock()
			return e.interrupt
		}

		engine.Continue(ctx, chat, input, autoContinue, emit, interrupted)
		fmt.Printf("[stream] engine_continue_returned title=%q messages=%d last=%s\n", title, len(chat.Messages), describeLastMessage(chat))
	}()

	return nil
}

type EventReader struct {
	eng *StreamEngine
	pos int
}

func (e *StreamEngine) Subscribe(title string) *EventReader {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Printf("[stream] subscribe_request title=%q active_title=%q state=%s events=%d saved_pos=%d stream_done=%v interrupt=%v\n", title, e.activeTitle, e.state, len(e.events), e.savedPos, e.streamDone, e.interrupt)

	if e.activeTitle != title {
		fmt.Printf("[stream] subscribe_reject title=%q active_title=%q\n", title, e.activeTitle)
		return nil
	}

	fmt.Printf("[stream] subscribe_ok title=%q start_pos=%d events=%d stream_done=%v\n", title, e.savedPos, len(e.events), e.streamDone)
	return &EventReader{eng: e, pos: e.savedPos}
}

func (r *EventReader) Wait(ctx context.Context) ([]cont.ContinueEvent, bool) {
	e := r.eng
	e.mu.Lock()
	defer e.mu.Unlock()

	for r.pos >= len(e.events) && !e.streamDone {
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			e.mu.Lock()
			return nil, true
		case <-time.After(50 * time.Millisecond):
		}
		e.mu.Lock()
	}

	var batch []cont.ContinueEvent
	for r.pos < len(e.events) {
		batch = append(batch, e.events[r.pos])
		r.pos++
	}

	done := e.streamDone && r.pos >= len(e.events)
	if len(batch) > 0 || done {
		fmt.Printf("[stream] reader_wait_return active_title=%q batch=%d pos=%d events=%d saved_pos=%d stream_done=%v done=%v\n", e.activeTitle, len(batch), r.pos, len(e.events), e.savedPos, e.streamDone, done)
	}
	return batch, done
}

func describeLastMessage(chat *model.Chat) string {
	if chat == nil || len(chat.Messages) == 0 {
		return "none"
	}
	msg := chat.Messages[len(chat.Messages)-1]
	return fmt.Sprintf("idx=%d role=%s content_len=%d reasoning_len=%d tool_calls=%d tool_call_id=%q name=%q send=%v approved=%v", len(chat.Messages)-1, msg.Role, len(msg.Content), len(msg.ReasoningContent), len(msg.ToolCalls), msg.ToolCallID, msg.Name, msg.SendToServer, msg.Approved)
}

func describeEvent(evt cont.ContinueEvent) string {
	s := fmt.Sprintf("content_len=%d", len(evt.Content))
	if evt.ToolCall != nil {
		s += fmt.Sprintf(" tool_call_id=%q tool_name=%q args_len=%d", evt.ToolCall.ID, evt.ToolCall.Function.Name, len(evt.ToolCall.Function.Arguments))
	}
	if evt.ToolResult != nil {
		msg := evt.ToolResult.Message
		s += fmt.Sprintf(" tool_result_id=%q tool_name=%q result_len=%d", msg.ToolCallID, msg.Name, len(msg.Content))
	}
	if evt.Error != nil {
		s += fmt.Sprintf(" error_type=%q error_detail_len=%d ids=%v", evt.Error.Type, len(evt.Error.Detail), evt.Error.IDs)
	}
	return s
}
