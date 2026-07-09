// Package proxy builds the reverse-proxy router from the gateway config.
package proxy

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/ahaygun/api-gateway-rate-limiter/internal/config"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/metrics"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/reqctx"
)

// Gateway routes incoming requests to the configured upstreams.
type Gateway struct {
	routes  []route
	logger  *slog.Logger
	metrics *metrics.Metrics
}

type route struct {
	prefix   string
	methods  map[string]bool // empty means "any method"
	proxy    *httputil.ReverseProxy
	upstream string
}

// New builds a Gateway from the given configuration.
func New(cfg *config.Config, logger *slog.Logger, m *metrics.Metrics) (*Gateway, error) {
	upstreams := make(map[string]*config.Upstream, len(cfg.Upstreams))
	for i := range cfg.Upstreams {
		u := &cfg.Upstreams[i]
		upstreams[u.Name] = u
	}

	g := &Gateway{logger: logger, metrics: m}
	for _, r := range cfg.Routes {
		up := upstreams[r.Upstream] // validated to exist in config.Load
		target, err := url.Parse(up.Target)
		if err != nil {
			return nil, fmt.Errorf("route %q: parse target: %w", r.PathPrefix, err)
		}

		rp := httputil.NewSingleHostReverseProxy(target)
		rp.Transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: up.Timeout.Std(),
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
		}
		rp.ErrorHandler = g.upstreamErrorHandler(up.Name)

		methods := make(map[string]bool, len(r.Methods))
		for _, m := range r.Methods {
			methods[strings.ToUpper(m)] = true
		}

		g.routes = append(g.routes, route{
			prefix:   r.PathPrefix,
			methods:  methods,
			proxy:    rp,
			upstream: up.Name,
		})
	}
	return g, nil
}

// ServeHTTP matches the request against the configured routes (longest
// prefix wins) and forwards it to the upstream.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var best *route
	for i := range g.routes {
		rt := &g.routes[i]
		if !strings.HasPrefix(r.URL.Path, rt.prefix) {
			continue
		}
		if best == nil || len(rt.prefix) > len(best.prefix) {
			best = rt
		}
	}

	if best == nil {
		http.Error(w, "no matching route", http.StatusNotFound)
		return
	}
	if info := reqctx.From(r.Context()); info != nil {
		info.Route = best.prefix
	}
	if len(best.methods) > 0 && !best.methods[r.Method] {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	best.proxy.ServeHTTP(w, r)
}

func (g *Gateway) upstreamErrorHandler(name string) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		g.logger.Warn("upstream error",
			"upstream", name,
			"path", r.URL.Path,
			"err", err,
		)
		if g.metrics != nil {
			g.metrics.UpstreamError(name)
		}
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}
}
