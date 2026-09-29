package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"testing"

	"hschat/internal/model"
	"hschat/internal/storage"
)

// powH peeks the server's current secret exponent — the in-package
// equivalent of a client that has finished its BSGS.
func powH(srv *Server) int64 {
	srv.pow.mu.Lock()
	defer srv.pow.mu.Unlock()
	return int64(srv.pow.h)
}

func TestPow_Parameters(t *testing.T) {
	// A few independent rounds: cheap, and they catch unlucky draws.
	for i := 0; i < 3; i++ {
		p, err := newPowLogin()
		if err != nil {
			t.Fatalf("newPowLogin: %v", err)
		}
		if !p.n.ProbablyPrime(20) || !p.m.ProbablyPrime(20) {
			t.Fatal("n and m must be prime")
		}
		if p.n.Cmp(p.m) >= 0 {
			t.Errorf("need n < m, got n=%s m=%s", p.n, p.m)
		}
		if twoN := new(big.Int).Lsh(p.n, 1); twoN.Cmp(p.m) <= 0 {
			t.Errorf("need m < 2n, got n=%s m=%s", p.n, p.m)
		}
		if p.m.BitLen() != 64 {
			t.Errorf("m should be a 64-bit prime, got %d bits", p.m.BitLen())
		}
		// Safe prime: (m-1)/2 prime too, so Pohlig–Hellman has no small
		// subgroup to chew on.
		q := new(big.Int).Rsh(new(big.Int).Sub(p.m, big.NewInt(1)), 1)
		if !q.ProbablyPrime(20) {
			t.Error("(m-1)/2 must be prime (safe prime)")
		}
		if p.h < powHMin || p.h > powHMax {
			t.Errorf("h out of [powHMin, powHMax]=[%d, %d]: %d", powHMin, powHMax, p.h)
		}
		if got := new(big.Int).Exp(p.n, new(big.Int).SetUint64(p.h), p.m); got.Cmp(p.challenge) != 0 {
			t.Errorf("challenge != n^h mod m: got %s want %s", p.challenge, got)
		}
		if len(p.salt) != powSaltLen {
			t.Errorf("salt length %d, want %d", len(p.salt), powSaltLen)
		}
	}
}

func TestPow_ConsumeRollsExactlyOnCorrectH(t *testing.T) {
	p, err := newPowLogin()
	if err != nil {
		t.Fatal(err)
	}
	h0, c0 := p.h, new(big.Int).Set(p.challenge)

	// Wrong and missing exponents never roll anything.
	zero := int64(0)
	if p.consume(&zero) {
		t.Error("h=0 must not consume")
	}
	if p.consume(nil) {
		t.Error("missing h must not consume")
	}
	if p.h != h0 || p.challenge.Cmp(c0) != 0 {
		t.Fatal("a rejected h must leave the challenge untouched")
	}

	// A correct h consumes and rolls, exactly once.
	h1 := int64(h0)
	if !p.consume(&h1) {
		t.Fatal("the correct h must consume")
	}
	if p.h == h0 {
		t.Error("h must change on a roll")
	}
	if p.challenge.Cmp(c0) == 0 {
		t.Error("challenge must be recomputed on a roll")
	}
	// rollLocked draws from [powHMin, powHMax] INCLUSIVE: only the open
	// ends are out of range.
	if p.h < powHMin || p.h > powHMax {
		t.Errorf("rolled h out of range: %d", p.h)
	}
	if p.consume(&h1) {
		t.Error("a replayed h must fail after the roll (single-use)")
	}

	// The rolled h is the new valid one.
	h2 := int64(p.h)
	if !p.consume(&h2) {
		t.Error("the freshly rolled h must consume")
	}
}

// TestPow_ParamRollRedrawsPair exercises the forced param-roll path
// directly (the 1/1000 coin inside consume is stochastic and would make
// a flaky test).
func TestPow_ParamRollRedrawsPair(t *testing.T) {
	p, err := newPowLogin()
	if err != nil {
		t.Fatal(err)
	}
	n0, m0 := new(big.Int).Set(p.n), new(big.Int).Set(p.m)
	salt0 := append([]byte(nil), p.salt...)

	p.mu.Lock()
	p.rollParamsLocked()
	p.rollLocked() // as consume would: pair first, then h bound to it
	p.mu.Unlock()

	if p.n.Cmp(n0) == 0 && p.m.Cmp(m0) == 0 {
		t.Error("(n, m) must change on a param roll")
	}
	if string(p.salt) != string(salt0) {
		t.Error("salt must NOT roll with the parameters (it would log everyone out)")
	}
	// The new pair keeps its shape...
	if !p.n.ProbablyPrime(20) || !p.m.ProbablyPrime(20) || p.n.Cmp(p.m) >= 0 {
		t.Errorf("param roll broke the pair shape: n=%s m=%s", p.n, p.m)
	}
	if twoN := new(big.Int).Lsh(p.n, 1); twoN.Cmp(p.m) <= 0 {
		t.Errorf("param roll broke m < 2n: n=%s m=%s", p.n, p.m)
	}
	q := new(big.Int).Rsh(new(big.Int).Sub(p.m, big.NewInt(1)), 1)
	if !q.ProbablyPrime(20) {
		t.Error("param roll broke the safe-prime property")
	}
	// ...and the challenge is re-bound to it.
	if got := new(big.Int).Exp(p.n, new(big.Int).SetUint64(p.h), p.m); got.Cmp(p.challenge) != 0 {
		t.Error("challenge must match the new pair")
	}
	// A black-box client can still solve the rolled state.
	if h, ok := bsgs(p.n, p.m, p.challenge, powHMax); !ok || h != p.h {
		t.Errorf("bsgs on rolled state: h=%d ok=%v want %d", h, ok, p.h)
	}
}

