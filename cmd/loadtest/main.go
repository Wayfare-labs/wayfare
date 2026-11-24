package main

import (
	"flag"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Result struct {
	TotalRequests int
	Successful    int
	Failed        int
	Duration      time.Duration
	RPS           float64
	P50           time.Duration
	P90           time.Duration
	P99           time.Duration
}

func runLoad(targetURL string, concurrency int, totalRequests int) Result {
	if concurrency <= 0 {
		concurrency = 1
	}
	if totalRequests <= 0 {
		totalRequests = 1
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var successCount int64
	var failCount int64

	latencies := make([]time.Duration, 0, totalRequests)
	var latMu sync.Mutex

	reqChan := make(chan struct{}, totalRequests)
	for i := 0; i < totalRequests; i++ {
		reqChan <- struct{}{}
	}
	close(reqChan)

	start := time.Now()
	var wg sync.WaitGroup

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range reqChan {
				reqStart := time.Now()
				resp, err := client.Get(targetURL)
				reqDur := time.Since(reqStart)

				if err == nil && resp.StatusCode < 500 {
					_ = resp.Body.Close()
					atomic.AddInt64(&successCount, 1)
				} else {
					if resp != nil {
						_ = resp.Body.Close()
					}
					atomic.AddInt64(&failCount, 1)
				}

				latMu.Lock()
				latencies = append(latencies, reqDur)
				latMu.Unlock()
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)

	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	var p50, p90, p99 time.Duration
	if n := len(latencies); n > 0 {
		p50 = latencies[n*50/100]
		p90 = latencies[n*90/100]
		p99 = latencies[n*99/100]
	}

	rps := float64(totalRequests) / duration.Seconds()

	return Result{
		TotalRequests: totalRequests,
		Successful:    int(successCount),
		Failed:        int(failCount),
		Duration:      duration,
		RPS:           rps,
		P50:           p50,
		P90:           p90,
		P99:           p99,
	}
}

func main() {
	target := flag.String("url", "http://127.0.0.1:8080/api/corridor", "target endpoint URL")
	concurrency := flag.Int("concurrency", 10, "concurrent worker count")
	requests := flag.Int("requests", 100, "total requests to execute")
	flag.Parse()

	res := runLoad(*target, *concurrency, *requests)
	fmt.Printf("Load test completed:\n")
	fmt.Printf("Requests: %d (%d success, %d failed)\n", res.TotalRequests, res.Successful, res.Failed)
	fmt.Printf("Duration: %s (%.2f req/s)\n", res.Duration.Round(time.Millisecond), res.RPS)
	fmt.Printf("Latency: p50=%s, p90=%s, p99=%s\n", res.P50.Round(time.Millisecond), res.P90.Round(time.Millisecond), res.P99.Round(time.Millisecond))
}
