package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
)

const (
	powHMin       = 1
	powHMax       = 100_000_000_000 // 1e11
	powParamRollP = 1000
	powSaltLen    = 16
)

// powLogin is the server-wide single login challenge: generator n,
// safe prime m with n < m < 2n, secret exponent h, and the public
// challenge n^h mod m. All access goes through the mutex; (h,
// challenge) only ever move together, as one atomic roll.
type powLogin struct {
	mu        sync.Mutex
	n, m      *big.Int
	salt      []byte
	h         uint64
	challenge *big.Int
}

// newPowLogin draws the boot-time parameters: the (n, m) pair, the
// per-boot salt, and the first challenge.
func newPowLogin() (*powLogin, error) {
	n, m, err := randomParamPair()
	if err != nil {
		return nil, err
	}
	salt := make([]byte, powSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	p := &powLogin{n: n, m: m, salt: salt}
	p.rollLocked()
	return p, nil
}

// randomParamPair draws the generator/modulus pair: two independent
// random safe primes with exactly 64 bits, tried in both orders. Since
// both land in the same dyadic interval [2^63, 2^64), ordering them as
// n = min, m = max automatically satisfies n < m < 2n (2·min ≥ 2^64 >
// max) — no next-prime scan from n required.
func randomParamPair() (*big.Int, *big.Int, error) {
	var a, b *big.Int
	var err error
	for {
		if a, err = randomSafePrime(); err != nil {
			return nil, nil, err
		}
		if b, err = randomSafePrime(); err != nil {
			return nil, nil, err
		}
		if a.Cmp(b) != 0 { // two draws hitting the same prime is a redraw, not a constraint violation
			break
		}
	}
	if a.Cmp(b) > 0 {
		a, b = b, a
	}
	return a, b, nil
}

// randomSafePrime returns a prime m = 2q+1 of exactly 64 bits with q
// prime. The safe-prime shape keeps Pohlig–Hellman from splitting the
// DLP into small sub-DLPs: m-1 = 2q leaves only the trivial factor 2.
func randomSafePrime() (*big.Int, error) {
	for {
		q, err := rand.Prime(rand.Reader, 63) // exactly 63 bits
		if err != nil {
			return nil, err
		}
		m := new(big.Int).Lsh(q, 1)
		m.Add(m, big.NewInt(1)) // exactly 64 bits
		if m.ProbablyPrime(20) {
			return m, nil
		}
	}
}

// rollLocked re-draws the secret exponent h uniformly from
// [powHMin, powHMax] and recomputes challenge = n^h mod m — the only
// moment the fast-power runs. Caller must hold p.mu.
func (p *powLogin) rollLocked() {
	off, err := rand.Int(rand.Reader, big.NewInt(powHMax-powHMin+1))
	if err != nil {
		// A dead entropy source means the process cannot mint challenges
		// (or anything else crypto) ever again; fail loudly rather than
		// silently keep a challenge that was already revealed as solved.
		panic(fmt.Sprintf("pow: cannot reseed exponent: %v", err))
	}
	p.h = uint64(powHMin) + off.Uint64()
	p.challenge = new(big.Int).Exp(p.n, new(big.Int).SetUint64(p.h), p.m)
}

// rollParamsLocked redraws the (n, m) pair with the same shape
// guarantees as at boot. The salt is deliberately NOT redrawn: the
// issued cookie hash(salt‖password) must keep working across param
// rolls. Runs a safe-prime search (~tens of ms) under the lock — at
// 1/1000 per h-roll that is a negligible hiccup on the login path.
// Caller must hold p.mu.
func (p *powLogin) rollParamsLocked() {
	n, m, err := randomParamPair()
	if err != nil {
		panic(fmt.Sprintf("pow: cannot redraw parameters: %v", err))
	}
	p.n, p.m = n, m
}

// powParamRoll flips the 1/powParamRollP coin deciding whether this
// h-roll also redraws (n, m).
func powParamRoll() bool {
	v, err := rand.Int(rand.Reader, big.NewInt(powParamRollP))
	if err != nil {
		panic(fmt.Sprintf("pow: cannot flip param-roll coin: %v", err))
	}
	return v.Sign() == 0
}

// loginSeed is the public "dlp-seed" handed to any client. n, m and
// challenge are decimal strings: they are ~2^64 and thus beyond JSON's
// exact integer range in JavaScript (2^53); the page BigInt()s them
// back. max_h rides along as a plain number (far below 2^53) so the
// client sizes its BSGS grid from the seed itself — retuning the
// server's constants needs no client change.
type loginSeed struct {
	N         string `json:"n"`
	M         string `json:"m"`
	Challenge string `json:"challenge"`
	MaxH      uint64 `json:"max_h"`
}

// seed snapshots the current public challenge.
func (p *powLogin) seed() loginSeed {
	p.mu.Lock()
	defer p.mu.Unlock()
	return loginSeed{N: p.n.String(), M: p.m.String(), Challenge: p.challenge.String(), MaxH: powHMax}
}

// consume reports whether the submitted exponent matches the current
// one. A match rolls h before returning — making every correct solution
// single-use — and the password is checked only afterwards, so each
// guess costs the attacker one full discrete log regardless of the
// guess being right or wrong. A wrong (or missing) h never rolls
// anything and reveals nothing.
func (p *powLogin) consume(h *int64) bool {
	if h == nil || *h < 0 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if uint64(*h) != p.h {
		return false
	}
	// The pair redraw must precede rollLocked: the fresh challenge
	// binds the new h to whichever pair is current.
	if powParamRoll() {
		p.rollParamsLocked()
	}
	p.rollLocked()
	return true
}

// handleLoginSeed serves GET /api/login/seed: the current dlp-seed
// (n, m, challenge) any client needs to start solving.
func (s *Server) handleLoginSeed(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, s.pow.seed())
}

// writeLoginError is the failure path of POST /api/login. The error is
// always accompanied by the current seed so the client can immediately
// start solving the next challenge in the background while the user
// retypes the password.
func (s *Server) writeLoginError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"error": msg, "seed": s.pow.seed()})
}
