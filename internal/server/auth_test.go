package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hschat/internal/model"
	"hschat/internal/storage"
)

// doReq runs a request through the full handler chain (auth middleware
// included), like main.go's http.ListenAndServe does.
func doReq(srv *Server, method, path, body string, setup func(*http.Request)) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body == "" {
		rd = strings.NewReader("")
	} else {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if setup != nil {
		setup(req)
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func withBasic(user, pass string) func(*http.Request) {
	return func(r *http.Request) { r.SetBasicAuth(user, pass) }
}

func withCookieVal(val string) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: authCookieName, Value: val}) }
}

func loginFor(t *testing.T, srv *Server, password string) string {
	t.Helper()
	// The login endpoint is PoW-gated: peek the current exponent (same
	// package, so we can) and present it like a client that has solved
	// the challenge would.
	w := doReq(srv, "POST", "/api/login", fmt.Sprintf(`{"password":%q,"h":%d}`, password, powH(srv)), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == authCookieName {
			return c.Value
		}
	}
	t.Fatal("login response carried no auth cookie")
	return ""
}

func TestAuth_EmptyPasswordLetsEverythingThrough(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{AuthPassword: ""})

	srv := New(testStaticFS)

	if w := doReq(srv, "GET", "/api/mode", "", nil); w.Code != http.StatusOK {
		t.Errorf("expected 200 without credentials when no password is configured, got %d", w.Code)
	}
}

func TestAuth_EmptyPasswordIgnoresStaleCookie(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{AuthPassword: ""})

	srv := New(testStaticFS)

	// Stale credentials (e.g. a leftover cookie from a previous password)
	// must not lock the user out when auth is disabled.
	if w := doReq(srv, "GET", "/api/mode", "", withCookieVal("stale")); w.Code != http.StatusOK {
		t.Errorf("expected 200 with stale cookie when no password is configured, got %d", w.Code)
	}
}

func TestAuth_UnauthenticatedPageRedirectsToLogin(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{AuthPassword: "secret"})

	srv := New(testStaticFS)

	w := doReq(srv, "GET", "/", "", nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 to login page for unauthenticated visit, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "login" {
		t.Errorf("expected relative Location 'login', got %q", loc)
	}
}

func TestAuth_NestedPathRedirectClimbsToLogin(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{AuthPassword: "secret"})

	srv := New(testStaticFS)

	// A relative Location resolves by replacing the request path's last
	// segment, so the bounce must carry one "../" per leading segment —
	// otherwise deep links like /web/js/app.js land on /web/js/login
	// (a 404) instead of the login page.
	for _, tc := range []struct{ path, want string }{
		{"/", "login"},
		{"/login/", "../login"}, // trailing slash: falls to the catch-all, still gated
		{"/web/", "../login"},
		{"/web/js/app.js", "../../login"},
		{"/deep/a/b/c.css", "../../../login"},
	} {
		w := doReq(srv, "GET", tc.path, "", nil)
		if w.Code != http.StatusSeeOther {
			t.Errorf("%s: expected 303, got %d", tc.path, w.Code)
			continue
		}
		if loc := w.Header().Get("Location"); loc != tc.want {
			t.Errorf("%s: Location = %q, want %q", tc.path, loc, tc.want)
		}
	}
}

func TestAuth_UnauthenticatedAPIGetsJSON401(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{AuthPassword: "secret"})

	srv := New(testStaticFS)

	w := doReq(srv, "GET", "/api/mode", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated API call, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Errorf("expected JSON content type on API 401, got %q", w.Header().Get("Content-Type"))
	}
	if strings.Contains(w.Header().Get("WWW-Authenticate"), "Basic") {
		t.Error("API 401 must not carry a Basic challenge (it would trigger the per-port native prompt)")
	}
}

func TestAuth_BasicAuthRemoved(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{AuthPassword: "secret"})

	srv := New(testStaticFS)

	// Basic Auth used to be accepted for curl-style clients; it verified
	// the raw password on every request — a guessing oracle that cost no
	// discrete log, defeating the login PoW. Even the CORRECT password
	// in a Basic header must be rejected now; only the cookie counts.
	for _, user := range []string{"admin", "", "anything"} {
		if w := doReq(srv, "GET", "/api/mode", "", withBasic(user, "secret")); w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for basic auth (user %q) even with the correct password, got %d", user, w.Code)
		}
	}
	// Same for deliberately wrong passwords, of course.
	for _, pass := range []string{"", "Secret", "secre", "secrets", "secret\x00"} {
		if w := doReq(srv, "GET", "/api/mode", "", withBasic("user", pass)); w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for basic password %q, got %d", pass, w.Code)
		}
	}
}

func TestAuth_LoginPageServedExempt(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{AuthPassword: "secret"})

	srv := New(testStaticFS)
	srv.loginHTML = []byte("<html>login-page</html>")

	w := doReq(srv, "GET", "/login", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected login page without credentials, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "login-page") {
		t.Errorf("login page content mismatch: %s", w.Body.String())
	}
}

func TestAuth_LoginPageRedirectsWhenAuthenticated(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{AuthPassword: "secret"})

	srv := New(testStaticFS)
	srv.loginHTML = []byte("<html>login-page</html>")
	cookie := loginFor(t, srv, "secret")

	w := doReq(srv, "GET", "/login", "", withCookieVal(cookie))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 back to app for authenticated /login visit, got %d", w.Code)
	}
}

