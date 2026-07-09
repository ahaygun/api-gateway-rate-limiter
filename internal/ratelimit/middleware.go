package ratelimit

import (
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/ahaygun/api-gateway-rate-limiter/internal/config"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/metrics"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/reqctx"
)

// Middleware enforces the per-client rate limit. Requests without an
// authenticated client or plan are not limited (nothing to meter). On a
// limiter error it fails open so a Redis outage does not take the API down.
func Middleware(l Limiter, plans map[string]config.Plan, m *metrics.Metrics, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info := reqctx.From(r.Context())
			if info == nil || info.Plan == "" {
				next.ServeHTTP(w, r)
				return
			}
			plan, ok := plans[info.Plan]
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			res, err := l.Allow(r.Context(), info.Client, plan.Rate, plan.Burst)
			if err != nil {
				logger.Warn("rate limiter unavailable, allowing request", "err", err, "client", info.Client)
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(res.Limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(max(res.Remaining, 0)))

			if !res.Allowed {
				retry := int(math.Ceil(res.RetryAfter.Seconds()))
				if retry < 1 {
					retry = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retry))
				m.RateLimited(info.Client)
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