// bsgs mirrors the login page's solver exactly: given only the public
// (n, m, challenge) and the announced bound, recover h with baby-step
// giant-step. If this stops finding h, the browser login flow is broken
// too.
func bsgs(n, m, c *big.Int, maxH uint64) (uint64, bool) {
	s := int64(math.Ceil(math.Sqrt(float64(maxH))))
	baby := map[string]int64{}
	v := new(big.Int).Mod(c, m)
	for j := int64(0); j < s; j++ {
		key := v.String()
		if _, ok := baby[key]; !ok {
			baby[key] = j
		}
		v.Mul(v, n)
		v.Mod(v, m)
	}
	stride := new(big.Int).Exp(n, big.NewInt(s), m)
	g := new(big.Int).Set(stride)
	for i := int64(1); i <= s; i++ {
		if j, ok := baby[g.String()]; ok && i*s-j >= 1 {
			return uint64(i*s - j), true
		}
		g.Mul(g, stride)
		g.Mod(g, m)
	}
	return 0, false
}

func TestPow_SolvableByBSGS(t *testing.T) {
	for i := 0; i < 3; i++ {
		p, err := newPowLogin()
		if err != nil {
			t.Fatal(err)
		}
		h, ok := bsgs(p.n, p.m, p.challenge, powHMax)
		if !ok {
			t.Fatal("bsgs found no solution")
		}
		if h != p.h {
			t.Errorf("bsgs recovered h=%d, want %d", h, p.h)
		}
	}
}

func TestPow_LoginSeedAndEndpointFlow(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{AuthPassword: "secret"})

	srv := New(testStaticFS)

	// The seed endpoint is auth-exempt: it must be, the client needs it
	// before it has any credentials.
	w := doReq(srv, "GET", "/api/login/seed", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("seed endpoint: %d", w.Code)
	}
	var seed loginSeed
	if err := json.Unmarshal(w.Body.Bytes(), &seed); err != nil {
		t.Fatalf("seed body: %v", err)
	}
	if seed.N == "" || seed.M == "" || seed.Challenge == "" {
		t.Errorf("incomplete seed: %+v", seed)
	}
	if seed.MaxH != powHMax {
		t.Errorf("seed max_h = %d, want %d", seed.MaxH, powHMax)
	}

	// Black-box: what the endpoint hands out is solvable by a client
	// that knows nothing but the seed.
	n, _ := new(big.Int).SetString(seed.N, 10)
	m, _ := new(big.Int).SetString(seed.M, 10)
	c, _ := new(big.Int).SetString(seed.Challenge, 10)
	h, ok := bsgs(n, m, c, seed.MaxH)
	if !ok {
		t.Fatal("seed from endpoint is not solvable")
	}

	// Full flow: solved h + correct password -> ok + cookie, and the
	// cookie is hash(salt‖password).
	w = doReq(srv, "POST", "/api/login", fmt.Sprintf(`{"password":"secret","h":%d}`, h), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("login with solved h: %d %s", w.Code, w.Body.String())
	}
	var cookie string
	for _, ck := range w.Result().Cookies() {
		if ck.Name == authCookieName {
			cookie = ck.Value
		}
	}
	if cookie == "" {
		t.Fatal("no auth cookie issued")
	}
	srv.pow.mu.Lock()
	salt := string(srv.pow.salt)
	srv.pow.mu.Unlock()
	want := sha256.Sum256([]byte(salt + "secret"))
	if cookie != hex.EncodeToString(want[:]) {
		t.Errorf("cookie is not hash(salt‖password): %s", cookie)
	}
	if w := doReq(srv, "GET", "/api/mode", "", withCookieVal(cookie)); w.Code != http.StatusOK {
		t.Errorf("issued cookie must grant access, got %d", w.Code)
	}
}
