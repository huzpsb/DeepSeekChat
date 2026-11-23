package server

import "testing"

// TestNew_EngineModeSyncedFromBootMode pins the startup glitch fix: the
// Server boots as "readonly" and the engine must agree from the very
// first request — not only after the first PUT /api/mode round-trip.
// Before the fix the engine held "" which gates like writable (invalid
// tool calls interrupted the run), while every UI surface reported
// "read"; toggling write→read was the only way to resync them.
func TestNew_EngineModeSyncedFromBootMode(t *testing.T) {
	setupServerTest(t)
	srv := New(testStaticFS)

	if got := srv.engine.Mode(); got != srv.getMode() {
		t.Fatalf("engine mode %q drifted from server mode %q at boot", got, srv.getMode())
	}
	if got := srv.engine.Mode(); got != "readonly" {
		t.Fatalf("engine boots in %q, want readonly", got)
	}
}
