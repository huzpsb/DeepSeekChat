package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"sync"
)

const (
	powHMin       = 1
	powHMax       = 100_000_000_000 // 1e11
	powParamRollP = 1000
	powSaltLen    = 16
	// powSlotCount is the number of independent challenge states the
	// login PoW is spread over. A correct consume rolls exactly one
	// state — the solver's own — so a client churning challenges from a
	// fixed source IP can disturb at most 1/powSlotCount of all client
	// IPs; with the previous single global state it churned the login
	// path for everyone.
	powSlotCount = 10
)

// powLogin is ONE independently rolling login challenge: generator n,
// safe prime m with n < m < 2n, secret exponent h, and the public
// challenge n^h mod m. All access goes through the mutex; (h,
// challenge) only ever move together, as one atomic roll. The server
// runs powSlotCount of these plus a fallback (see powTable); the
// states share nothing, so rolling one disturbs no other.
type powLogin struct {
	mu        sync.Mutex
	n, m      *big.Int
	h         uint64
	challenge *big.Int
}

// newPowLogin draws one state: the (n, m) pair and the first challenge.
func newPowLogin() (*powLogin, error) {
	n, m, err := randomParamPair()
	if err != nil {
		return nil, err
	}
	p := &powLogin{n: n, m: m}
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
// guarantees as at boot. The salt lives on the powTable, not here, so
// it cannot roll with the parameters: the issued cookie
// hash(salt‖password) must keep working across param rolls. Runs a
// safe-prime search (~tens of ms) under the lock — at 1/1000 per
// h-roll that is a negligible hiccup on the login path. Caller must
// hold p.mu.
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

type powTable struct {
	salt  []byte
	slots [powSlotCount]*powLogin
}

// newPowTable draws the boot-time state: the shared per-boot salt and
// one fresh challenge per slot. Ten safe-prime pair searches (tens of
// ms each) at boot only.
func newPowTable() (*powTable, error) {
	salt := make([]byte, powSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	t := &powTable{salt: salt}
	for i := range t.slots {
		p, err := newPowLogin()
		if err != nil {
			return nil, err
		}
		t.slots[i] = p
	}
	return t, nil
}

// powSlotIndex maps an inbound IP string to its slot index: SHA-256,
// first 8 bytes big-endian, mod powSlotCount. The hash needs no
// adversarial strength — a client cannot choose its TCP source
// address, so the input is not attacker-picked — it only has to
// scatter distinct addresses evenly.
func powSlotIndex(ip string) int {
	sum := sha256.Sum256([]byte(ip))
	return int(binary.BigEndian.Uint64(sum[:8]) % powSlotCount)
}

// remoteIP extracts the canonical inbound IP of a request's connection.
// RemoteAddr is filled in by net/http from the TCP peer, so it cannot
// be forged by the client; X-Forwarded-For and friends are deliberately
// ignored (see powTable). "" means "no usable IP" — the caller must
// fall back.
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr // already a bare address without port
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String() // canonical text form: one address, one slot
	}
	return ""
}

// slotFor returns the challenge state serving r: slot hash(inbound IP)
// of the connection. No special case for an undeterminable IP:
// remoteIP yields "", whose hash is one fixed slot — such (rare,
// server-fault) requests share that state exactly like any other
// address bucket, i.e. the pre-slot shared-challenge behavior confined
// to their own 1/powSlotCount of the table.
func (t *powTable) slotFor(r *http.Request) *powLogin {
	return t.slots[powSlotIndex(remoteIP(r))]
}

// loginSeed is the public "dlp-seed" handed to any client. n, m and
// challenge are decimal strings: they are ~2^64 and thus beyond JSON's
// exact integer range in JavaScript (2^53); the page BigInt()s them
// back. max_h rides along as a plain number (far below 2^53) so the
// client sizes its BSGS grid from the seed itself — retuning the
// server's constants needs no client change. The client never learns
// which slot it is on; same-IP requests are simply always judged
// against the same state.
type loginSeed struct {
	N         string `json:"n"`
	M         string `json:"m"`
	Challenge string `json:"challenge"`
	MaxH      uint64 `json:"max_h"`
}

// seed snapshots this state's current public challenge.
func (p *powLogin) seed() loginSeed {
	p.mu.Lock()
	defer p.mu.Unlock()
	return loginSeed{N: p.n.String(), M: p.m.String(), Challenge: p.challenge.String(), MaxH: powHMax}
}

// consume reports whether the submitted exponent matches this state's
// current one. A match rolls h before returning — making every correct
// solution single-use — and the password is checked only afterwards, so
// each guess costs the attacker one full discrete log regardless of the
// guess being right or wrong. A wrong (or missing) h never rolls
// anything and reveals nothing. Because the state is the caller's own
// slot, the roll is confined to clients hashing into that slot.
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

// handleLoginSeed serves GET /api/login/seed: the dlp-seed of the
// CALLER's slot. Keyed to the inbound connection IP, it is exactly the
// state the same client's POST /api/login will be judged against —
// applicant and consumer are one IP, one slot.
func (s *Server) handleLoginSeed(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, s.pow.slotFor(r).seed())
}

// writeLoginError is the failure path of POST /api/login. The error is
// always accompanied by the caller's slot's current seed so the client
// can immediately start solving the next challenge in the background
// while the user retypes the password.
func (s *Server) writeLoginError(w http.ResponseWriter, r *http.Request, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"error": msg, "seed": s.pow.slotFor(r).seed()})
}
