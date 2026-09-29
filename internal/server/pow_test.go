package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"hschat/internal/model"
	"hschat/internal/storage"
)

// slotH peeks a state's current secret exponent — the in-package
// equivalent of a client that has finished its BSGS against it.
func slotH(p *powLogin) uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.h
}

// powH peeks the exponent of the slot serving httptest's default
// RemoteAddr ("192.0.2.1:1234") — i.e. the state every plain doReq
// login request is judged against.
func powH(srv *Server) int64 {
	return int64(slotH(srv.pow.slotFor(httptest.NewRequest("POST", "/api/login", nil))))
}

func TestPow_Parameters(t *testing.T) {
	// A few independent rounds: cheap, and they catch unlucky draws.
	for i := 0; i < 3; i++ {
		tbl, err := newPowTable()
		if err != nil {
			t.Fatalf("newPowTable: %v", err)
		}
		if len(tbl.salt) != powSaltLen {
			t.Errorf("salt length %d, want %d", len(tbl.salt), powSaltLen)
		}
		states := tbl.slots[:]
		if len(states) != powSlotCount {
			t.Errorf("table has %d states, want %d", len(states), powSlotCount)
		}
		seen := map[*powLogin]bool{}
		for _, p := range states {
			// Independent means independent: no two table entries may
			// alias one state, or a roll in "one slot" would roll them
			// all and the isolation guarantee silently dies.
			if seen[p] {
				t.Fatal("two table entries alias the same state")
			}
			seen[p] = true

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

// TestPow_SlotIsolation is the availability property the slotting
// buys: a correct consume rolls only the solver's own state. Every
// other state keeps the challenge it issued, so a client churning
// challenges from one IP can disturb at most 1/powSlotCount of the
// client IPs instead of everyone.
func TestPow_SlotIsolation(t *testing.T) {
	tbl, err := newPowTable()
	if err != nil {
		t.Fatal(err)
	}
	// Everyone except slot 0 (the attacker's slot).
	quiet := tbl.slots[1:]
	snapH := make([]uint64, len(quiet))
	snapC := make([]*big.Int, len(quiet))
	for i, p := range quiet {
		p.mu.Lock()
		snapH[i], snapC[i] = p.h, new(big.Int).Set(p.challenge)
		p.mu.Unlock()
	}

	// The attacker solves and consumes their own slot.
	h0 := int64(slotH(tbl.slots[0]))
	if !tbl.slots[0].consume(&h0) {
		t.Fatal("correct h of slot 0 must consume")
	}
	if slotH(tbl.slots[0]) == uint64(h0) {
		t.Error("slot 0 must roll on a correct consume")
	}

	// No other state moved: their issued seeds remain valid.
	for i, p := range quiet {
		p.mu.Lock()
		h, c := p.h, p.challenge
		p.mu.Unlock()
		if h != snapH[i] || c.Cmp(snapC[i]) != 0 {
			t.Errorf("state %d must be untouched by a consume on slot 0", i)
		}
	}
}

// TestPow_SlotRouting pins the IP→state routing rules: port ignored,
// IPv6 spellings canonicalized, unusable RemoteAddr folded onto the
// fixed hash("") slot — never a panic, never an arbitrary slot.
func TestPow_SlotRouting(t *testing.T) {
	tbl, err := newPowTable()
	if err != nil {
		t.Fatal(err)
	}
	req := func(remote string) *http.Request {
		r := httptest.NewRequest("GET", "/api/login/seed", nil)
		r.RemoteAddr = remote
		return r
	}

	// The port of the connection must not change the slot.
	if tbl.slotFor(req("203.0.113.9:1111")) != tbl.slotFor(req("203.0.113.9:99999")) {
		t.Error("port must not change the slot")
	}
	// One address, one slot — across IPv6 textual spellings.
	if tbl.slotFor(req("[2001:db8::1]:443")) != tbl.slotFor(req("[2001:0db8:0000:0000:0000:0000:0000:0001]:80")) {
		t.Error("the same IPv6 address in different spellings must share a slot")
	}
	// The mapping is the documented powSlotIndex of the inbound IP.
	if got, want := tbl.slotFor(req("203.0.113.9:1")), tbl.slots[powSlotIndex("203.0.113.9")]; got != want {
		t.Error("slotFor must use powSlotIndex of the connection IP")
	}
	// No usable IP (server fault, not a client choice): no special
	// state — every such request hashes "" and lands on one fixed,
	// ordinary slot, like any other address.
	emptySlot := tbl.slots[powSlotIndex("")]
	for _, bad := range []string{"", "localhost:1234", "not-an-ip", "1.2.3.4.5:6"} {
		if got := tbl.slotFor(req(bad)); got != emptySlot {
			t.Errorf("RemoteAddr %q must fold onto the hash(\"\") slot", bad)
		}
	}
}

// TestPow_SlotIndexDistribution: the IP hash must actually use all
// slots. With a uniform hash, the chance that any of the 10 slots
// stays empty across 10,000 distinct IPs is ~10·0.9^10000 ≈ 0 — a
// failure here means a broken mapping, not bad luck.
func TestPow_SlotIndexDistribution(t *testing.T) {
	counts := make([]int, powSlotCount)
	for i := 0; i < 10000; i++ {
		ip := fmt.Sprintf("10.%d.%d.%d", i/256%256, i%256, (i*7)%251)
		counts[powSlotIndex(ip)]++
	}
	for slot, n := range counts {
		if n == 0 {
			t.Errorf("slot %d never hit: %v", slot, counts)
		}
	}
}

// TestPow_ParamRollRedrawsPair exercises the forced param-roll path
// directly (the 1/1000 coin inside consume is stochastic and would make
// a flaky test).
func TestPow_ParamRollRedrawsPair(t *testing.T) {
	tbl, err := newPowTable()
	if err != nil {
		t.Fatal(err)
	}
	p := tbl.slots[3]
	n0, m0 := new(big.Int).Set(p.n), new(big.Int).Set(p.m)
	salt0 := append([]byte(nil), tbl.salt...)

	p.mu.Lock()
	p.rollParamsLocked()
	p.rollLocked() // as consume would: pair first, then h bound to it
	p.mu.Unlock()

	if p.n.Cmp(n0) == 0 && p.m.Cmp(m0) == 0 {
		t.Error("(n, m) must change on a param roll")
	}
	// The salt lives on the table and must NOT roll with a state's
	// parameters (it would log everyone out).
	if string(tbl.salt) != string(salt0) {
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

// bsgsFromSeed is bsgs applied to what the endpoint actually hands
// out — the black-box client's entry point.
func bsgsFromSeed(sd loginSeed) (uint64, bool) {
	n, _ := new(big.Int).SetString(sd.N, 10)
	m, _ := new(big.Int).SetString(sd.M, 10)
	c, _ := new(big.Int).SetString(sd.Challenge, 10)
	return bsgs(n, m, c, sd.MaxH)
}

func TestPow_SolvableByBSGS(t *testing.T) {
	tbl, err := newPowTable()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range tbl.slots[:] {
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

	// Client A is every plain doReq call: httptest's default remote
	// "192.0.2.1:1234". Its seed endpoint is auth-exempt — it must be,
	// the client needs it before it has any credentials.
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
	h, ok := bsgsFromSeed(seed)
	if !ok {
		t.Fatal("seed from endpoint is not solvable")
	}

	// Client B comes from a deterministically different slot (the hash
	// is fixed, so scanning a few IPs always finds one).
	ipB := ""
	for _, ip := range []string{"198.51.100.1", "198.51.100.2", "198.51.100.3", "198.51.100.4", "198.51.100.5"} {
		if powSlotIndex(ip) != powSlotIndex("192.0.2.1") {
			ipB = ip
			break
		}
	}
	if ipB == "" {
		t.Fatal("no IP with a different slot found — impossible with 10 slots")
	}
	asB := func(r *http.Request) { r.RemoteAddr = ipB + ":7777" }

	// B starts solving its own slot's seed.
	wB := doReq(srv, "GET", "/api/login/seed", "", asB)
	var seedB loginSeed
	if err := json.Unmarshal(wB.Body.Bytes(), &seedB); err != nil {
		t.Fatalf("seed B body: %v", err)
	}
	hB, ok := bsgsFromSeed(seedB)
	if !ok {
		t.Fatal("seed B is not solvable")
	}

	// Full flow for A: solved h + correct password -> ok + cookie, and
	// the cookie is hash(salt‖password). A's consume rolls only A's
	// slot.
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
	salt := string(srv.pow.salt) // immutable after boot
	want := sha256.Sum256([]byte(salt + "secret"))
	if cookie != hex.EncodeToString(want[:]) {
		t.Errorf("cookie is not hash(salt‖password): %s", cookie)
	}
	if w := doReq(srv, "GET", "/api/mode", "", withCookieVal(cookie)); w.Code != http.StatusOK {
		t.Errorf("issued cookie must grant access, got %d", w.Code)
	}

	// A's login did not disturb B: B's in-flight solve still consumes.
	if w := doReq(srv, "POST", "/api/login", fmt.Sprintf(`{"password":"secret","h":%d}`, hB), asB); w.Code != http.StatusOK {
		t.Fatalf("B's in-flight solve must survive A's login: %d %s", w.Code, w.Body.String())
	}

	// Cross-slot replay: a solve for A's slot, submitted from B's IP,
	// is judged against B's slot and must fail — applicant and consumer
	// of a seed are the same IP by construction.
	w = doReq(srv, "GET", "/api/login/seed", "", nil) // A's slot, freshly rolled
	var seedA2 loginSeed
	if err := json.Unmarshal(w.Body.Bytes(), &seedA2); err != nil {
		t.Fatalf("seed A2 body: %v", err)
	}
	hA2, ok := bsgsFromSeed(seedA2)
	if !ok {
		t.Fatal("seed A2 is not solvable")
	}
	if w := doReq(srv, "POST", "/api/login", fmt.Sprintf(`{"password":"secret","h":%d}`, hA2), asB); w.Code != http.StatusUnauthorized {
		t.Fatalf("a solve from another slot must be rejected, got %d %s", w.Code, w.Body.String())
	}
	// The rejected cross-slot attempt rolled nothing (wrong h never
	// rolls), so the very same solve still works from its own IP.
	if w := doReq(srv, "POST", "/api/login", fmt.Sprintf(`{"password":"secret","h":%d}`, hA2), nil); w.Code != http.StatusOK {
		t.Fatalf("the same solve from its own IP must succeed, got %d %s", w.Code, w.Body.String())
	}
}
