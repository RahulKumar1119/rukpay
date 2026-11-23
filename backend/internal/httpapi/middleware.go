package httpapi

import (
	"context"
	"log"
	"net/http"
	"strconv"
)

// maxBodyBytes is the largest request body the service reads, 64 KiB
// (Requirement 10.10).
const maxBodyBytes = 64 << 10

// The middleware chain, outermost first, is logging, cors, bodyLimit, router.

// statusRecorder remembers the status code of a response, for the log line. It
// passes Flush through, because the event stream needs it, and exposes the
// wrapped writer through Unwrap so http.ResponseController keeps working.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		s.status = http.StatusOK
		s.wroteHeader = true
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// logging writes one line per request: the method, the route pattern, the status
// and the vault id. It never writes a request body, signals, identifiers,
// amounts, tokens or the signing key (Requirements 11.1, 11.2). A request that
// matched no route logs "-" for the pattern, and a request with no vault id logs
// "-" for the id, so nothing the caller chose reaches the log.
func logging(next http.Handler, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := &requestInfo{}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), infoKey, info)))

		route, id := info.route, info.vaultID
		if route == "" {
			route = "-"
		}
		if id == "" {
			id = "-"
		}
		logger.Printf("route=%q status=%s vault=%s", route, strconv.Itoa(rec.status), id)
	})
}

// cors answers the browser's cross-origin rules. It echoes
// Access-Control-Allow-Origin only when the request Origin equals allowed, so
// every other origin gets no such header (Requirement 10.8). It answers every
// OPTIONS request itself with 204, listing the allowed methods and headers
// (Requirement 10.7).
func cors(next http.Handler, allowed string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Add("Vary", "Origin")
		if origin := r.Header.Get("Origin"); origin != "" && origin == allowed {
			h.Set("Access-Control-Allow-Origin", origin)
		}

		if r.Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type")
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// bodyLimit caps every request body at 64 KiB. A request that declares a larger
// body is answered with 413 at once. A body that grows past the limit while it is
// read makes the read fail, and the handler answers 413 (Requirement 10.10).
func bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxBodyBytes {
			writeCode(w, http.StatusRequestEntityTooLarge, codeTooLarge, msgTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}
