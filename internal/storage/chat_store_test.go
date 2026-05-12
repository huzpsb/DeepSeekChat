package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hschat/internal/model"
)

func TestSaveAndGetChat(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{
		Title: "test_chat",
		Messages: []model.Message{
			{Role: "user", Content: "hello", SendToServer: true},
			{Role: "assistant", Content: "hi there", SendToServer: true},
		},
	}

	err := SaveChat(chat)
	if err != nil {
		t.Fatalf("SaveChat failed: %v", err)
	}

	// Verify file exists
	expectedPath := filepath.Join(chatsDir, "test_chat.json")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("chat file not created: %v", err)
	}

	var saved model.Chat
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("failed to unmarshal saved file: %v", err)
	}
	if saved.Title != "test_chat" {
		t.Errorf("expected title 'test_chat', got '%s'", saved.Title)
	}

	// Get it back
	retrieved, err := GetChat("test_chat")
	if err != nil {
		t.Fatalf("GetChat failed: %v", err)
	}
	if retrieved.Title != "test_chat" {
		t.Errorf("expected title 'test_chat', got '%s'", retrieved.Title)
	}
	if len(retrieved.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(retrieved.Messages))
	}
	if retrieved.Messages[0].Content != "hello" {
		t.Errorf("expected 'hello', got '%s'", retrieved.Messages[0].Content)
	}
}

func TestGetChat_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	_, err := GetChat("nonexistent")
	if err == nil {
		t.Errorf("expected error for nonexistent chat")
	}
}

func TestListChats_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chats, err := ListChats()
	if err != nil {
		t.Fatalf("ListChats failed: %v", err)
	}
	if len(chats) != 0 {
		t.Errorf("expected 0 chats, got %d", len(chats))
	}
}

func TestListChats_Multiple(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chats := []*model.Chat{
		{Title: "chat_a", Messages: []model.Message{{Role: "user", Content: "a", SendToServer: true}}},
		{Title: "chat_b", Messages: []model.Message{{Role: "user", Content: "b", SendToServer: true}}},
		{Title: "chat_c", Messages: []model.Message{{Role: "user", Content: "c", SendToServer: true}}},
	}

	for _, c := range chats {
		if err := SaveChat(c); err != nil {
			t.Fatalf("SaveChat failed: %v", err)
		}
	}

	listed, err := ListChats()
	if err != nil {
		t.Fatalf("ListChats failed: %v", err)
	}
	if len(listed) != 3 {
		t.Errorf("expected 3 chats, got %d", len(listed))
	}
}

func TestDeleteChat(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{Title: "to_delete", Messages: []model.Message{
		{Role: "user", Content: "bye", SendToServer: true},
	}}
	if err := SaveChat(chat); err != nil {
		t.Fatalf("SaveChat failed: %v", err)
	}

	// Verify exists
	path := filepath.Join(chatsDir, "to_delete.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("chat file should exist before delete")
	}

	err := DeleteChat("to_delete")
	if err != nil {
		t.Fatalf("DeleteChat failed: %v", err)
	}

	// Original path should no longer exist
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("chat file should not exist at original path after delete")
	}

	// Should exist in recycler
	recyclerPath := filepath.Join(chatsDir, "recycler", "to_delete.json")
	if _, err := os.Stat(recyclerPath); os.IsNotExist(err) {
		t.Errorf("chat file should exist in recycler after delete")
	}
}

func TestDeleteChat_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	// DeleteChat non-existent should return error
	err := DeleteChat("nonexistent")
	if err == nil {
		t.Errorf("expected error for deleting nonexistent chat")
	}
}

func TestDupeChat(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	original := &model.Chat{
		Title: "original",
		Messages: []model.Message{
			{Role: "user", Content: "hello", SendToServer: true},
			{Role: "assistant", Content: "world", SendToServer: true,
				ReasoningContent: "think",
				ToolCalls: []model.ToolCall{
					{ID: "id1", Function: model.FunctionCall{Name: "tool_a", Arguments: "{}"}},
				},
			},
			{Role: "tool", ToolCallID: "id1", Name: "tool_a", Content: "result", SendToServer: true},
		},
	}
	if err := SaveChat(original); err != nil {
		t.Fatalf("SaveChat failed: %v", err)
	}

	duped, err := DupeChat("original")
	if err != nil {
		t.Fatalf("DupeChat failed: %v", err)
	}
	if duped.Title != "original_copy" {
		t.Errorf("expected title 'original_copy', got '%s'", duped.Title)
	}
	if len(duped.Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(duped.Messages))
	}

	// Original should still exist
	orig, err := GetChat("original")
	if err != nil {
		t.Fatalf("original should still exist: %v", err)
	}
	if orig.Title != "original" {
		t.Errorf("original title should stay 'original', got '%s'", orig.Title)
	}

	// Copy file should exist on disk
	copyPath := filepath.Join(chatsDir, "original_copy.json")
	if _, err := os.Stat(copyPath); os.IsNotExist(err) {
		t.Errorf("duped file should exist on disk")
	}
}

