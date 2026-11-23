package server

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hschat/internal/model"
	"hschat/internal/storage"
)

// TestHandlerDeflateEndToEnd drives the real Handler() chain (deflate →
// auth → mux) the way a browser would: Accept-Encoding carrying deflate
// plus a large chat document must come back Content-Encoding: deflate,
// decodable to the identical JSON.
func TestHandlerDeflateEndToEnd(t *testing.T) {
	setupServerTest(t)
	big := strings.Repeat("the quick brown fox jumps over the lazy dog. ", 40)
	if err := storage.SaveChat(&model.Chat{
		Title: "bigchat",
		Messages: []model.Message{
			{Role: "user", Content: big, SendToServer: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	srv := New(testStaticFS)

	req := httptest.NewRequest(http.MethodGet, "/api/chats/bigchat", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if enc := w.Header().Get("Content-Encoding"); enc != "deflate" {
		t.Fatalf("Content-Encoding = %q, want deflate", enc)
	}

	zr, err := zlib.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("body not zlib framed: %v", err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	var chat model.Chat
	if err := json.Unmarshal(plain, &chat); err != nil {
		t.Fatalf("decompressed body is not the chat JSON: %v", err)
	}
	if chat.Title != "bigchat" || chat.Messages[0].Content != big {
		t.Fatalf("round-trip mismatch: %+v", chat)
	}
	if w.Body.Len() >= len(plain) {
		t.Errorf("no compression win: wire %d vs raw %d bytes", w.Body.Len(), len(plain))
	}

	// Same request without Accept-Encoding must come back raw and equal.
	req2 := httptest.NewRequest(http.MethodGet, "/api/chats/bigchat", nil)
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, req2)
	if enc := w2.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("Content-Encoding = %q, want none without Accept-Encoding", enc)
	}
	if !strings.Contains(w2.Body.String(), "bigchat") || !strings.Contains(w2.Body.String(), big[:30]) {
		t.Fatalf("raw body looks wrong: %s", w2.Body.String()[:80])
	}
}
