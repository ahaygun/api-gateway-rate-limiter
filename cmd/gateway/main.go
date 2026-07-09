// Command gateway is the API gateway / rate limiter entrypoint.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ahaygun/api-gateway-rate-limiter/internal/auth"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/config"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/metrics"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/middleware"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/proxy"
	"github.com/ahaygun/api-gateway-rate-limiter/internal/ratelimit"
)

func main() {
	cfgPath := flag.String("config", "gateway.yaml", "path to the config file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(*cfgPath, logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(cfgPath string, logger *slog.Logger) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	m := metrics.New()

	limiter, closeLimiter, err := buildLimiter(cfg, logger)
	if err != nil {
		return err
	}
	defer closeLimiter()

	gw, err := proxy.New(cfg, logger, m)
	if err != nil {
		return err
	}

	authenticator := auth.New(cfg.Clients)

	// Chain order (outermost first). Recovery wraps everything; metrics and
	// logging observe all outcomes including 401/429; auth runs before the
	// rate limiter so the client/plan are known when we meter.
	handler := middleware.Chain(gw,
		middleware.Recovery(logger),
		middleware.RequestID(),
		m.Middleware,
		middleware.Logging(logger),
		authenticator.Middleware,
		ratelimit.Middleware(limiter, cfg.Plans, m, logger),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/metrics", m.Handler())
	mux.Handle("/", handler)

	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout.Std(),
		WriteTimeout: cfg.Server.WriteTimeout.Std(),
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("gateway listening", "addr", cfg.Server.Addr, "ratelimit_backend", cfg.RateLimit.Backend)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return err
	case sig := <-stop:
		logger.Info("shutting down", "signal", sig.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return err
	}
	logger.Info("stopped cleanly")
	return nil
}

// buildLimiter selects the rate-limit backend from config. It returns the
// limiter and a close function (no-op for the in-memory backend).
func buildLimiter(cfg *config.Config, logger *slog.Logger) (ratelimit.Limiter, func(), error) {
	if cfg.RateLimit.Backend == "redis" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rl, err := ratelimit.NewRedisLimiter(ctx, cfg.RateLimit.RedisAddr)
		if err != nil {
			return nil, nil, err
		}
		logger.Info("rate limiter: redis", "addr", cfg.RateLimit.RedisAddr)
		return rl, func() { _ = rl.Close() }, nil
	}
	logger.Info("rate limiter: in-memory")
	return ratelimit.NewMemoryLimiter(), func() {}, nil
}
