package proxy

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ahaygun/api-gateway-rate-limiter/internal/config"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/metrics"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/reqctx"
)

func TestGateway_RoutingAndMethods(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hello from upstream")
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Upstreams: []config.Upstream{{Name: "u", Target: upstream.URL, Timeout: config.Duration(2 * time.Second)}},
		Routes:    []config.Route{{PathPrefix: "/v1/sms", Upstream: "u", Methods: []string{"GET"}}},
	}
	gw, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), metrics.New())
	if err != nil {
		t.Fatalf("build gateway: %v", err)
	}

	do := func(method, path string) (*httptest.ResponseRecorder, *reqctx.Info) {
		req := httptest.NewRequest(method, path, nil)
		ctx, info := reqctx.With(req.Context())
		rec := httptest.NewRecorder()
		gw.ServeHTTP(rec, req.WithContext(ctx))
		return rec, info
	}

	t.Run("matched GET forwards to upstream", func(t *testing.T) {
		rec, info := do(http.MethodGet, "/v1/sms/send")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if rec.Body.String() != "hello from upstream" {
			t.Fatalf("body = %q", rec.Body.String())
		}
		if info.Route != "/v1/sms" {
			t.Fatalf("route context = %q, want /v1/sms", info.Route)
		}
	})

	t.Run("disallowed method is 405", func(t *testing.T) {
		if rec, _ := do(http.MethodDelete, "/v1/sms"); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("unmatched path is 404", func(t *testing.T) {
		if rec, _ := do(http.MethodGet, "/nope"); rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}
