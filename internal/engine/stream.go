package engine

import (
	"context"
	"fmt"
	"sync"

	"hschat/internal/builtin/sandbox"
	cont "hschat/internal/continue"
	"hschat/internal/llm"
	"hschat/internal/log"
	"hschat/internal/model"
	"hschat/internal/storage"
)

// Session holds the reentrant stream state of a single chat.
// The event log of the current run is kept in memory only; events below
// savedPos are already reflected on disk. A subscriber can therefore
// reconstruct the exact live state as: disk history + events[savedPos:].
type Session struct {
	mu        sync.Mutex
	title     string
	running   bool
	interrupt bool
	gen       int64
	events    []cont.ContinueEvent
	savedPos  int
	cancel    context.CancelFunc
	notify    chan struct{} // closed & replaced on every state change
}

// broadcastLocked wakes all waiters. Caller must hold s.mu.
func (s *Session) broadcastLocked() {
	close(s.notify)
	s.notify = make(chan struct{})
}

// StreamEngine manages per-chat inference sessions.
type StreamEngine struct {
	mu       sync.Mutex
	sessions map[string]*Session

	// global running-state broadcast: statusVersion is bumped (and
	// statusNotify closed & replaced) every time any session's running
	// flag changes, so status subscribers can watch all chats at once.
	statusVersion uint64
	statusNotify  chan struct{}

	mode   string
	mcpMgr cont.ToolExecutor
	client *llm.Client
}

func Init(client *llm.Client, executor cont.ToolExecutor) *StreamEngine {
	return &StreamEngine{
		sessions:     map[string]*Session{},
		statusNotify: make(chan struct{}),
		mcpMgr:       executor,
		client:       client,
	}
}

// SetClient swaps the LLM client used by subsequent inference runs.
func (e *StreamEngine) SetClient(client *llm.Client) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.client = client
}

func (e *StreamEngine) getClient() *llm.Client {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.client
}

func (e *StreamEngine) getSession(title string, create bool) *Session {
	e.mu.Lock()
	defer e.mu.Unlock()
	sess, ok := e.sessions[title]
	if !ok && create {
		sess = &Session{title: title, notify: make(chan struct{})}
		e.sessions[title] = sess
	}
	return sess
}

// DropSession forgets a session (chat deleted/renamed). Must not be running.
func (e *StreamEngine) DropSession(title string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if sess, ok := e.sessions[title]; ok {
		sess.mu.Lock()
		running := sess.running
		sess.mu.Unlock()
		if !running {
			delete(e.sessions, title)
		}
	}
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

func (e *StreamEngine) IsInferencingWith(title string) bool {
	sess := e.getSession(title, false)
	if sess == nil {
		return false
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.running
}

// runningChatsLocked computes the running set. Caller must hold e.mu.
func (e *StreamEngine) runningChatsLocked() map[string]bool {
	out := map[string]bool{}
	for _, sess := range e.sessions {
		sess.mu.Lock()
		if sess.running {
			out[sess.title] = true
		}
		sess.mu.Unlock()
	}
	return out
}

// RunningChats returns the set of chat titles currently inferencing.
func (e *StreamEngine) RunningChats() map[string]bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.runningChatsLocked()
}

// broadcastStatus wakes all status subscribers. Must be called AFTER the
// running-state change has been committed (and the session lock released,
// to keep the e.mu -> sess.mu lock order).
func (e *StreamEngine) broadcastStatus() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.statusVersion++
	close(e.statusNotify)
	e.statusNotify = make(chan struct{})
}

// RunningChatsSnapshot returns the current running set together with a
// version token for WaitStatusChange.
func (e *StreamEngine) RunningChatsSnapshot() (map[string]bool, uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.runningChatsLocked(), e.statusVersion
}

// WaitStatusChange blocks until the running set's version differs from
// version, then returns the new snapshot. ok=false means ctx was cancelled.
func (e *StreamEngine) WaitStatusChange(ctx context.Context, version uint64) (map[string]bool, uint64, bool) {
	for {
		e.mu.Lock()
		if e.statusVersion != version {
			running := e.runningChatsLocked()
			v := e.statusVersion
			e.mu.Unlock()
			return running, v, true
		}
		ch := e.statusNotify
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, version, false
		case <-ch:
		}
	}
}