func TestRenameChat(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{Title: "old_name", Messages: []model.Message{
		{Role: "user", Content: "test", SendToServer: true},
	}}
	if err := SaveChat(chat); err != nil {
		t.Fatalf("SaveChat failed: %v", err)
	}

	err := RenameChat("old_name", "new_name")
	if err != nil {
		t.Fatalf("RenameChat failed: %v", err)
	}

	// Old file should not exist
	oldPath := filepath.Join(chatsDir, "old_name.json")
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old file should not exist after rename")
	}

	// New file should exist
	newPath := filepath.Join(chatsDir, "new_name.json")
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		t.Errorf("new file should exist after rename")
	}

	// Old name should not be retrievable
	_, err = GetChat("old_name")
	if err == nil {
		t.Errorf("old name should not be retrievable after rename")
	}

	// New name should be retrievable
	renamed, err := GetChat("new_name")
	if err != nil {
		t.Fatalf("new name should be retrievable: %v", err)
	}
	if renamed.Title != "new_name" {
		t.Errorf("chat content title should be 'new_name', got '%s'", renamed.Title)
	}
}

func TestRenameChat_OldNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	err := RenameChat("nonexistent", "anything")
	if err == nil {
		t.Errorf("expected error renaming nonexistent chat")
	}
}

func TestRenameChat_Conflict(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat1 := &model.Chat{Title: "chat_a", Messages: nil}
	chat2 := &model.Chat{Title: "chat_b", Messages: nil}
	SaveChat(chat1)
	SaveChat(chat2)
	defer func() {
		DeleteChat("chat_a")
		DeleteChat("chat_b")
	}()

	err := RenameChat("chat_a", "chat_b")
	if err == nil {
		t.Errorf("expected error renaming to existing chat name")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestRenameChat_SameName(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{Title: "same", Messages: nil}
	SaveChat(chat)
	defer DeleteChat("same")

	err := RenameChat("same", "same")
	if err != nil {
		t.Errorf("renaming to same name should succeed: %v", err)
	}
}

func TestChatExists(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	if ChatExists("anything") {
		t.Errorf("ChatExists should return false for non-existent chat")
	}

	SaveChat(&model.Chat{Title: "real", Messages: nil})
	defer DeleteChat("real")

	if !ChatExists("real") {
		t.Errorf("ChatExists should return true for existing chat")
	}
}

func TestRenameChat_SanitizedConflict(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	// Two titles that sanitize to different filenames but same title match
	SaveChat(&model.Chat{Title: "a:b", Messages: nil})
	SaveChat(&model.Chat{Title: "a_b", Messages: nil})
	defer func() {
		DeleteChat("a:b")
		DeleteChat("a_b")
	}()

	// Renaming "a:b" to "a_b" should fail because "a_b" already exists
	err := RenameChat("a:b", "a_b")
	if err == nil {
		t.Errorf("expected error renaming to existing title")
	}
}

func TestRenameChat_TitleTooLong(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{Title: "short", Messages: nil}
	SaveChat(chat)
	defer DeleteChat("short")

	longName := ""
	for i := 0; i < 201; i++ {
		longName += "x"
	}
	err := RenameChat("short", longName)
	if err == nil {
		t.Errorf("expected error for title > 200 chars")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Errorf("expected 'too long' error, got: %v", err)
	}

	// Original chat must still exist (no data loss)
	if !ChatExists("short") {
		t.Errorf("original chat should still exist after failed rename")
	}
}

func TestSaveChat_SanitizedFilename(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	// Titles with characters that are unsafe for filenames
	tests := []struct {
		title            string
		expectedFilename string
	}{
		{"chat with spaces", "chat with spaces.json"},
		{"a:b", "a_b.json"},
		{"normal_name", "normal_name.json"},
		{"2026-05-10 164721", "2026-05-10 164721.json"},
	}

	for _, tc := range tests {
		t.Run(tc.title, func(t *testing.T) {
			chat := &model.Chat{Title: tc.title, Messages: nil}
			if err := SaveChat(chat); err != nil {
				t.Fatalf("SaveChat failed: %v", err)
			}

			// File should exist (not check exact filename since spaces aren't replaced)
			retrieved, err := GetChat(tc.title)
			if err != nil {
				t.Fatalf("GetChat failed for %q: %v", tc.title, err)
			}
			if retrieved.Title != tc.title {
				t.Errorf("expected title %q, got %q", tc.title, retrieved.Title)
			}

			// Clean up
			DeleteChat(tc.title)
		})
	}
}

func TestRenameChat_UpdatesJSONTitle(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{Title: "test chat", Messages: []model.Message{
		{Role: "user", Content: "hello", SendToServer: true},
	}}
	if err := SaveChat(chat); err != nil {
		t.Fatalf("SaveChat failed: %v", err)
	}

	if err := RenameChat("test chat", "renamed chat"); err != nil {
		t.Fatalf("RenameChat failed: %v", err)
	}

	retrieved, err := GetChat("renamed chat")
	if err != nil {
		t.Fatalf("GetChat failed: %v", err)
	}
	if retrieved.Title != "renamed chat" {
		t.Errorf("expected title 'renamed chat', got '%s'", retrieved.Title)
	}

	// Old title should not be accessible
	_, err = GetChat("test chat")
	if err == nil {
		t.Errorf("old title should not be retrievable")
	}
}

func TestListChats_IgnoresNonJSON(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	// Ensure chats directory exists
	if err := os.MkdirAll(chatsDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// Create a non-JSON file
	os.WriteFile(filepath.Join(chatsDir, "notes.txt"), []byte("hello"), 0644)

	chat := &model.Chat{Title: "my_chat", Messages: nil}
	if err := SaveChat(chat); err != nil {
		t.Fatalf("SaveChat failed: %v", err)
	}

	chats, err := ListChats()
	if err != nil {
		t.Fatalf("ListChats failed: %v", err)
	}
	// Should only have the .json file
	found := false
	for _, c := range chats {
		if c.Title == "my_chat" {
			found = true
		}
	}
	if !found {
		t.Errorf("my_chat should be in list")
	}
	if len(chats) > 1 {
		t.Errorf("expected only 1 chat, got %d", len(chats))
	}
}

func TestSaveChat_ProducesValidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{
		Title: "with_special",
		Messages: []model.Message{
			{},
			{Role: "user", Content: "hello\nworld", SendToServer: true},
			{Role: "assistant", Content: ""},
		},
	}

	if err := SaveChat(chat); err != nil {
		t.Fatalf("SaveChat failed: %v", err)
	}

	_, err := GetChat("with_special")
	if err != nil {
		t.Fatalf("GetChat failed: %v", err)
	}
}

func TestSaveChat_Overwrite(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{Title: "overwrite_test", Messages: []model.Message{
		{Role: "user", Content: "v1", SendToServer: true},
	}}
	SaveChat(chat)

	// Overwrite
	chat.Messages = append(chat.Messages, model.Message{Role: "user", Content: "v2", SendToServer: true})
	if err := SaveChat(chat); err != nil {
		t.Fatalf("SaveChat overwrite failed: %v", err)
	}

	retrieved, _ := GetChat("overwrite_test")
	if len(retrieved.Messages) != 2 {
		t.Errorf("expected 2 messages after overwrite, got %d", len(retrieved.Messages))
	}
}

func TestListChats_CorruptFiles(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	if err := os.MkdirAll(chatsDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// Write corrupt JSON
	os.WriteFile(filepath.Join(chatsDir, "corrupt.json"), []byte("not json"), 0644)

	chat := &model.Chat{Title: "good", Messages: nil}
	SaveChat(chat)

	chats, err := ListChats()
	if err != nil {
		t.Fatalf("ListChats failed: %v", err)
	}
	found := false
	for _, c := range chats {
		if c.Title == "good" {
			found = true
		}
	}
	if !found {
		t.Errorf("good chat should be in list even with corrupt file")
	}
	// Corrupt should be skipped
	for _, c := range chats {
		if c.Title == "corrupt" {
			t.Errorf("corrupt chat should be skipped")
		}
	}
}

func TestDupeChat_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	_, err := DupeChat("nonexistent")
	if err == nil {
		t.Errorf("expected error duplicating nonexistent chat")
	}
}
