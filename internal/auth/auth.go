// Package auth resolves an incoming API key to a configured client and plan.
package auth

import (
	"net/http"

	"github.com/ahaygun/api-gateway-rate-limiter/internal/config"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/reqctx"
)

// APIKeyHeader is the header clients present to authenticate.
const APIKeyHeader = "X-API-Key"

// Authenticator maps API keys to clients.
type Authenticator struct {
	byKey   map[string]config.Client
	enabled bool
}

// New builds an Authenticator from the configured clients. If no clients are
// configured, authentication is disabled and every request passes through as
// anonymous.
func New(clients []config.Client) *Authenticator {
	byKey := make(map[string]config.Client, len(clients))
	for _, c := range clients {
		byKey[c.APIKey] = c
	}
	return &Authenticator{byKey: byKey, enabled: len(clients) > 0}
}

// Middleware validates the API key and records the client and plan in the
// request context. With auth disabled it is a pass-through.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.enabled {
			next.ServeHTTP(w, r)
			return
		}
		key := r.Header.Get(APIKeyHeader)
		client, ok := a.byKey[key]
		if !ok {
			w.Header().Set("WWW-Authenticate", "ApiKey")
			http.Error(w, "missing or invalid API key", http.StatusUnauthorized)
			return
		}
		if info := reqctx.From(r.Context()); info != nil {
			info.Client = client.Name
			info.Plan = client.Plan
		}
		next.ServeHTTP(w, r)
	})
}
