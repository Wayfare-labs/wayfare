package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Wayfare-labs/wayfare/monitor"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// checkFreshness reports every corridor whose newest record is older than
// maxAge, or which has no record at all, and returns a non-zero exit code
// when any corridor fails (issue #310: alert when a scheduled measurement
// fails).
//
// The point is to make silent failure loud, so a corridor with no record —
// the case where the very first sweep never ran, or where its history volume
// vanished — is reported exactly like a stale one rather than passed over.
// The checked set is the union of the scheduler's expected corridors and
// whatever the store actually holds: an instance measuring beyond the
// defaults still alerts when one of its own corridors goes quiet. Ages are
// measured against the injected now so the check is testable without waiting
// for a clock.
//
// Exit codes follow verifyStore: 0 means everything is fresh, 1 means at
// least one corridor is stale or missing, 2 means the check itself could not
// run — a broken store reports "unreadable", never "stale", because the two
// need different responses and an unknown is never dressed up as a finding.
func checkFreshness(store runstore.Store, expected []monitor.Corridor, maxAge time.Duration, now time.Time, logger *slog.Logger) int {
	if store == nil {
		logger.Error("-check-fresh requires a store; none is configured")
		return 2
	}
	if maxAge <= 0 {
		logger.Error("-max-age must be positive", "value", maxAge)
		return 2
	}

	ctx := context.Background()

	stored, err := store.Corridors(ctx)
	if err != nil {
		logger.Error("listing corridors", "error", err)
		return 2
	}

	keys := make([]string, 0, len(expected)+len(stored))
	seen := make(map[string]bool, len(expected)+len(stored))
	add := func(k string) {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for _, c := range expected {
		add(c.Key())
	}
	for _, k := range stored {
		add(k)
	}

	if len(keys) == 0 {
		// Nothing expected and nothing recorded: data collection has not
		// started anywhere. That is precisely the silent failure this check
		// exists to make loud, so it is a failure, not a vacuous pass.
		fmt.Println("FAIL no corridors to check and no history recorded")
		return 1
	}

	failed := 0
	for _, k := range keys {
		rec, err := store.Latest(ctx, k)
		if err != nil {
			fmt.Printf("FAIL %s: latest read error: %v\n", k, err)
			failed++
			continue
		}
		if rec == nil {
			fmt.Printf("FAIL %s: no record — measurement has never been recorded\n", k)
			failed++
			continue
		}
		age := now.Sub(rec.RecordedAt.UTC())
		if age < 0 {
			age = 0
		}
		if age > maxAge {
			fmt.Printf("FAIL %s: newest record is %s old, over the %s limit (recorded %s)\n",
				k, humanDuration(age), maxAge, rec.RecordedAt.UTC().Format(time.RFC3339))
			failed++
			continue
		}
		fmt.Printf("ok   %s: newest record is %s old\n", k, humanDuration(age))
	}
	if failed > 0 {
		fmt.Printf("\n%d of %d corridors are stale or missing\n", failed, len(keys))
		return 1
	}
	return 0
}

// humanDuration renders an age for the check-fresh report: whole units at
// the largest scale that fits, so an operator reading one workflow log line
// can tell minutes from months at a glance.
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}
