# api-gateway-rate-limiter

A small but production-shaped **API gateway** written in Go. It sits in front of
backend services and applies the cross-cutting concerns an operator needs to
safely expose developer APIs: routing, authentication, **per-client rate
limiting**, observability and graceful operation.

> Think of the layer that protects and meters a telco's public APIs
> (SMS, billing, location) — that is what this project models.

## Features

- **Config-driven reverse proxy** — path-prefix routing to upstreams, longest
  prefix wins, per-route method allow-lists, per-upstream timeouts.
- **API-key authentication** — resolves a key to a client and a plan.
- **Per-client rate limiting** — token bucket with two interchangeable
  backends: **in-memory** (single instance) and **Redis + Lua** (distributed).
  Returns `429` with `Retry-After` and `X-RateLimit-*` headers.
- **Resilience** — per-upstream **circuit breaker** (fails fast with `503`
  while a backend is unhealthy) and bounded **retries with exponential
  backoff** for safe requests.
- **Observability** — Prometheus metrics at `/metrics`, structured `slog`
  logging with request IDs, `/healthz` liveness endpoint.
- **Robust operation** — panic recovery, upstream errors surfaced as `502`,
  a rate-limiter outage fails *open*, graceful shutdown on SIGTERM.
- **Self-contained demo** — mock upstream + `docker-compose` bring the whole
  stack (gateway + Redis + upstream) up with one command.

## Architecture

```
Client ──▶ Gateway ─────────────────────────▶ Upstream (sms-service)
             │
             ├─ recovery      panic → 500, server stays up
             ├─ request-id    X-Request-ID on every call
             ├─ metrics       Prometheus counters + latency histogram
             ├─ logging       method, path, status, latency, client
             ├─ auth          X-API-Key → client + plan (else 401)
             ├─ rate limit    token bucket (memory | redis) → 429
             └─ proxy         httputil.ReverseProxy → upstream
                              └─ resilient transport: circuit breaker + retries
```

`/healthz` and `/metrics` bypass auth and rate limiting.

## Run it locally (no Docker)

Three terminals from the repo root:

```bash
# 1. mock backend
go run ./cmd/upstream --addr :9001 --name sms-service

# 2. gateway (in-memory rate limiting by default)
go run ./cmd/gateway --config gateway.yaml
```

```bash
# 3. send requests
curl -s -H "X-API-Key: demo-free-key" localhost:8080/v1/sms/send
# {"method":"GET","path":"/v1/sms/send","service":"sms-service",...}

curl -s -o /dev/null -w "%{http_code}\n" localhost:8080/v1/sms/send          # 401 (no key)
curl -s -o /dev/null -w "%{http_code}\n" -X DELETE -H "X-API-Key: demo-free-key" localhost:8080/v1/sms  # 405

# watch the limiter: free plan is burst 10, refill 5/s
for i in $(seq 1 15); do
  curl -s -o /dev/null -w "%{http_code} " -H "X-API-Key: demo-free-key" localhost:8080/v1/sms/send
done; echo
# 200 ×10 then 429 ×5

curl -s localhost:8080/healthz          # ok
curl -s localhost:8080/metrics | grep gateway_
```

## Run it with Docker (Redis-backed, distributed limiting)

```bash
docker compose up --build
# gateway on :8080, using the Redis token-bucket backend
```

Compose starts Redis, the mock upstream and the gateway wired together using
`deploy/gateway.docker.yaml` (`ratelimit.backend: redis`).

## Configuration

See [`gateway.yaml`](gateway.yaml). Highlights:

```yaml
ratelimit:
  backend: memory        # or "redis"
  redis_addr: localhost:6379

plans:                   # rate = tokens/sec, burst = bucket capacity
  free: { rate: 5,   burst: 10 }
  pro:  { rate: 100, burst: 200 }

clients:
  - { api_key: "demo-free-key", name: "acme-free",  plan: free }
  - { api_key: "demo-pro-key",  name: "globex-pro", plan: pro }
```

Per-upstream resilience is opt-in:

```yaml
upstreams:
  - name: sms-service
    target: "http://localhost:9001"
    timeout: 3s
    retry:
      max_attempts: 2       # extra attempts for GET/HEAD, exponential backoff
      backoff: 50ms
    circuit_breaker:
      failure_threshold: 5  # open after 5 consecutive failures
      cooldown: 5s          # then one half-open trial probes recovery
```

Config is validated on startup: unknown upstream/plan references, bad targets
or durations, and duplicate keys are rejected with a clear error.

## Design decisions

- **Why a token bucket** — it allows short bursts up to `burst` while holding
  the long-run rate at `rate`, which fits API traffic better than a hard
  fixed-window cap (no boundary spikes).
- **Why Redis + Lua** — with more than one gateway replica the limit must be
  shared, so the counter lives in Redis. The refill-check-write step runs as a
  single **Lua script** so it is atomic; concurrent requests can't race the
  bucket. The in-memory backend stays available for single-instance runs and
  tests.
- **Fail open** — if the limiter backend is unreachable the request is allowed
  (and logged), so a Redis blip degrades metering rather than taking the API
  down.
- **Circuit breaker over blind retries** — retrying a hard-down upstream just
  adds load. The breaker trips after consecutive failures and fails fast with
  `503`, then probes recovery with a single half-open trial. Retries only
  cover *transient* blips, and only for safe (GET/HEAD) requests so there is
  never a body to replay.
- **Graceful shutdown** — on SIGTERM the server stops accepting connections
  and lets in-flight requests finish within a deadline.

## Performance

Built-in load generator, proxy path, everything co-located on one laptop:

| throughput | p50 | p95 | p99 | errors |
|------------|-----|-----|-----|--------|
| ~2,900 req/s | ~4 ms | ~54 ms | ~63 ms | 0 |

See [`loadtest/`](loadtest/) to reproduce.

## Testing

```bash
go test -race ./...
```

Covers config validation, both rate-limiter backends (the Redis/Lua path runs
against an in-process [miniredis](https://github.com/alicebob/miniredis)),
API-key auth, proxy routing/method handling, the circuit-breaker state machine
and the retry/circuit behaviour end-to-end. CI runs gofmt, vet, build and the
race-enabled tests on every push.

## Layout

```
cmd/gateway        gateway entrypoint (wiring + graceful shutdown)
cmd/upstream       mock backend for local testing
internal/config    YAML loading + validation
internal/proxy     reverse-proxy router
internal/middleware recovery, request-id, logging
internal/auth      API-key → client/plan
internal/ratelimit token bucket (memory + redis/lua) + middleware
internal/breaker   circuit breaker state machine
internal/metrics   Prometheus collectors
internal/reqctx    per-request context shared across the chain
loadtest/          load generator + vegeta targets
deploy/            Docker/compose config
```
