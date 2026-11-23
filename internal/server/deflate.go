package server

import (
	"compress/zlib"
	"net/http"
	"strconv"
	"strings"
)

// deflateMinLength is the smallest uncompressed body worth compressing:
// below it the zlib header/trailer overhead plus the CPU cost can exceed
// the savings, so tiny status payloads ({"ok":true}, login errors, …)
// pass through raw.
const deflateMinLength = 512

// withDeflate wraps next with transparent HTTP "deflate" (zlib framing,
// RFC 1950 — what Content-Encoding: deflate means) response compression,
// applied only when the request's Accept-Encoding offers deflate, which
// every mainstream browser does.
//
// The transport decision is deliberately lazy: headers are held back
// until the first body bytes or a Flush arrive, so the middleware can
// skip compression for event streams (SSE must keep per-write flush
// semantics), already-encoded bodies, inherently incompressible media
// types, and bodies below deflateMinLength. Everything else — chat JSON,
// static JS/CSS/HTML, SVG — goes out deflate-compressed.
func withDeflate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HEAD carries no body; asking the client to decode nothing buys
		// nothing (and a Content-Encoding without a body confuses some
		// caches).
		if r.Method == http.MethodHead || !acceptsDeflate(r.Header.Values("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}
		dw := &deflateResponseWriter{rw: w}
		defer dw.finish()
		next.ServeHTTP(dw, r)
	})
}

// acceptsDeflate parses the Accept-Encoding header values and reports
// whether deflate is offered with a non-zero quality. A bare "deflate"
// (no q-parameter) counts as q=1.
func acceptsDeflate(values []string) bool {
	for _, header := range values {
		for _, coding := range strings.Split(header, ",") {
			coding = strings.TrimSpace(coding)
			if coding == "" {
				continue
			}
			name, params, _ := strings.Cut(coding, ";")
			if !strings.EqualFold(strings.TrimSpace(name), "deflate") {
				continue
			}
			q := 1.0
			if p := strings.TrimSpace(params); len(p) >= 2 && strings.EqualFold(p[:2], "q=") {
				if v, err := strconv.ParseFloat(p[2:], 64); err == nil {
					q = v
				}
			}
			if q > 0 {
				return true
			}
		}
	}
	return false
}

// deflateResponseWriter defers the commit point of the wrapped response
// until enough is known about the body to pick raw vs. deflate transport.
// Before the commit, status codes and small chunks are remembered; after
// it, writes stream straight through (optionally via the zlib encoder).
type deflateResponseWriter struct {
	rw       http.ResponseWriter
	status   int          // status code deferred until commit (0 = unset)
	buf      []byte       // body bytes held while below deflateMinLength
	zw       *zlib.Writer // non-nil once deflate transport started
	commited bool         // headers sent to the underlying writer?
	err      error        // first transport error, if any (sticky)
}

// fail records the first transport error; later writes keep failing with
// it so handlers notice a broken pipe instead of writing into the void.
func (w *deflateResponseWriter) fail(err error) {
	if w.err == nil {
		w.err = err
	}
}

func (w *deflateResponseWriter) Header() http.Header { return w.rw.Header() }

// Unwrap lets http.ResponseController reach the underlying writer's
// Flusher/ Hijacker without knowing this wrapper exists.
func (w *deflateResponseWriter) Unwrap() http.ResponseWriter { return w.rw }

func (w *deflateResponseWriter) WriteHeader(code int) {
	if w.commited {
		// Superfluous WriteHeader after the first write: swallow it,
		// exactly like net/http does.
		return
	}
	w.status = code
}

func (w *deflateResponseWriter) Write(p []byte) (int, error) {
	if !w.commited {
		// decide folds p into the pending buffer; when it commits, the
		// buffered bytes (p included) are written out right there — do
		// not write them a second time below.
		w.decide(p)
		return len(p), w.err
	}
	if w.zw != nil {
		if _, err := w.zw.Write(p); err != nil {
			w.fail(err)
			return 0, err
		}
		return len(p), nil
	}
	n, err := w.rw.Write(p)
	if err != nil {
		w.fail(err)
	}
	return n, err
}

