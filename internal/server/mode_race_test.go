package server

// Regression test for the Server.mode data race: mode is written by
// PUT /api/mode and read by every message-editing gate, so concurrent
// requests used to race on the unsynchronized string field. This test is
// only meaningful under -race (it passes trivially otherwise).

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestModeConcurrentAccess(t *testing.T) {
	setupServerTest(t)
	srv := New(testStaticFS)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// concurrent mode switches (writers)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				w := httptest.NewRecorder()
				req := httptest.NewRequest("PUT", "/api/mode", strings.NewReader(`{"mode":"sudo"}`))
				srv.mux.ServeHTTP(w, req)
			}
		}()
	}

	// concurrent readers (GET /api/mode + the readonly gate on the
	// message-edit path reads mode too)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				w := httptest.NewRecorder()
				srv.mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/mode", nil))
			}
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	// let them interleave for a moment
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("PUT", "/api/mode", strings.NewReader(`{"mode":"writable"}`))
	for i := 0; i < 100; i++ {
		srv.mux.ServeHTTP(w2, req2)
	}
	close(stop)
	<-done

	// final sanity: the mode is one of the valid values
	m := srv.getMode()
	if m != "readonly" && m != "writable" && m != "sudo" {
		t.Fatalf("invalid mode after concurrent access: %q", m)
	}
}
