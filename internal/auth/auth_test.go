package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ahaygun/api-gateway-rate-limiter/internal/config"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/reqctx"
)

func nextHandler(seen *reqctx.Info) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if info := reqctx.From(r.Context()); info != nil {
			*seen = *info
		}
		w.WriteHeader(http.StatusOK)
	})
}

func serve(h http.Handler, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v1/sms", nil)
	ctx, _ := reqctx.With(req.Context())
	req = req.WithContext(ctx)
	if key != "" {
		req.Header.Set(APIKeyHeader, key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuth_RejectsMissingKey(t *testing.T) {
	a := New([]config.Client{{APIKey: "k1", Name: "acme", Plan: "free"}})
	rec := serve(a.Middleware(nextHandler(&reqctx.Info{})), "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuth_AcceptsValidKeyAndSetsClient(t *testing.T) {
	a := New([]config.Client{{APIKey: "k1", Name: "acme", Plan: "free"}})
	var seen reqctx.Info
	rec := serve(a.Middleware(nextHandler(&seen)), "k1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if seen.Client != "acme" || seen.Plan != "free" {
		t.Fatalf("context client/plan = %q/%q, want acme/free", seen.Client, seen.Plan)
	}
}

func TestAuth_DisabledWhenNoClients(t *testing.T) {
	a := New(nil)
	rec := serve(a.Middleware(nextHandler(&reqctx.Info{})), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (auth disabled)", rec.Code)
	}
}