func TestAuth_LoginSubmitFlow(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{AuthPassword: "secret"})

	srv := New(testStaticFS)

	// No h at all -> pow rejection, no cookie, current seed attached.
	w := doReq(srv, "POST", "/api/login", `{"password":"secret"}`, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing pow, got %d %s", w.Code, w.Body.String())
	}
	var res struct {
		Error string    `json:"error"`
		Seed  loginSeed `json:"seed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("bad error body: %v", err)
	}
	if res.Error != "pow" || res.Seed.Challenge == "" {
		t.Errorf("expected pow error + seed, got %+v", res)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Error("failed login must not set a cookie")
	}

	// Correct h but wrong password -> password rejection; the h was
	// still consumed (rolled), and the failure carries the new seed.
	before := powH(srv)
	w = doReq(srv, "POST", "/api/login", fmt.Sprintf(`{"password":"nope","h":%d}`, before), nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d", w.Code)
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Error != "wrong password" || res.Seed.Challenge == "" {
		t.Errorf("expected wrong-password error + seed, got %+v", res)
	}
	if after := powH(srv); after == before {
		t.Error("a correct h must roll even when the password is wrong")
	}

	// Correct h + correct password -> cookie grants access everywhere.
	cookie := loginFor(t, srv, "secret")
	if w := doReq(srv, "GET", "/api/mode", "", withCookieVal(cookie)); w.Code != http.StatusOK {
		t.Errorf("expected 200 with auth cookie, got %d", w.Code)
	}

	// A fabricated cookie (not derived from salt+password) is rejected.
	if w := doReq(srv, "GET", "/api/mode", "", withCookieVal(strings.Repeat("f", 64))); w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for fabricated cookie, got %d", w.Code)
	}
}

func TestAuth_LoginSubmitInvalidBody(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{AuthPassword: "secret"})

	srv := New(testStaticFS)

	if w := doReq(srv, "POST", "/api/login", "not json", nil); w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid login body, got %d", w.Code)
	}
}

func TestAuth_LoginWhenPasswordUnconfigured(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{AuthPassword: ""})

	srv := New(testStaticFS)

	// Auth disabled: /api/login just says ok so the login page never
	// dead-ends (the redirect to '.' would succeed anyway).
	if w := doReq(srv, "POST", "/api/login", `{"password":"anything"}`, nil); w.Code != http.StatusOK {
		t.Errorf("expected 200 when auth disabled, got %d", w.Code)
	}
}

func TestAuth_ExemptAssetsStayReachable(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{AuthPassword: "secret"})

	srv := New(testStaticFS)

	// 404 is fine (empty test embed); only a redirect/401 would mean the
	// gate intercepted a login-page asset.
	for _, path := range []string{"/favicon.ico", "/assets/dschat.svg"} {
		if w := doReq(srv, "GET", path, "", nil); w.Code != http.StatusNotFound {
			t.Errorf("expected 404 (reachable, not gated) for %q, got %d", path, w.Code)
		}
	}
	// App assets under /web/ stay gated.
	if w := doReq(srv, "GET", "/web/css/style.css", "", nil); w.Code != http.StatusSeeOther {
		t.Errorf("expected 303 to login for gated /web/ asset, got %d", w.Code)
	}
}

func TestAuth_TakesEffectAfterConfigReload(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{AuthPassword: ""})

	srv := New(testStaticFS)

	if w := doReq(srv, "GET", "/api/mode", "", nil); w.Code != http.StatusOK {
		t.Fatalf("expected 200 before password is set, got %d", w.Code)
	}

	// Simulate an on-disk edit picked up via /api/mcp/reload: the
	// middleware must consult the live config, not a startup snapshot.
	// MCP connect failures only log a warning, so Reload succeeds even
	// though the example servers do not exist here.
	storage.SaveConfig(&model.MCPConfig{AuthPassword: "late-secret"})
	if err := srv.mcpMgr.Reload(); err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	if w := doReq(srv, "GET", "/api/mode", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 after password was set + reloaded, got %d", w.Code)
	}
	// And the reloaded password still yields a working login (cookie
	// route only — the Basic Auth shortcut is gone).
	cookie := loginFor(t, srv, "late-secret")
	if w := doReq(srv, "GET", "/api/mode", "", withCookieVal(cookie)); w.Code != http.StatusOK {
		t.Errorf("expected 200 with the reloaded password, got %d", w.Code)
	}
}

func TestAuth_CookieInvalidatedWhenPasswordChanges(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{AuthPassword: "old"})

	srv := New(testStaticFS)
	cookie := loginFor(t, srv, "old")

	if w := doReq(srv, "GET", "/api/mode", "", withCookieVal(cookie)); w.Code != http.StatusOK {
		t.Fatalf("expected 200 before password change, got %d", w.Code)
	}

	storage.SaveConfig(&model.MCPConfig{AuthPassword: "new"})
	if err := srv.mcpMgr.Reload(); err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	// The cookie carries H(old password): stateless rotation means old
	// cookies die the moment the password changes.
	if w := doReq(srv, "GET", "/api/mode", "", withCookieVal(cookie)); w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with stale cookie after password change, got %d", w.Code)
	}
	newCookie := loginFor(t, srv, "new")
	if newCookie == cookie {
		t.Error("new password must yield a different cookie value")
	}
	if w := doReq(srv, "GET", "/api/mode", "", withCookieVal(newCookie)); w.Code != http.StatusOK {
		t.Errorf("expected 200 with fresh cookie, got %d", w.Code)
	}
}
