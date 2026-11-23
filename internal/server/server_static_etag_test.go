package server

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// etagTestFiles is a stand-in web tree with the paths the static
// handlers actually look up. style.css is padded past the deflate
// threshold so the full-chain test exercises compression too.
var etagTestFiles = map[string]string{
	"web/index.html":     "<!doctype html><title>app __VERSION__</title>",
	"web/login.html":     "<!doctype html><title>login</title>",
	"web/css/style.css":  strings.Repeat("body{margin:0}\n", 64),
	"web/js/app.js":      "console.log('app');",
	"assets/favicon.ico": "\x00\x00\x01\x00\x00\x01fake-icon",
}

// newStaticServer boots a Server whose static tree is a real on-disk FS
// instead of an embed, so handleStatic serves actual bytes and the ETag
// machinery can be driven end to end.
func newStaticServer(t *testing.T, files map[string]string) *Server {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	setupServerTest(t)
	return newServer(os.DirFS(dir))
}

func get(t *testing.T, srv *Server, target, ifNoneMatch string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	return w
}

// A first hit serves the asset with an ETag equal to the quoted MD5 of
// the body, plus the no-cache directive that keeps later reuses
// conditional.
func TestStaticETag_FirstHitETagIsBodyMD5(t *testing.T) {
	srv := newStaticServer(t, etagTestFiles)

	w := get(t, srv, "/web/js/app.js", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	sum := md5.Sum(w.Body.Bytes())
	want := `"` + hex.EncodeToString(sum[:]) + `"`
	if got := w.Header().Get("ETag"); got != want {
		t.Fatalf("ETag = %q, want MD5-of-body %q", got, want)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type = %q, want a javascript type", ct)
	}
}

// The second request with the stored ETag comes back body-less 304.
func TestStaticETag_IfNoneMatchYields304(t *testing.T) {
	srv := newStaticServer(t, etagTestFiles)

	etag := get(t, srv, "/web/css/style.css", "").Header().Get("ETag")
	w := get(t, srv, "/web/css/style.css", etag)
	if w.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("304 carries a body of %d bytes", w.Body.Len())
	}
	// RFC 7232 §4.1: the validator must be repeated on the 304 so the
	// cache can confirm which representation it hit.
	if got := w.Header().Get("ETag"); got != etag {
		t.Fatalf("304 ETag = %q, want %q", got, etag)
	}
}

// A stale ETag (content rebuilt, or a lying client) must fall back to a
// full 200 with the body.
func TestStaticETag_StaleIfNoneMatchServesBody(t *testing.T) {
	srv := newStaticServer(t, etagTestFiles)

	w := get(t, srv, "/web/js/app.js", `"00000000000000000000000000000000"`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "console.log") {
		t.Fatalf("body missing after stale revalidation: %q", w.Body.String())
	}
}

// "*" matches any current representation; lists are scanned per RFC
// 7232 §2.3 with weak comparison (W/ ignored).
func TestEtagMatches(t *testing.T) {
	etag := `"abc123def"`
	for _, tc := range []struct {
		header string
		want   bool
	}{
		{"", false},
		{`"other"`, false},
		{etag, true},
		{`W/` + etag, true},
		{`"zzz", ` + etag, true},
		{`W/"zzz", W/` + etag, true},
		{etag + `, "def"`, true},
		{"*", true},
		{"   *   ", true},
	} {
		if got := etagMatches(tc.header, etag); got != tc.want {
			t.Errorf("etagMatches(%q, %s) = %v, want %v", tc.header, etag, got, tc.want)
		}
	}
}

func TestStaticETag_StarAlwaysNotModified(t *testing.T) {
	srv := newStaticServer(t, etagTestFiles)

	if w := get(t, srv, "/web/js/app.js", "*"); w.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304 for If-None-Match: *", w.Code)
	}
}

// index.html is served version-substituted, so its ETag must hash the
// bytes on the wire (placeholder replaced), and both "/" and the direct
// path must share one cache entry.
func TestStaticETag_IndexHTMLHashesServedBytes(t *testing.T) {
	srv := newStaticServer(t, etagTestFiles)

	root := get(t, srv, "/", "")
	if root.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", root.Code)
	}
	if strings.Contains(root.Body.String(), versionPlaceholder) {
		t.Fatalf("served index.html still contains the raw placeholder: %q", root.Body.String())
	}
	sum := md5.Sum(root.Body.Bytes())
	want := `"` + hex.EncodeToString(sum[:]) + `"`
	if got := root.Header().Get("ETag"); got != want {
		t.Fatalf("ETag = %q, want MD5 of served (substituted) bytes %q", got, want)
	}
	direct := get(t, srv, "/web/index.html", root.Header().Get("ETag"))
	if direct.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304: / and /web/index.html must share the ETag", direct.Code)
	}
}

func TestStaticETag_Favicon(t *testing.T) {
	srv := newStaticServer(t, etagTestFiles)

	w := get(t, srv, "/favicon.ico", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("favicon served without ETag")
	}
	if w2 := get(t, srv, "/favicon.ico", etag); w2.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304 on revalidation", w2.Code)
	}
}

// The login page is a static asset too; it must revalidate the same way
// (auth-exempt, reachable without credentials).
func TestStaticETag_LoginPage(t *testing.T) {
	srv := newStaticServer(t, etagTestFiles)

	w := get(t, srv, "/login", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("login page served without ETag")
	}
	if w2 := get(t, srv, "/login", etag); w2.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304 on revalidation", w2.Code)
	}
}

// Through the full Handler() chain (deflate → auth → mux) a revalidation
// of a compressible asset must yield a bare 304: no body, and no
// Content-Encoding claimed for a response that carries nothing to
// decode.
func TestStaticETag_NotModifiedThroughDeflateChain(t *testing.T) {
	srv := newStaticServer(t, etagTestFiles)

	req := httptest.NewRequest(http.MethodGet, "/web/css/style.css", nil)
	req.Header.Set("Accept-Encoding", "deflate")
	first := httptest.NewRecorder()
	srv.Handler().ServeHTTP(first, req)
	if enc := first.Header().Get("Content-Encoding"); enc != "deflate" {
		t.Fatalf("first hit Content-Encoding = %q, want deflate", enc)
	}
	etag := first.Header().Get("ETag")

	req2 := httptest.NewRequest(http.MethodGet, "/web/css/style.css", nil)
	req2.Header.Set("Accept-Encoding", "deflate")
	req2.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	srv.Handler().ServeHTTP(second, req2)
	if second.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Fatalf("304 carries a body of %d bytes", second.Body.Len())
	}
	if enc := second.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("304 Content-Encoding = %q, want none", enc)
	}
	if got := second.Header().Get("ETag"); got != etag {
		t.Fatalf("304 ETag = %q, want %q", got, etag)
	}
}

// Concurrent first hits on a cold entry must all agree on the digest.
func TestStaticETag_ConcurrentFirstAccess(t *testing.T) {
	srv := newStaticServer(t, etagTestFiles)

	const n = 16
	etags := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := httptest.NewRecorder()
			srv.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/web/js/app.js", nil))
			etags[i] = w.Header().Get("ETag")
		}(i)
	}
	wg.Wait()
	for i, e := range etags {
		if e == "" || e != etags[0] {
			t.Fatalf("etag %d = %q, want the shared %q", i, e, etags[0])
		}
	}
}
