package storage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hschat/internal/model"
)

// chdirTmp moves the test into a fresh temp dir (the store's paths are
// relative to the working directory) and restores it afterwards.
func chdirTmp(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })
}

func TestChatCache_ServesRepeatedReadsFromMemory(t *testing.T) {
	chdirTmp(t)

	chat := &model.Chat{Title: "cached", Messages: []model.Message{
		{Role: "user", Content: "hello", SendToServer: true},
	}}
	if err := SaveChat(chat); err != nil {
		t.Fatalf("SaveChat: %v", err)
	}
	path := filepath.Join(chatsDir, "cached.json")

	// Prime the cache, then swap the file behind the cache's back with
	// same-size content and the SAME mtime: a read now must still return
	// the cached (unmodified) content, proving no disk re-read happened.
	if _, err := GetChat("cached"); err != nil {
		t.Fatalf("GetChat: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stale := bytes.Replace(orig, []byte("hello"), []byte("STALE"), 1) // same length
	if len(stale) != len(orig) {
		t.Fatalf("test setup: stale content must keep the file size (%d vs %d)", len(stale), len(orig))
	}
	if err := os.WriteFile(path, stale, 0644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(path, info.ModTime(), info.ModTime())

	second, err := GetChat("cached")
	if err != nil {
		t.Fatalf("GetChat (cached): %v", err)
	}
	if second.Messages[0].Content != "hello" {
		t.Fatalf("cache did not serve the unmodified entry: got %q", second.Messages[0].Content)
	}
}

func TestChatCache_InvalidatedByModification(t *testing.T) {
	chdirTmp(t)

	if err := SaveChat(&model.Chat{Title: "watched", Messages: []model.Message{
		{Role: "user", Content: "v1", SendToServer: true},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := GetChat("watched"); err != nil {
		t.Fatal(err)
	}

	// External edit with a clearly newer mtime (and identical size):
	// size alone must not keep the stale entry alive.
	path := filepath.Join(chatsDir, "watched.json")
	newer := []byte(`{"title":"watched","messages":[{"role":"user","content":"v2","send_to_server":true}]}`)
	if err := os.WriteFile(path, newer, 0644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	os.Chtimes(path, future, future)

	got, err := GetChat("watched")
	if err != nil {
		t.Fatal(err)
	}
	if got.Messages[0].Content != "v2" {
		t.Fatalf("modified file was served stale: got %q", got.Messages[0].Content)
	}
}

func TestChatCache_SaveChatWriteThrough(t *testing.T) {
	chdirTmp(t)

	if err := SaveChat(&model.Chat{Title: "writethru", Messages: []model.Message{
		{Role: "user", Content: "before", SendToServer: true},
	}}); err != nil {
		t.Fatal(err)
	}
	// Prime the cache so only the write-through refresh can update it.
	if _, err := GetChat("writethru"); err != nil {
		t.Fatal(err)
	}
	if err := SaveChat(&model.Chat{Title: "writethru", Messages: []model.Message{
		{Role: "user", Content: "after", SendToServer: true},
	}}); err != nil {
		t.Fatal(err)
	}

	got, err := GetChat("writethru")
	if err != nil {
		t.Fatal(err)
	}
	if got.Messages[0].Content != "after" {
		t.Fatalf("SaveChat write-through failed: got %q", got.Messages[0].Content)
	}
}

func TestChatCache_DeleteDropsEntry(t *testing.T) {
	chdirTmp(t)

	if err := SaveChat(&model.Chat{Title: "gone", Messages: nil}); err != nil {
		t.Fatal(err)
	}
	if _, err := GetChat("gone"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteChat("gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := GetChat("gone"); err == nil {
		t.Fatal("deleted chat still resolvable (stale cache entry?)")
	}
}

func TestChatCache_RenameRefreshesBothPaths(t *testing.T) {
	chdirTmp(t)

	if err := SaveChat(&model.Chat{Title: "oldname", Messages: []model.Message{
		{Role: "user", Content: "hi", SendToServer: true},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := GetChat("oldname"); err != nil {
		t.Fatal(err)
	}
	if err := RenameChat("oldname", "newname"); err != nil {
		t.Fatalf("RenameChat: %v", err)
	}

	if _, err := GetChat("oldname"); err == nil {
		t.Fatal("old title should no longer resolve after rename")
	}
	got, err := GetChat("newname")
	if err != nil {
		t.Fatalf("GetChat(newname): %v", err)
	}
	if got.Title != "newname" || got.Messages[0].Content != "hi" {
		t.Fatalf("renamed chat content wrong: %+v", got)
	}
}

func TestChatCache_ReturnedChatIsNotShared(t *testing.T) {
	chdirTmp(t)

	if err := SaveChat(&model.Chat{Title: "copyme", Messages: []model.Message{
		{Role: "user", Content: "original", SendToServer: true},
	}}); err != nil {
		t.Fatal(err)
	}

	a, err := GetChat("copyme")
	if err != nil {
		t.Fatal(err)
	}
	a.Messages[0].Content = "mutated"
	a.Title = "mutated"

	b, err := GetChat("copyme")
	if err != nil {
		t.Fatal(err)
	}
	if b.Messages[0].Content != "original" || b.Title != "copyme" {
		t.Fatalf("GetChat returned shared state: %+v", b)
	}
}

func TestChatCache_ListChatsSeesExternalChanges(t *testing.T) {
	chdirTmp(t)

	if err := SaveChat(&model.Chat{Title: "listed", Messages: nil}); err != nil {
		t.Fatal(err)
	}
	if _, err := ListChats(); err != nil {
		t.Fatal(err)
	}

	// Add another chat file behind the store's back; ListChats reads the
	// directory fresh every time and must see it.
	data, _ := json.Marshal(&model.Chat{Title: "external", Messages: nil})
	if err := os.WriteFile(filepath.Join(chatsDir, "external.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	chats, err := ListChats()
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]bool{}
	for _, c := range chats {
		titles[c.Title] = true
	}
	if !titles["listed"] || !titles["external"] {
		t.Fatalf("ListChats missed files: %v", titles)
	}
}
