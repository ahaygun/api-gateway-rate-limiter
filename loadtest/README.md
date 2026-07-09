# Load testing

Two ways to drive load at the gateway.

## Built-in generator (no external tools)

`loadgen` is a small Go program that fires requests with a worker pool for a
fixed duration and reports throughput and latency percentiles.

```bash
# start the stack first (see repo README), then:
go run ./loadtest/loadgen \
  -url http://localhost:8080/v1/sms/send \
  -key demo-pro-key \
  -c 50 -d 5s
```

Flags: `-url`, `-key` (X-API-Key), `-c` (concurrency), `-d` (duration).

## vegeta (optional)

```bash
vegeta attack -targets=loadtest/targets.txt -rate=1000 -duration=10s | vegeta report
```

## Reference numbers

Measured on an M-series laptop with the gateway, mock upstream and the load
generator all running on the same machine (so latency includes local
scheduling contention — a dedicated upstream would look better). Proxy path,
authentication and rate limiting effectively off, 50 workers for 5s:

| metric      | value       |
|-------------|-------------|
| throughput  | ~2,900 req/s |
| latency p50 | ~4 ms       |
| latency p95 | ~54 ms      |
| latency p99 | ~63 ms      |
| errors      | 0           |

To watch the **rate limiter** engage instead, point the same command at the
`free` plan key (`-key demo-free-key`): most requests return `429` once the
burst of 10 is spent, refilling at 5 req/s.