func (e *StreamEngine) RequestInterrupt(title string) bool {
	sess := e.getSession(title, false)
	if sess == nil {
		return false
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	log.Printf("[stream] interrupt_requested title=%q running=%v events=%d saved_pos=%d gen=%d\n", title, sess.running, len(sess.events), sess.savedPos, sess.gen)
	if sess.running {
		sess.interrupt = true
		if sess.cancel != nil {
			sess.cancel()
		}
		sess.broadcastLocked()
		return true
	}
	return false
}

func (e *StreamEngine) WaitForIdle(title string) {
	sess := e.getSession(title, false)
	if sess == nil {
		return
	}
	for {
		sess.mu.Lock()
		if !sess.running {
			sess.mu.Unlock()
			return
		}
		ch := sess.notify
		sess.mu.Unlock()
		<-ch
	}
}

// ReadChatConsistent reads the chat file while holding the session lock so
// the returned savedPos exactly matches the disk content: everything below
// savedPos in the session event log is contained in the returned messages.
func (e *StreamEngine) ReadChatConsistent(title string) (chat *model.Chat, savedPos int, running bool, err error) {
	sess := e.getSession(title, false)
	if sess == nil {
		chat, err = storage.GetChat(title)
		return chat, 0, false, err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	chat, err = storage.GetChat(title)
	return chat, sess.savedPos, sess.running, err
}

func (e *StreamEngine) StartInference(title, input string, autoContinue bool) error {
	mode := e.Mode()
	sess := e.getSession(title, true)

	sess.mu.Lock()
	if sess.running {
		sess.mu.Unlock()
		log.Printf("[stream] start_inference_reject title=%q reason=busy\n", title)
		return fmt.Errorf("chat is currently being processed")
	}
	chat, err := storage.GetChat(title)
	if err != nil {
		sess.mu.Unlock()
		log.Printf("[stream] start_inference_reject title=%q reason=load_chat_error err=%q\n", title, err.Error())
		return fmt.Errorf("chat not found: %w", err)
	}
	sess.running = true
	sess.interrupt = false
	sess.gen++
	sess.events = nil
	sess.savedPos = 0
	ctx, cancel := context.WithCancel(context.Background())
	if chat.RootDir != "" {
		ctx = sandbox.WithRootDir(ctx, chat.RootDir)
	}
	sess.cancel = cancel
	gen := sess.gen
	sess.broadcastLocked()
	sess.mu.Unlock()
	e.broadcastStatus()
	log.Printf("[stream] start_inference title=%q gen=%d input_len=%d auto_continue=%v messages=%d last=%s\n", title, gen, len(input), autoContinue, len(chat.Messages), describeLastMessage(chat))

	go e.run(sess, ctx, cancel, chat, input, autoContinue, mode)
	return nil
}

func (e *StreamEngine) run(sess *Session, ctx context.Context, cancel context.CancelFunc, chat *model.Chat, input string, autoContinue bool, mode string) {
	title := sess.title
	emit := func(evt cont.ContinueEvent) {
		sess.mu.Lock()
		sess.events = append(sess.events, evt)
		count := len(sess.events)
		sess.broadcastLocked()
		sess.mu.Unlock()
		log.Printf("[stream] emit title=%q event=%s event_count=%d %s\n", title, evt.Type, count, describeEvent(evt))
	}

	defer func() {
		sess.mu.Lock()
		if r := recover(); r != nil {
			log.Printf("[stream] goroutine_panic title=%q panic=%v\n", title, r)
			sess.events = append(sess.events, cont.ContinueEvent{
				Type: "error",
				Error: &cont.ErrorDetail{
					Type:   "internal_error",
					Detail: fmt.Sprintf("internal panic: %v", r),
				},
			})
		}
		saveLen := len(sess.events)
		if err := storage.SaveChat(chat); err != nil {
			log.Printf("[stream] final_save_error title=%q err=%q\n", title, err.Error())
		} else {
			sess.savedPos = saveLen
		}
		sess.running = false
		sess.broadcastLocked()
		log.Printf("[stream] goroutine_done title=%q events=%d saved_pos=%d gen=%d messages=%d last=%s\n", title, len(sess.events), sess.savedPos, sess.gen, len(chat.Messages), describeLastMessage(chat))
		sess.mu.Unlock()
		// broadcast after releasing sess.mu to keep the e.mu -> sess.mu order
		e.broadcastStatus()
		cancel()
	}()

	// save persists the chat at a message boundary and advances savedPos.
	// It runs on the inference goroutine, so events cannot change mid-save.
	save := func() {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		saveLen := len(sess.events)
		if err := storage.SaveChat(chat); err != nil {
			log.Printf("[stream] save_error title=%q err=%q\n", title, err.Error())
			return
		}
		sess.savedPos = saveLen
		log.Printf("[stream] saved title=%q events=%d saved_pos=%d gen=%d messages=%d last=%s\n", title, len(sess.events), sess.savedPos, sess.gen, len(chat.Messages), describeLastMessage(chat))
	}

	engine := cont.NewEngine(e.getClient(), mode, e.mcpMgr, save)

	interrupted := func() bool {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		return sess.interrupt
	}

	engine.Continue(ctx, chat, input, autoContinue, emit, interrupted)
	log.Printf("[stream] engine_continue_returned title=%q messages=%d last=%s\n", title, len(chat.Messages), describeLastMessage(chat))
}

// ReadResult is one step of a subscription.
type ReadResult struct {
	Events   []cont.ContinueEvent
	Reset    bool // gen changed (or initial sync): consumer must resync from SavedPos
	Idle     bool // run is idle; delivered exactly once per gen
	Gen      int64
	SavedPos int
	Running  bool
	// Errors carries the current run's error events on Reset. Error events
	// are never persisted to disk, so SavedPos can advance past them without
	// any subscriber ever having seen them (e.g. a run that fails instantly,
	// before the subscriber processes the gen change). Re-delivering them
	// with the resync guarantees the failure reason is never lost.
	Errors []cont.ContinueEvent
}

type EventReader struct {
	sess     *Session
	pos      int
	gen      int64
	needSync bool
	idleSent bool
}

func (e *StreamEngine) Subscribe(title string) *EventReader {
	sess := e.getSession(title, true)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	log.Printf("[stream] subscribe title=%q running=%v events=%d saved_pos=%d gen=%d\n", title, sess.running, len(sess.events), sess.savedPos, sess.gen)
	return &EventReader{
		sess:     sess,
		pos:      sess.savedPos,
		gen:      sess.gen,
		needSync: true,
	}
}

// Wait blocks until there is something to report or ctx is done.
// ok=false means ctx was cancelled.
func (r *EventReader) Wait(ctx context.Context) (res ReadResult, ok bool) {
	sess := r.sess
	for {
		sess.mu.Lock()
		if r.needSync || r.gen != sess.gen {
			r.needSync = false
			r.gen = sess.gen
			r.pos = sess.savedPos
			r.idleSent = false
			var errs []cont.ContinueEvent
			for _, evt := range sess.events {
				if evt.Type == "error" {
					errs = append(errs, evt)
				}
			}
			res = ReadResult{Reset: true, Gen: sess.gen, SavedPos: sess.savedPos, Running: sess.running, Errors: errs}
			sess.mu.Unlock()
			return res, true
		}
		if r.pos < len(sess.events) {
			batch := make([]cont.ContinueEvent, len(sess.events)-r.pos)
			copy(batch, sess.events[r.pos:])
			r.pos = len(sess.events)
			sess.mu.Unlock()
			return ReadResult{Events: batch}, true
		}
		if !sess.running && !r.idleSent {
			r.idleSent = true
			res = ReadResult{Idle: true, Gen: sess.gen}
			sess.mu.Unlock()
			return res, true
		}
		ch := sess.notify
		sess.mu.Unlock()
		select {
		case <-ctx.Done():
			return ReadResult{}, false
		case <-ch:
		}
	}
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
