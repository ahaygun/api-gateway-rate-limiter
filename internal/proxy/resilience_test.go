package proxy

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ahaygun/api-gateway-rate-limiter/internal/config"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/metrics"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/reqctx"
)

func newGateway(t *testing.T, up config.Upstream) *Gateway {
	t.Helper()
	cfg := &config.Config{
		Upstreams: []config.Upstream{up},
		Routes:    []config.Route{{PathPrefix: "/x", Upstream: up.Name, Methods: []string{"GET"}}},
	}
	gw, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), metrics.New())
	if err != nil {
		t.Fatalf("build gateway: %v", err)
	}
	return gw
}

func getX(gw *Gateway) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	ctx, _ := reqctx.With(req.Context())
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestGateway_RetriesTransientFailures(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 { // fail the first two attempts
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	gw := newGateway(t, config.Upstream{
		Name:    "u",
		Target:  upstream.URL,
		Timeout: config.Duration(2 * time.Second),
		Retry:   config.Retry{MaxAttempts: 3, Backoff: config.Duration(time.Millisecond)},
	})

	rec := getX(gw)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after retries", rec.Code)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("upstream calls = %d, want 3 (2 failures + 1 success)", got)
	}
}

func TestGateway_CircuitOpensAfterThreshold(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	gw := newGateway(t, config.Upstream{
		Name:           "u",
		Target:         upstream.URL,
		Timeout:        config.Duration(2 * time.Second),
		CircuitBreaker: config.CircuitBreaker{FailureThreshold: 2, Cooldown: config.Duration(time.Minute)},
	})

	// First two requests reach the upstream and get its 500.
	if rec := getX(gw); rec.Code != http.StatusInternalServerError {
		t.Fatalf("request 1 status = %d, want 500", rec.Code)
	}
	if rec := getX(gw); rec.Code != http.StatusInternalServerError {
		t.Fatalf("request 2 status = %d, want 500", rec.Code)
	}

	// Breaker is now open: the third request fails fast with 503 and never
	// touches the upstream.
	callsBefore := calls.Load()
	if rec := getX(gw); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("request 3 status = %d, want 503 (circuit open)", rec.Code)
	}
	if calls.Load() != callsBefore {
		t.Fatalf("upstream was called while circuit open (%d -> %d)", callsBefore, calls.Load())
	}
}
