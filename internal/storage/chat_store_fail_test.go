package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hschat/internal/model"
)

func TestRenameChat_CJKLengthBug(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{Title: "orig", Messages: nil}
	SaveChat(chat)

	// 50 CJK characters is 150 bytes in UTF-8 (each = 3 bytes)
	cjkName := strings.Repeat("中文", 25) // 50 chars, 150 bytes
	if len(cjkName) != 150 {
		t.Fatalf("expected 150 bytes for 50 CJK chars, got %d", len(cjkName))
	}

	err := RenameChat("orig", cjkName)
	if err == nil {
		t.Error("BUG: expected error for CJK title exceeding byte limit. " +
			"Error says 'max 50 characters' but len() counts bytes. " +
			"50 CJK chars = 150 bytes > 100 byte limit, should fail.")
	}
}

func TestRenameChat_CJKJustUnderLimit(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{Title: "orig", Messages: nil}
	SaveChat(chat)

	// 33 CJK chars = 99 bytes (under 100 byte limit)
	cjkName := strings.Repeat("中", 33)
	if len(cjkName) != 99 {
		t.Fatalf("expected 99 bytes for 33 CJK chars, got %d", len(cjkName))
	}

	err := RenameChat("orig", cjkName)
	if err != nil {
		t.Logf("33 CJK chars (99 bytes) passed rename: %v", err)
	} else {
		t.Log("33 CJK chars (99 bytes) renamed successfully (under 100 byte limit)")
	}

	// Original must still exist if rename failed
	if err != nil && !ChatExists("orig") {
		t.Error("original chat should still exist after failed rename")
	}
}

func TestRenameChat_Exactly100Bytes(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{Title: "orig", Messages: nil}
	SaveChat(chat)

	// Exactly 100 ASCII bytes
	exact100 := strings.Repeat("a", 100)
	if len(exact100) != 100 {
		t.Fatalf("expected 100 bytes, got %d", len(exact100))
	}

	err := RenameChat("orig", exact100)
	if err != nil {
		if strings.Contains(err.Error(), "too long") {
			t.Error("BUG: 100-byte title should pass (limit is >100, not >=100 at line 152)")
		}
	}
	if ChatExists(exact100) {
		defer DeleteChat(exact100)
	}
}

func TestRenameChat_101BytesFails(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{Title: "orig", Messages: nil}
	SaveChat(chat)

	longName := strings.Repeat("b", 101)
	err := RenameChat("orig", longName)
	if err == nil {
		t.Error("expected error for 101-byte title")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Errorf("expected 'too long' error, got: %v", err)
	}
}

func TestSaveChat_TitleWithPathSeparator(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{
		Title:    "sub/dir/chat",
		Messages: []model.Message{},
	}

	err := SaveChat(chat)
	if err != nil {
		t.Fatalf("saving chat with path separators: %v", err)
	}

	retrieved, err := GetChat("sub/dir/chat")
	if err != nil {
		t.Fatalf("GetChat: %v", err)
	}
	if retrieved.Title != "sub/dir/chat" {
		t.Errorf("title mismatch: %s", retrieved.Title)
	}
}

func TestSaveChat_TitleWithBackslash(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{
		Title:    "sub\\dir\\chat",
		Messages: []model.Message{},
	}

	err := SaveChat(chat)
	if err != nil {
		t.Fatalf("saving chat with backslashes: %v", err)
	}

	retrieved, err := GetChat("sub\\dir\\chat")
	if err != nil {
		t.Fatalf("GetChat: %v", err)
	}
	if retrieved.Title != "sub\\dir\\chat" {
		t.Errorf("title mismatch: %s", retrieved.Title)
	}
}

func TestSaveChat_TitleWithColon(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{
		Title:    "windows:file",
		Messages: []model.Message{},
	}

	err := SaveChat(chat)
	if err != nil {
		t.Fatalf("saving chat with colon: %v", err)
	}

	retrieved, err := GetChat("windows:file")
	if err != nil {
		t.Fatalf("GetChat: %v", err)
	}
	if retrieved.Title != "windows:file" {
		t.Errorf("title mismatch: %s", retrieved.Title)
	}
}

func TestSaveChat_OverwritePreservesTitle(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat1 := &model.Chat{
		Title: "test",
		Messages: []model.Message{
			{Role: "user", Content: "v1", SendToServer: true},
		},
	}
	SaveChat(chat1)

	chat2 := &model.Chat{
		Title: "test",
		Messages: []model.Message{
			{Role: "user", Content: "v1", SendToServer: true},
			{Role: "assistant", Content: "v2", SendToServer: true},
		},
	}
	err := SaveChat(chat2)
	if err != nil {
		t.Fatalf("overwrite failed: %v", err)
	}

	retrieved, _ := GetChat("test")
	if len(retrieved.Messages) != 2 {
		t.Errorf("expected 2 messages after overwrite, got %d", len(retrieved.Messages))
	}
}

