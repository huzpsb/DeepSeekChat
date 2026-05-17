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

type StreamEngine struct {
	mu        sync.Mutex
	state     State
	interrupt bool

	activeTitle string
	activeChat  *model.Chat
	events      []cont.ContinueEvent
	streamDone  bool

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
	if e.state == StateInferencing {
		e.interrupt = true
		if e.cancelFunc != nil {
			e.cancelFunc()
		}
	}
}

func (e *StreamEngine) WaitForIdle() {
	for {
		e.mu.Lock()
		if e.state == StateIdle {
			e.mu.Unlock()
			return
		}
		e.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *StreamEngine) StartInference(title, input string, autoContinue bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state == StateInferencing {
		return fmt.Errorf("another chat is being processed")
	}

	chat, err := storage.GetChat(title)
	if err != nil {
		return fmt.Errorf("chat not found: %w", err)
	}

	e.state = StateInferencing
	e.interrupt = false
	e.activeTitle = title
	e.activeChat = chat
	e.events = nil
	e.streamDone = false

	ctx, cancel := context.WithCancel(context.Background())
	e.cancelFunc = cancel

	go func() {
		defer cancel()

		engine := cont.NewEngine(e.dsClient, e.mode, e.mcpMgr)

		interrupted := func() bool {
			e.mu.Lock()
			defer e.mu.Unlock()
			return e.interrupt
		}

		emit := func(evt cont.ContinueEvent) {
			e.mu.Lock()
			e.events = append(e.events, evt)
			e.mu.Unlock()
		}

		engine.Continue(ctx, chat, input, autoContinue, emit, interrupted)

		e.mu.Lock()
		e.streamDone = true
		e.state = StateIdle
		e.mu.Unlock()

		storage.SaveChat(chat)
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

	if e.activeTitle != title {
		return nil
	}

	return &EventReader{eng: e, pos: 0}
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

	return batch, e.streamDone && r.pos >= len(e.events)
}
