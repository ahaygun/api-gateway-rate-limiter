// Package metrics defines the Prometheus collectors the gateway exposes and
// the middleware that records them.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ahaygun/api-gateway-rate-limiter/internal/reqctx"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the collectors and their registry.
type Metrics struct {
	reg            *prometheus.Registry
	requests       *prometheus.CounterVec
	duration       *prometheus.HistogramVec
	rateLimited    *prometheus.CounterVec
	upstreamErrors *prometheus.CounterVec
	retries        *prometheus.CounterVec
	circuitState   *prometheus.GaugeVec
}

// New creates and registers the gateway collectors.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		reg: reg,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_requests_total",
			Help: "Total requests handled, by route, method and status.",
		}, []string{"route", "method", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "gateway_request_duration_seconds",
			Help:    "Request latency in seconds, by route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		rateLimited: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_ratelimit_rejected_total",
			Help: "Requests rejected by the rate limiter, by client.",
		}, []string{"client"}),
		upstreamErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_upstream_errors_total",
			Help: "Upstream errors, by upstream name.",
		}, []string{"upstream"}),
		retries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_upstream_retries_total",
			Help: "Upstream call retries, by upstream name.",
		}, []string{"upstream"}),
		circuitState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gateway_circuit_state",
			Help: "Circuit breaker state by upstream (0=closed, 1=open, 2=half-open).",
		}, []string{"upstream"}),
	}
	reg.MustRegister(m.requests, m.duration, m.rateLimited, m.upstreamErrors, m.retries, m.circuitState)
	return m
}

// Handler serves the /metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// RateLimited increments the rejected-by-rate-limit counter for a client.
func (m *Metrics) RateLimited(client string) {
	m.rateLimited.WithLabelValues(labelOrNone(client)).Inc()
}

// UpstreamError increments the upstream-error counter.
func (m *Metrics) UpstreamError(upstream string) {
	m.upstreamErrors.WithLabelValues(labelOrNone(upstream)).Inc()
}

// Retry increments the upstream-retry counter.
func (m *Metrics) Retry(upstream string) {
	m.retries.WithLabelValues(labelOrNone(upstream)).Inc()
}

// SetCircuitState records the current circuit-breaker state for an upstream.
func (m *Metrics) SetCircuitState(upstream string, state int) {
	m.circuitState.WithLabelValues(labelOrNone(upstream)).Set(float64(state))
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Middleware records request count and latency for each request.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		route := "unmatched"
		if info := reqctx.From(r.Context()); info != nil && info.Route != "" {
			route = info.Route
		}
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		m.requests.WithLabelValues(route, r.Method, strconv.Itoa(rec.status)).Inc()
		m.duration.WithLabelValues(route).Observe(time.Since(start).Seconds())
	})
}

func labelOrNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