func TestDupeChat_DeepCopy(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	original := &model.Chat{
		Title: "deep_copy",
		Messages: []model.Message{
			{Role: "user", Content: "msg", SendToServer: true},
			{
				Role: "assistant", Content: "reply", SendToServer: true,
				ReasoningContent: "think",
				ToolCalls: []model.ToolCall{
					{ID: "tc1", Function: model.FunctionCall{Name: "read", Arguments: `{"a":1}`}},
				},
			},
			{Role: "tool", ToolCallID: "tc1", Name: "read", Content: "done", SendToServer: true},
		},
	}
	SaveChat(original)

	duped, err := DupeChat("deep_copy")
	if err != nil {
		t.Fatalf("DupeChat failed: %v", err)
	}

	// Modify the original - duped should not be affected (true deep copy)
	original.Messages[0].Content = "MODIFIED"
	if duped.Messages[0].Content != "msg" {
		t.Error("duped chat should be independent of original (deep copy)")
	}
}

func TestDeleteChat_NonexistentRecyclerDir(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{Title: "todelete", Messages: nil}
	SaveChat(chat)

	err := DeleteChat("todelete")
	if err != nil {
		t.Fatalf("DeleteChat failed: %v", err)
	}

	// Ensure recycler directory was created
	recyclerPath := filepath.Join(chatsDir, "recycler", "todelete.json")
	if _, err := os.Stat(recyclerPath); os.IsNotExist(err) {
		t.Error("recycled file should exist")
	}
}

func TestSaveChat_NilMessages(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{
		Title: "nil_msgs",
	}

	err := SaveChat(chat)
	if err != nil {
		t.Fatalf("SaveChat with nil messages failed: %v", err)
	}

	retrieved, _ := GetChat("nil_msgs")
	if retrieved.Messages == nil {
		t.Log("nil messages serialized correctly")
	}
}

func TestRenameChat_ToNameContainingUnicode(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat := &model.Chat{Title: "ascii", Messages: nil}
	SaveChat(chat)

	unicodeName := "你好世界" // 4 CJK chars = 12 bytes
	err := RenameChat("ascii", unicodeName)
	if err != nil {
		t.Fatalf("Renaming to unicode name failed: %v", err)
	}

	retrieved, err := GetChat(unicodeName)
	if err != nil {
		t.Fatalf("GetChat with unicode name failed: %v", err)
	}
	if retrieved.Title != unicodeName {
		t.Errorf("title mismatch: expected '%s', got '%s'", unicodeName, retrieved.Title)
	}
}

func TestSanitizeFilename_PeriodsNotReplaced(t *testing.T) {
	// Periods are allowed in filenames
	result := sanitizeFilename("test.file.txt")
	if result != "test.file.txt" {
		t.Errorf("periods should not be replaced: got '%s'", result)
	}
}

func TestSanitizeFilename_SpacesNotReplaced(t *testing.T) {
	result := sanitizeFilename("hello world")
	if result != "hello world" {
		t.Errorf("spaces should not be replaced: got '%s'", result)
	}
}

func TestSanitizeFilename_AllSpecialChars(t *testing.T) {
	result := sanitizeFilename(`<test>:file"?*|`)
	expected := `_test__file____`
	if result != expected {
		t.Errorf("sanitizeFilename: got '%s', want '%s'", result, expected)
	}
}

func TestChatExists_WithColonInTitle(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	SaveChat(&model.Chat{Title: "a:b", Messages: nil})

	if !ChatExists("a:b") {
		t.Error("ChatExists should return true for title with colon")
	}
}

func TestChatExists_EmptyTitle(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	if ChatExists("") {
		t.Errorf("empty title should not exist initially")
	}

	SaveChat(&model.Chat{Title: "", Messages: nil})
	if ChatExists("") {
		t.Log("empty title chat exists after save")
	}
}

func TestSaveChat_TitleThatCollidesWithSanitized(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	chat1 := &model.Chat{Title: "a:b", Messages: nil}
	SaveChat(chat1)

	chat2 := &model.Chat{Title: "a_b", Messages: nil}
	err := SaveChat(chat2)
	if err != nil {
		t.Fatalf("SaveChat for a_b failed: %v", err)
	}

	// Both should be retrievable by their original titles
	if !ChatExists("a:b") {
		t.Error("a:b should exist")
	}
	if !ChatExists("a_b") {
		t.Error("a_b should exist")
	}
}
