package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/monitor"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// The freshness check is offline-testable by construction: it reads only the
// store and compares ages against an injected now, so every test here drives
// a real FileStore in a temp dir with deliberately aged records.

func freshLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// appendRecord writes one minimal valid record for corridor at the given
// time, sealing the chain through the store's own Append path.
func appendRecord(t *testing.T, st runstore.Store, corridor string, at time.Time) {
	t.Helper()
	rec := &runstore.Record{
		RecordedAt: at,
		Corridor:   corridor,
		Integrity:  "DIRECT",
		Reference: runstore.Reference{
			Mid: "1350.2568", Source: "currency-api",
			AsOf: at.UTC().Format(time.RFC3339), ScoredAgainst: "currency-api",
		},
		FloorLossPct: "25.02", FloorSize: "0.1",
		WorstLossPct: "97.68", WorstSize: "5000",
		Finding: "No usable size.",
	}
	if err := st.Append(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// everything it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stdout = orig
	w.Close()
	out := <-done
	r.Close()
	return out
}

func openFreshStore(t *testing.T) runstore.Store {
	t.Helper()
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestCheckFreshAllCorridorsFresh(t *testing.T) {
	now := time.Now().UTC()
	st := openFreshStore(t)
	for _, c := range monitor.DefaultCorridors() {
		appendRecord(t, st, c.Key(), now.Add(-time.Hour))
	}

	var out string
	code := captureExit(t, func() int {
		return checkFreshness(st, monitor.DefaultCorridors(), 12*time.Hour, now, freshLogger())
	}, &out)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, out)
	}
	for _, c := range monitor.DefaultCorridors() {
		if !strings.Contains(out, "ok   "+c.Key()) {
			t.Errorf("output missing ok line for %s:\n%s", c.Key(), out)
		}
	}
}

func TestCheckFreshStaleCorridorFails(t *testing.T) {
	now := time.Now().UTC()
	st := openFreshStore(t)
	appendRecord(t, st, "USDC-NGNC", now.Add(-time.Hour))
	appendRecord(t, st, "USDC-GHSC", now.Add(-72*time.Hour))
	appendRecord(t, st, "USDC-KESC", now.Add(-time.Hour))

	var out string
	code := captureExit(t, func() int {
		return checkFreshness(st, monitor.DefaultCorridors(), 12*time.Hour, now, freshLogger())
	}, &out)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, out)
	}
	if !strings.Contains(out, "FAIL USDC-GHSC") {
		t.Errorf("output missing stale FAIL for USDC-GHSC:\n%s", out)
	}
	if strings.Contains(out, "FAIL USDC-NGNC") {
		t.Errorf("output flags a fresh corridor:\n%s", out)
	}
	if !strings.Contains(out, "1 of 3 corridors") {
		t.Errorf("output missing failure tally:\n%s", out)
	}
}

func TestCheckFreshMissingRecordIsLoud(t *testing.T) {
	// The shape of "the schedule never ran": a corridor expected by the
	// scheduler with nothing recorded. It must fail, not pass vacuously.
	now := time.Now().UTC()
	st := openFreshStore(t)
	appendRecord(t, st, "USDC-NGNC", now)

	var out string
	code := captureExit(t, func() int {
		return checkFreshness(st, monitor.DefaultCorridors(), 12*time.Hour, now, freshLogger())
	}, &out)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, out)
	}
	if !strings.Contains(out, "FAIL USDC-GHSC: no record") {
		t.Errorf("output missing no-record FAIL:\n%s", out)
	}
}

func TestCheckFreshStoreOnlyCorridorsAlsoChecked(t *testing.T) {
	// A corridor the store holds but the defaults do not expect (an
	// instance measuring beyond the defaults) still has to be fresh.
	now := time.Now().UTC()
	st := openFreshStore(t)
	for _, c := range monitor.DefaultCorridors() {
		appendRecord(t, st, c.Key(), now)
	}
	appendRecord(t, st, "USDC-ZZZZ", now.Add(-100*time.Hour))

	var out string
	code := captureExit(t, func() int {
		return checkFreshness(st, monitor.DefaultCorridors(), 12*time.Hour, now, freshLogger())
	}, &out)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, out)
	}
	if !strings.Contains(out, "FAIL USDC-ZZZZ") {
		t.Errorf("output missing FAIL for store-only corridor:\n%s", out)
	}
}

func TestCheckFreshNothingAnywhereFails(t *testing.T) {
	// No expected corridors and an empty store: collection has not started.
	var out string
	code := captureExit(t, func() int {
		return checkFreshness(runstore.Nop{}, nil, 12*time.Hour, time.Now(), freshLogger())
	}, &out)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, out)
	}
	if !strings.Contains(out, "FAIL") {
		t.Errorf("empty collection must be reported as a failure:\n%s", out)
	}
}

func TestCheckFreshFutureRecordIsFresh(t *testing.T) {
	// Clock skew between the writer and this check must not read as stale.
	now := time.Now().UTC()
	st := openFreshStore(t)
	for _, c := range monitor.DefaultCorridors() {
		appendRecord(t, st, c.Key(), now.Add(2*time.Hour))
	}

	code := checkFreshness(st, monitor.DefaultCorridors(), 12*time.Hour, now, freshLogger())
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}

func TestCheckFreshNilStoreIsCheckFailure(t *testing.T) {
	code := checkFreshness(nil, monitor.DefaultCorridors(), 12*time.Hour, time.Now(), freshLogger())
	if code != 2 {
		t.Errorf("nil store = %d, want 2", code)
	}
}

func TestCheckFreshRejectsNonPositiveMaxAge(t *testing.T) {
	st := openFreshStore(t)
	for _, d := range []time.Duration{0, -time.Hour} {
		if code := checkFreshness(st, monitor.DefaultCorridors(), d, time.Now(), freshLogger()); code != 2 {
			t.Errorf("maxAge %v = %d, want 2", d, code)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m"},
		{6*time.Hour + 45*time.Minute, "6h45m"},
		{72 * time.Hour, "3d0h"},
	}
	for _, c := range cases {
		if got := humanDuration(c.d); got != c.want {
			t.Errorf("humanDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// captureExit runs fn (which prints its report to stdout) and returns both
// the exit code it produced and what it printed.
func captureExit(t *testing.T, fn func() int, out *string) int {
	t.Helper()
	code := 0
	*out = captureStdout(t, func() { code = fn() })
	return code
}
