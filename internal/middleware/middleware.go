// Package middleware provides the cross-cutting HTTP layers that wrap the
// gateway: panic recovery, request IDs, structured logging and the chain
// helper that composes them.
package middleware

import (
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/ahaygun/api-gateway-rate-limiter/internal/reqctx"
)

// Middleware wraps an http.Handler with additional behaviour.
type Middleware func(http.Handler) http.Handler

// Chain composes middleware so that Chain(h, a, b, c) runs a, then b, then c,
// then h (a is the outermost layer).
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// statusRecorder captures the status code and bytes written so outer layers
// can log and measure the response.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Flush lets streaming responses (e.g. SSE) pass through the recorder.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

var requestCounter atomic.Uint64

// RequestID installs a reqctx.Info holder and assigns a monotonic request ID,
// echoed back in the X-Request-ID response header.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, info := reqctx.With(r.Context())
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = "req-" + strconv.FormatUint(requestCounter.Add(1), 10)
			}
			info.RequestID = id
			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Recovery turns a panic in a downstream handler into a 500 instead of
// crashing the whole server.
func Recovery(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered", "path", r.URL.Path, "panic", rec)
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Logging emits one structured line per request once it is handled, reading
// the route and client that inner layers recorded.
func Logging(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)

			info := reqctx.From(r.Context())
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.bytes,
				"duration_ms", float64(time.Since(start).Microseconds()) / 1000,
			}
			if info != nil {
				attrs = append(attrs,
					"request_id", info.RequestID,
					"route", info.Route,
					"client", info.Client,
				)
			}
			logger.Info("request", attrs...)
		})
	}
}
