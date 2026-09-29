package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"hschat/internal/log"
)

const (
	authCookieName   = "dschat_auth"
	authCookieMaxAge = 30 * 24 * int(time.Hour/time.Second) // 30 days
)

// authToken derives the cookie value for a password as H(salt‖password),
// with the salt drawn once per boot alongside the PoW parameters. The
// fixed-length 16-byte salt prefix keeps the concatenation unambiguous.
// Still stateless by design: the cookie carries H(password), never the
// password itself, so changing (or clearing) the config password
// instantly invalidates every previously issued cookie — no server-side
// session table to manage, and the check below naturally follows config
// reloads. The per-boot salt additionally means a restart logs everyone
// out: a fresh salt re-keys every cookie ever issued.
func (s *Server) authToken(password string) string {
	h := sha256.New()
	h.Write(s.pow.salt)
	h.Write([]byte(password))
	return hex.EncodeToString(h.Sum(nil))
}

// authExempt reports whether a path is reachable without credentials.
// Only the login page itself, the login endpoint, and the handful of
// static assets the login page renders with live here — everything else
// goes through the gate.
func authExempt(path string) bool {
	return path == "/login" ||
		path == "/api/login" ||
		path == "/api/login/seed" ||
		path == "/favicon.ico" ||
		strings.HasPrefix(path, "/assets/")
}

// redirectRel issues a 303 with a RELATIVE Location target. net/http's
// http.Redirect resolves relative targets against the request path into
// a root-absolute URL ("/login"), which breaks behind path-prefix
// proxies (jupyter-server-proxy mounts the app under /proxy/<port>/):
// the browser would resolve that against the proxy origin, not the app.
// RFC 7231 permits relative references; writing the header directly
// keeps the resolution in the browser, relative to the current page.
func redirectRel(w http.ResponseWriter, target string) {
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusSeeOther)
}

// loginRedirectTarget builds the Location that bounces an
// unauthenticated PAGE request to the login screen. The target stays
// relative (proxy-prefix friendly, see redirectRel) and climbs one
// "../" per leading path segment, because a relative reference
// resolves against the request path's DIRECTORY: a flat "login" only
// lands correctly for top-level URLs — from "/web/js/app.js" it would
// resolve to "/web/js/login" (404), not "/login". Extra ".." past the
// root are dropped by RFC 3986 resolution, so a plain slash count is
// enough even for odd paths ("/a//b").
func loginRedirectTarget(path string) string {
	up := strings.Count(path, "/") - 1
	if up < 0 {
		up = 0
	}
	return strings.Repeat("../", up) + "login"
}

// authMiddleware guards the whole handler tree (API + SSE + static).
// Credentials come as the dschat_auth cookie set by POST /api/login —
// cookies travel across ports and path prefixes, where the browser's
// per-origin Basic Auth cache becomes annoying (e.g. one password
// prompt per port behind jupyter-server-proxy). The Basic Auth fast
// path was removed: it verified the raw password on every request,
// forming a guessing oracle that bypassed the login PoW entirely.
// Non-browser clients use the same /api/login (+seed) flow.
//
// When auth_password is empty (the default) every request, including
// ones carrying stale credentials, passes through untouched.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := s.mcpMgr.Config().AuthPassword
		if want == "" || authExempt(r.URL.Path) || s.checkAuth(r, want) {
			next.ServeHTTP(w, r)
			return
		}

		log.Printf("[server] auth_rejected path=%q remote=%q\n", r.URL.Path, r.RemoteAddr)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			// fetch()/EventSource expect JSON errors; SSE in particular
			// must NOT be redirected (EventSource would follow the
			// redirect and choke on the HTML).
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		// Relative target on purpose: keeps working behind path-prefix
		// proxies (jupyter-server-proxy mounts us under /proxy/<port>/).
		redirectRel(w, loginRedirectTarget(r.URL.Path))
	})
}

// checkAuth validates a request against want: the dschat_auth cookie is
// the only accepted credential. A plain equality suffices — no
// constant-time theater: the compare happens once per remote request,
// where network jitter dwarfs any early-exit signal, and even a perfect
// timing oracle would only leak prefixes of sha256(cookie)/sha256(token)
// — recovering the token itself from that is a preimage problem, not a
// comparison problem (and each login probe already costs a discrete log
// via the PoW gate).
func (s *Server) checkAuth(r *http.Request, want string) bool {
	if c, err := r.Cookie(authCookieName); err == nil && c.Value == s.authToken(want) {
		return true
	}
	return false
}

// handleLoginPage serves web/login.html. An already-authenticated visit
// bounces straight back to the app.
func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if pass := s.mcpMgr.Config().AuthPassword; pass != "" && s.checkAuth(r, pass) {
		redirectRel(w, ".")
		return
	}
	if s.loginHTML == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(s.loginHTML)
}

// handleLoginSubmit exchanges (password, PoW h) for the auth cookie.
func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		H        *int64 `json:"h"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	want := s.mcpMgr.Config().AuthPassword
	if want == "" {
		// Auth disabled: nothing to check, let the client through.
		s.writeJSON(w, map[string]bool{"ok": true})
		return
	}
	// PoW gate comes before the password check: the attempt must present
	// the current exponent h of the caller's OWN slot (keyed to the
	// inbound IP — the seed's issuer and its consumer are one IP), and a
	// correct presentation rolls that slot's challenge (single-use) no
	// matter what the password turns out to be — so an attacker pays one
	// discrete log per guess, win or lose, and disturbs only the
	// 1/powSlotCount of IPs sharing their slot.
	if !s.pow.slotFor(r).consume(req.H) {
		log.Printf("[server] login_pow_rejected remote=%q\n", r.RemoteAddr)
		s.writeLoginError(w, r, "pow", http.StatusUnauthorized)
		return
	}
	if req.Password != want {
		log.Printf("[server] login_failed remote=%q\n", r.RemoteAddr)
		// Failure always carries the freshly rolled challenge of the
		// caller's slot.
		s.writeLoginError(w, r, "wrong password", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    s.authToken(want),
		Path:     "/",
		MaxAge:   authCookieMaxAge,
		HttpOnly: true,
		// Lax (not Strict) so following a link into DsChat from another
		// page keeps the session. No Secure flag: deployments behind
		// plain-HTTP localhost or a TLS-terminating proxy would silently
		// drop the cookie otherwise.
		SameSite: http.SameSiteLaxMode,
	})
	log.Printf("[server] login_ok remote=%q\n", r.RemoteAddr)
	s.writeJSON(w, map[string]bool{"ok": true})
}
