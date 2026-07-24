package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	cont "hschat/internal/continue"
	"hschat/internal/deepseek"
	"hschat/internal/mcp"
	"hschat/internal/model"
	"hschat/internal/storage"
)

func Run(prompt, title string) error {
	cfg, err := storage.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	mcpMgr := mcp.NewManager()
	mcpMgr.SkipAskUser = true
	mcpMgr.ApproveAll = true
	if err := mcpMgr.LoadAndConnect(); err != nil {
		return fmt.Errorf("mcp init: %w", err)
	}

	dsClient := deepseek.NewClient(cfg.APIKey, cfg.ThirdParty)

	if title == "" {
		title = time.Now().Format("2006-01-02 150405")
	}

	chat := &model.Chat{
		Title:    title,
		Messages: []model.Message{},
	}
	if err := storage.SaveChat(chat); err != nil {
		return fmt.Errorf("create chat: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	engine := cont.NewEngine(dsClient, "readonly", mcpMgr, func() {
		storage.SaveChat(chat)
	})
	engine.ContinueOnInvalidArgs = true

	var lastError string
	emit := func(evt cont.ContinueEvent) {
		switch evt.Type {
		case "delta":
			fmt.Print(evt.Content)
		case "tool_call":
			if evt.ToolCall != nil {
				fmt.Printf("\n[TOOL] %s\n", evt.ToolCall.Function.Name)
			}
		case "tool_result":
			if evt.ToolResult != nil {
				content := evt.ToolResult.Message.Content
				if len(content) > 500 {
					content = content[:500] + "..."
				}
				fmt.Printf("[RESULT] %s\n", content)
			}
		case "error":
			if evt.Error != nil {
				lastError = evt.Error.Detail
				fmt.Fprintf(os.Stderr, "\n[ERROR] %s\n", lastError)
			}
		case "user_added":
			fmt.Printf("[PROMPT] %s\n", prompt)
		}
	}

	interrupted := func() bool { return false }

	engine.Continue(ctx, chat, prompt, true, emit, interrupted)

	if err := storage.SaveChat(chat); err != nil {
		fmt.Fprintf(os.Stderr, "save chat: %v\n", err)
	}

	if lastError != "" {
		return fmt.Errorf("engine error: %s", lastError)
	}

	fmt.Println()
	return nil
}
