package server

import (
	"bytes"
	"compress/zlib"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func doDeflate(t *testing.T, accept string, next http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/chats", nil)
	if accept != "" {
		req.Header.Set("Accept-Encoding", accept)
	}
	w := httptest.NewRecorder()
	withDeflate(next).ServeHTTP(w, req)
	return w
}

// bigBody is comfortably above deflateMinLength so the middleware commits
// to compression.
var bigBody = strings.Repeat(`{"role":"user","content":"hello deflated world"},`, 64)

func TestDeflateCompressesLargeJSON(t *testing.T) {
	w := doDeflate(t, "gzip, deflate, br", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, bigBody)
	}))

	if enc := w.Header().Get("Content-Encoding"); enc != "deflate" {
		t.Fatalf("Content-Encoding = %q, want deflate", enc)
	}
	zr, err := zlib.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("body is not zlib/deflate framed: %v", err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if string(plain) != bigBody {
		t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(plain), len(bigBody))
	}
	if !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding") {
		t.Errorf("Vary header missing Accept-Encoding: %q", w.Header().Get("Vary"))
	}
	if w.Body.Len() >= len(bigBody) {
		t.Errorf("compression did not shrink the body: %d -> %d", len(bigBody), w.Body.Len())
	}
}

func TestDeflateSkipsSmallBodies(t *testing.T) {
	w := doDeflate(t, "deflate", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))

	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("small body got Content-Encoding %q, want none", enc)
	}
	if w.Body.String() != `{"ok":true}` {
		t.Fatalf("body = %q, want raw passthrough", w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding") {
		t.Errorf("Vary header missing Accept-Encoding: %q", w.Header().Get("Vary"))
	}
}

func TestDeflateSkipsEventStream(t *testing.T) {
	w := doDeflate(t, "gzip, deflate", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		io.WriteString(w, "event: sync\ndata: "+strings.Repeat("x", 4*deflateMinLength)+"\n\n")
		w.(http.Flusher).Flush()
	}))

	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("SSE got Content-Encoding %q, want none", enc)
	}
	if !strings.HasPrefix(w.Body.String(), "event: sync\n") {
		t.Fatalf("SSE body was mangled: %q", w.Body.String()[:40])
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestDeflateNotAccepted(t *testing.T) {
	w := doDeflate(t, "gzip", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, bigBody)
	}))

	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("Content-Encoding = %q, want none for gzip-only client", enc)
	}
	if w.Body.String() != bigBody {
		t.Fatal("body should pass through untouched")
	}
}

func TestDeflateQZeroRejected(t *testing.T) {
	w := doDeflate(t, "gzip, deflate;q=0", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, bigBody)
	}))

	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("Content-Encoding = %q, want none for deflate;q=0", enc)
	}
}

func TestDeflateSkipsBinaryContent(t *testing.T) {
	body := bytes.Repeat([]byte{0x00, 0xFF, 0x7F}, 2*deflateMinLength/3)
	w := doDeflate(t, "deflate", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon")
		w.Write(body)
	}))

	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("binary body got Content-Encoding %q, want none", enc)
	}
	if !bytes.Equal(w.Body.Bytes(), body) {
		t.Fatal("body should pass through untouched")
	}
}

func TestDeflateSniffsUndecoratedBodies(t *testing.T) {
	// No Content-Type set: the middleware must sniff like net/http does
	// and still compress a large HTML-ish body.
	w := doDeflate(t, "deflate", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "<html><body>"+strings.Repeat("sniff ", 300)+"</body></html>")
	}))

	if enc := w.Header().Get("Content-Encoding"); enc != "deflate" {
		t.Fatalf("Content-Encoding = %q, want deflate (sniffed text/html)", enc)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want sniffed text/html", ct)
	}
}

func TestDeflatePreservesErrorStatus(t *testing.T) {
	w := doDeflate(t, "deflate", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":"chat not found"}`)
	}))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if w.Body.String() != `{"error":"chat not found"}` {
		t.Fatalf("small error body should stay raw, got %q", w.Body.String())
	}
}

func TestDeflateLargeBodyAcrossManyWrites(t *testing.T) {
	// One big flush-worthy body delivered in many small writes (how
	// json.Encoder + chunked handlers behave) must reassemble exactly.
	w := doDeflate(t, "deflate", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for i := 0; i < 100; i++ {
			io.WriteString(w, bigBody[:200])
		}
	}))

	if enc := w.Header().Get("Content-Encoding"); enc != "deflate" {
		t.Fatalf("Content-Encoding = %q, want deflate", enc)
	}
	zr, err := zlib.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("not zlib framed: %v", err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if string(plain) != strings.Repeat(bigBody[:200], 100) {
		t.Fatal("multi-write round-trip mismatch")
	}
}

func TestDeflateHeadRequestUncompressed(t *testing.T) {
	req := httptest.NewRequest(http.MethodHead, "/api/chats", nil)
	req.Header.Set("Accept-Encoding", "deflate")
	w := httptest.NewRecorder()
	withDeflate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, bigBody)
	})).ServeHTTP(w, req)

	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("HEAD got Content-Encoding %q, want none", enc)
	}
}
