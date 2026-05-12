package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hschat/internal/model"
)

const chatsDir = "chats"

func sanitizeFilename(title string) string {
	repl := strings.NewReplacer(
		"<", "_", ">", "_", ":", "_", "\"", "_",
		"/", "_", "\\", "_", "|", "_", "?", "_", "*", "_",
	)
	return repl.Replace(title)
}

func findChatFile(title string) (string, error) {
	sanitized := sanitizeFilename(title)
	path := filepath.Join(chatsDir, sanitized+".json")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	entries, err := os.ReadDir(chatsDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(chatsDir, e.Name()))
		if err != nil {
			continue
		}
		var chat model.Chat
		if err := json.Unmarshal(data, &chat); err != nil {
			continue
		}
		if chat.Title == title {
			return filepath.Join(chatsDir, e.Name()), nil
		}
	}
	return "", fmt.Errorf("chat not found: %s", title)
}

func ListChats() ([]model.Chat, error) {
	if err := os.MkdirAll(chatsDir, 0755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(chatsDir)
	if err != nil {
		return nil, err
	}
	var chats []model.Chat
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(chatsDir, e.Name()))
		if err != nil {
			continue
		}
		var chat model.Chat
		if err := json.Unmarshal(data, &chat); err != nil {
			continue
		}
		chats = append(chats, chat)
	}
	return chats, nil
}

func GetChat(title string) (*model.Chat, error) {
	path, err := findChatFile(title)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var chat model.Chat
	if err := json.Unmarshal(data, &chat); err != nil {
		return nil, err
	}
	return &chat, nil
}

func SaveChat(chat *model.Chat) error {
	if err := os.MkdirAll(chatsDir, 0755); err != nil {
		return err
	}

	oldPath, err := findChatFile(chat.Title)
	if err == nil {
		sanitized := sanitizeFilename(chat.Title)
		newPath := filepath.Join(chatsDir, sanitized+".json")
		if oldPath != newPath {
			os.Remove(oldPath)
		}
	}

	data, err := json.MarshalIndent(chat, "", "  ")
	if err != nil {
		return err
	}
	sanitized := sanitizeFilename(chat.Title)
	return os.WriteFile(filepath.Join(chatsDir, sanitized+".json"), data, 0644)
}

func DeleteChat(title string) error {
	path, err := findChatFile(title)
	if err != nil {
		return err
	}
	recyclerDir := filepath.Join(chatsDir, "recycler")
	if err := os.MkdirAll(recyclerDir, 0755); err != nil {
		return err
	}
	dest := filepath.Join(recyclerDir, filepath.Base(path))
	return os.Rename(path, dest)
}

func DupeChat(title string) (*model.Chat, error) {
	chat, err := GetChat(title)
	if err != nil {
		return nil, err
	}
	chat.Title = title + "_copy"
	if err := SaveChat(chat); err != nil {
		return nil, err
	}
	return chat, nil
}

func ChatExists(title string) bool {
	_, err := findChatFile(title)
	return err == nil
}

func RenameChat(oldTitle, newTitle string) error {
	if oldTitle == newTitle {
		return nil
	}

	if len(newTitle) > 100 {
		// cjk is counted len 2:1, so
		return fmt.Errorf("title too long (max 50 characters)")
	}

	// Check that old chat exists
	chat, err := GetChat(oldTitle)
	if err != nil {
		return err
	}

	// Check for conflict: new title must not already exist
	if ChatExists(newTitle) {
		return fmt.Errorf("a chat with that name already exists")
	}

	oldPath, err := findChatFile(oldTitle)
	if err != nil {
		return err
	}

	chat.Title = newTitle
	sanitized := sanitizeFilename(newTitle)
	newPath := filepath.Join(chatsDir, sanitized+".json")

	// Write new file first (safe), then remove old one
	if oldPath != newPath {
		data, err := json.MarshalIndent(chat, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(newPath, data, 0644); err != nil {
			return fmt.Errorf("cannot write renamed chat: %w", err)
		}
		os.Remove(oldPath)
		return nil
	}

	// Same filename — just update the JSON in place
	data, err := json.MarshalIndent(chat, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(oldPath, data, 0644)
}
