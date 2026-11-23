package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"hschat/internal/model"
)

const chatsDir = "chats"

// chatCache is a read-through content cache for chat files, keyed by the
// file's absolute path. An entry stays valid while the file's
// modification time and size are unchanged; every write path in this
// file refreshes or invalidates the affected entries, so external edits
// (different mtime/size) and internal saves both converge to fresh data
// while unmodified files skip the disk read entirely.
var chatCache = struct {
	sync.RWMutex
	entries map[string]chatCacheEntry
}{entries: make(map[string]chatCacheEntry)}

// chatCacheEntry is the cached <modification time, size, content> triple
// for one chat file.
type chatCacheEntry struct {
	modTime time.Time
	size    int64
	data    []byte
}

// chatCacheKey makes the cache key for a chat path. Absolute on purpose:
// the store's paths are relative to the process working directory (and
// tests chdir freely), so a bare relative path could alias different
// files across directories.
func chatCacheKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// readChatFile returns the content of a chat file, served from chatCache
// when its modification time and size match the cached entry — an
// unmodified file is never read from disk again. The returned bytes are
// shared with the cache and must be treated as read-only (all callers
// only unmarshal them).
func readChatFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		// The file is gone (deleted or renamed away): drop any stale
		// entry so it cannot shadow a future file at the same path.
		invalidateChatCache(path)
		return nil, err
	}

	key := chatCacheKey(path)
	chatCache.RLock()
	entry, ok := chatCache.entries[key]
	chatCache.RUnlock()
	if ok && entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) {
		return entry.data, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	chatCache.Lock()
	chatCache.entries[key] = chatCacheEntry{modTime: info.ModTime(), size: info.Size(), data: data}
	chatCache.Unlock()
	return data, nil
}

// refreshChatCache stores freshly written content: called right after a
// successful write of data to path so the next read is served from
// memory without touching the disk again. If the post-write stat fails
// the entry is dropped instead (next read goes back to disk).
func refreshChatCache(path string, data []byte) {
	key := chatCacheKey(path)
	info, err := os.Stat(path)
	chatCache.Lock()
	defer chatCache.Unlock()
	if err != nil {
		delete(chatCache.entries, key)
		return
	}
	chatCache.entries[key] = chatCacheEntry{modTime: info.ModTime(), size: info.Size(), data: data}
}

// invalidateChatCache drops the cached content of the given paths; every
// remove/rename path calls it because the old content no longer belongs
// to the path.
func invalidateChatCache(paths ...string) {
	chatCache.Lock()
	for _, p := range paths {
		delete(chatCache.entries, chatCacheKey(p))
	}
	chatCache.Unlock()
}

func sanitizeFilename(title string) string {
	repl := strings.NewReplacer(
		"<", "_", ">", "_", ":", "_", "\"", "_",
		"/", "_", "\\", "_", "|", "_", "?", "_", "*", "_",
	)
	return repl.Replace(title)
}

func getSortedChatFiles(entries []os.DirEntry) []os.FileInfo {
	infos := make([]os.FileInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		if info, err := e.Info(); err == nil {
			infos = append(infos, info)
		}
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].ModTime().After(infos[j].ModTime())
	})
	return infos
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

	infos := getSortedChatFiles(entries)

	for _, info := range infos {
		data, err := readChatFile(filepath.Join(chatsDir, info.Name()))
		if err != nil {
			continue
		}
		var chat model.Chat
		if err := json.Unmarshal(data, &chat); err != nil {
			continue
		}
		if chat.Title == title {
			return filepath.Join(chatsDir, info.Name()), nil
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

	infos := getSortedChatFiles(entries)

	var chats []model.Chat
	for _, info := range infos {
		data, err := readChatFile(filepath.Join(chatsDir, info.Name()))
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
	data, err := readChatFile(path)
	if err != nil {
		return nil, err
	}
	// Unmarshal from the (possibly cached) bytes into a fresh object on
	// every call: callers mutate the returned chat before saving it, so
	// a shared parsed instance would corrupt the cache.
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

	sanitized := sanitizeFilename(chat.Title)
	newPath := filepath.Join(chatsDir, sanitized+".json")

	if oldPath, err := findChatFile(chat.Title); err == nil && oldPath != newPath {
		os.Remove(oldPath)
		invalidateChatCache(oldPath)
	}

	data, err := json.MarshalIndent(chat, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(newPath, data, 0644); err != nil {
		return err
	}
	// Write-through: keep the cache in step with what just hit the disk.
	refreshChatCache(newPath, data)
	return nil
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
	if err := os.Rename(path, dest); err != nil {
		return err
	}
	invalidateChatCache(path)
	return nil
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
		refreshChatCache(newPath, data)
		os.Remove(oldPath)
		invalidateChatCache(oldPath)
		return nil
	}

	// Same filename — just update the JSON in place
	data, err := json.MarshalIndent(chat, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(oldPath, data, 0644); err != nil {
		return err
	}
	refreshChatCache(oldPath, data)
	return nil
}
