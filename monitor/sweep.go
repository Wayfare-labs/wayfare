package monitor

import (
	"context"
	"sync"
	"time"
)

// SweepResult holds the outcome and duration of a single corridor sweep.
type SweepResult struct {
	Corridor   Corridor
	Duration   time.Duration
	Err        error
}

// SweepCorridors runs measurements for multiple corridors concurrently using
// a bounded worker pool of size concurrency. It preserves per-corridor isolation
// and records duration per sweep.
func SweepCorridors(ctx context.Context, corridors []Corridor, concurrency int, measureFn func(context.Context, Corridor) error) []SweepResult {
	if concurrency <= 0 {
		concurrency = 1
	}
	if len(corridors) == 0 {
		return nil
	}

	type indexedCorridor struct {
		index int
		corridor Corridor
	}

	jobs := make(chan indexedCorridor, len(corridors))
	results := make([]SweepResult, len(corridors))

	for i, c := range corridors {
		jobs <- indexedCorridor{index: i, corridor: c}
	}
	close(jobs)

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				start := time.Now()
				err := measureFn(ctx, job.corridor)
				dur := time.Since(start)
				results[job.index] = SweepResult{
					Corridor: job.corridor,
					Duration: dur,
					Err:      err,
				}
			}
		}()
	}
	wg.Wait()

	return results
}
