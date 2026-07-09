package proxy

import (
	"errors"
	"net/http"
	"time"

	"github.com/ahaygun/api-gateway-rate-limiter/internal/breaker"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/metrics"
)

// errCircuitOpen is returned when the breaker rejects a call; the gateway's
// ErrorHandler maps it to 503 Service Unavailable.
var errCircuitOpen = errors.New("circuit breaker open")

// resilientTransport wraps a base RoundTripper with a circuit breaker and
// bounded, backed-off retries. Only safe methods (GET, HEAD) are retried, so
// there is never a request body to replay.
type resilientTransport struct {
	base        http.RoundTripper
	breaker     *breaker.Breaker // nil when disabled
	maxAttempts int              // total attempts including the first
	backoff     time.Duration
	upstream    string
	metrics     *metrics.Metrics
	sleep       func(time.Duration) // injectable for tests
}

func (t *resilientTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.breaker != nil && !t.breaker.Allow() {
		return nil, errCircuitOpen
	}

	attempts := 1
	if t.maxAttempts > 1 && isSafeMethod(req.Method) {
		attempts = t.maxAttempts
	}

	var resp *http.Response
	var err error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			if t.metrics != nil {
				t.metrics.Retry(t.upstream)
			}
			t.sleep(t.backoff << (i - 1)) // exponential: 1x, 2x, 4x ...
			if t.breaker != nil && !t.breaker.Allow() {
				return nil, errCircuitOpen
			}
		}

		resp, err = t.base.RoundTrip(req)
		if err == nil && resp.StatusCode < http.StatusInternalServerError {
			t.recordSuccess()
			return resp, nil
		}
		t.recordFailure()

		if i == attempts-1 {
			// Out of attempts: hand back the last outcome as-is — a 5xx
			// response to the client, or the transport error (→ 502).
			return resp, err
		}
		// Will retry: discard this failed response body.
		if resp != nil {
			resp.Body.Close()
		}
	}
	return resp, err
}

func (t *resilientTransport) recordSuccess() {
	if t.breaker != nil {
		t.breaker.Success()
	}
}

func (t *resilientTransport) recordFailure() {
	if t.breaker != nil {
		t.breaker.Failure()
	}
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}
