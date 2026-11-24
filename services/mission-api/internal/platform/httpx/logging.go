package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

// RequestIDHeader is echoed on every response so an operator report can be
// traced back to the exact request in the log.
const RequestIDHeader = "X-Request-Id"

type requestIDKey struct{}

// RequestID returns the id assigned to the request being served, or an empty
// string outside a logged request.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// recorder captures what was written so the log line can state the outcome.
type recorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(payload []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	written, err := r.ResponseWriter.Write(payload)
	r.bytes += written
	return written, err
}

// WithRequestLog gives every request an id and logs one line per request:
// method, path, status and duration. Handlers attach the same id to their own
// lines, so a refusal and the request that caused it can be read together.
func WithRequestLog(next http.Handler, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		id := newRequestID()

		w.Header().Set(RequestIDHeader, id)
		wrapped := &recorder{ResponseWriter: w}
		request := r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))

		next.ServeHTTP(wrapped, request)

		if wrapped.status == 0 {
			wrapped.status = http.StatusOK
		}
		attributes := []any{
			slog.String("request_id", id),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", wrapped.status),
			slog.Int64("duration_ms", time.Since(started).Milliseconds()),
			slog.Int("bytes", wrapped.bytes),
		}

		// A refusal is an expected outcome and stays at info; only a failure
		// of the service itself is worth waking someone for.
		if wrapped.status >= http.StatusInternalServerError {
			logger.WarnContext(request.Context(), "request failed", attributes...)
			return
		}
		logger.InfoContext(request.Context(), "request served", attributes...)
	})
}

func newRequestID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// An id is for correlation only; a timestamp still distinguishes
		// requests if the random source fails.
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(bytes)
}