// Flush implements http.Flusher. Before the commit point it pins the
// transport decision (an early flush below the threshold keeps the
// response raw — the client wants these bytes now); afterwards it pushes
// buffered compressed data through.
func (w *deflateResponseWriter) Flush() {
	if !w.commited {
		w.decide(nil)
	}
	if w.zw != nil {
		if err := w.zw.Flush(); err != nil {
			w.fail(err)
		}
	}
	if f, ok := w.rw.(http.Flusher); ok {
		f.Flush()
	}
}

// decide folds chunk into the pending body and commits the transport
// unless the response is still below the compression threshold, in which
// case it keeps buffering and reports true. chunk == nil means "decide
// now, no new bytes" (a Flush or handler return).
func (w *deflateResponseWriter) decide(chunk []byte) (buffering bool) {
	if chunk != nil {
		w.buf = append(w.buf, chunk...)
	}

	h := w.rw.Header()
	// Mirror net/http: an undecorated body gets its Content-Type sniffed
	// from the leading UNCOMPRESSED bytes, not from whatever the encoder
	// happens to emit first.
	if h.Get("Content-Type") == "" && len(w.buf) > 0 {
		h.Set("Content-Type", http.DetectContentType(w.buf))
	}

	// Never (re)compress: SSE must stream per-write, a pre-encoded body
	// would double-wrap, and binary media types do not shrink.
	if h.Get("Content-Encoding") != "" || !compressibleContentType(h.Get("Content-Type"), w.buf) {
		w.commitRaw()
		return false
	}

	if len(w.buf) < deflateMinLength {
		if chunk != nil {
			return true // wait for more bytes before paying the compressor
		}
		// Flushed/finished below the threshold: compression can no
		// longer pay off, send raw.
		w.commitRaw()
		return false
	}
	w.commitDeflate()
	return false
}

// commitRaw sends the buffered bytes uncompressed and pins the response
// to raw transport for the rest of its lifetime.
func (w *deflateResponseWriter) commitRaw() {
	w.commited = true
	w.addVary()
	if w.status != 0 {
		w.rw.WriteHeader(w.status)
	}
	if len(w.buf) > 0 {
		if _, err := w.rw.Write(w.buf); err != nil {
			w.fail(err)
		}
	}
	w.buf = nil
}

// commitDeflate switches the response to deflate transport.
func (w *deflateResponseWriter) commitDeflate() {
	w.commited = true
	h := w.rw.Header()
	// The encoded length is only known after the final flush; a stale
	// Content-Length would truncate or over-read the body.
	h.Del("Content-Length")
	h.Set("Content-Encoding", "deflate")
	w.addVary()
	if w.status != 0 {
		w.rw.WriteHeader(w.status)
	}
	if zw, err := zlib.NewWriterLevel(w.rw, zlib.BestSpeed); err != nil {
		w.fail(err)
	} else {
		w.zw = zw
	}
	if _, err := w.zw.Write(w.buf); err != nil {
		w.fail(err)
	}
	w.buf = nil
}

// addVary marks the response as varying on Accept-Encoding so shared
// caches never serve the compressed variant to a client that cannot
// decode it.
func (w *deflateResponseWriter) addVary() {
	w.rw.Header().Add("Vary", "Accept-Encoding")
}

// finish runs after the handler returns: it closes an active compressor
// (flushing the zlib trailer) or commits a small never-committed body.
func (w *deflateResponseWriter) finish() {
	switch {
	case w.zw != nil:
		if err := w.zw.Close(); err != nil {
			w.fail(err)
		}
	case !w.commited:
		w.commitRaw()
	}
}

// compressibleContentType reports whether a body of this MIME type (raw
// media type, parameters stripped) is worth deflate-encoding. Text-ish
// and JSON/JS/SVG types compress several-fold; binary types (PNG, WOFF2,
// ICO, …) are already entropy-coded and would only grow.
func compressibleContentType(contentType string, sample []byte) bool {
	if contentType == "" {
		// Mirror net/http's sniffing so undecorated responses get the
		// same classification they would have gotten for rendering.
		contentType = http.DetectContentType(sample)
	}
	mediaType, _, _ := strings.Cut(contentType, ";")
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	switch mediaType {
	case "text/event-stream":
		return false // SSE: per-write flushes must stay unbuffered
	case "application/json", "application/javascript", "application/x-javascript", "text/javascript", "application/xml", "image/svg+xml", "application/wasm":
		return true
	}
	return strings.HasPrefix(mediaType, "text/")
}
