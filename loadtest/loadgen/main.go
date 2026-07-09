// Command loadgen is a tiny self-contained load generator used to produce the
// throughput and latency numbers in the README. It fires requests with a pool
// of workers for a fixed duration and reports status codes and percentiles.
//
// Usage:
//
//	go run ./loadtest/loadgen -url http://localhost:8080/v1/sms/send \
//	    -key demo-pro-key -c 50 -d 5s
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	url := flag.String("url", "http://localhost:8080/v1/sms/send", "target URL")
	key := flag.String("key", "", "API key sent as X-API-Key")
	concurrency := flag.Int("c", 50, "number of concurrent workers")
	duration := flag.Duration("d", 5*time.Second, "test duration")
	flag.Parse()

	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(*duration)

	var (
		mu        sync.Mutex
		latencies []time.Duration
		statuses  = map[int]int64{}
		errors    atomic.Int64
	)

	var wg sync.WaitGroup
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each worker keeps local tallies to stay lock-free in the hot
			// path, then merges once at the end.
			local := make([]time.Duration, 0, 1024)
			localStatus := map[int]int64{}
			for time.Now().Before(deadline) {
				req, _ := http.NewRequest(http.MethodGet, *url, nil)
				if *key != "" {
					req.Header.Set("X-API-Key", *key)
				}
				start := time.Now()
				resp, err := client.Do(req)
				elapsed := time.Since(start)
				if err != nil {
					errors.Add(1)
					continue
				}
				resp.Body.Close()
				local = append(local, elapsed)
				localStatus[resp.StatusCode]++
			}
			mu.Lock()
			latencies = append(latencies, local...)
			for code, n := range localStatus {
				statuses[code] += n
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	total := len(latencies)
	if total == 0 {
		fmt.Fprintln(os.Stderr, "no successful requests")
		os.Exit(1)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	fmt.Printf("duration:     %s\n", *duration)
	fmt.Printf("concurrency:  %d\n", *concurrency)
	fmt.Printf("requests:     %d\n", total)
	fmt.Printf("throughput:   %.0f req/s\n", float64(total)/duration.Seconds())
	fmt.Printf("latency p50:  %s\n", pct(latencies, 50))
	fmt.Printf("latency p95:  %s\n", pct(latencies, 95))
	fmt.Printf("latency p99:  %s\n", pct(latencies, 99))
	fmt.Printf("errors:       %d\n", errors.Load())
	fmt.Printf("status codes:\n")
	codes := make([]int, 0, len(statuses))
	for c := range statuses {
		codes = append(codes, c)
	}
	sort.Ints(codes)
	for _, c := range codes {
		fmt.Printf("  %d: %d\n", c, statuses[c])
	}
}

func pct(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := p * len(sorted) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx].Round(time.Microsecond)
}
