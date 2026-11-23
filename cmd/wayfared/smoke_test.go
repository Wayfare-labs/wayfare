package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/metrics"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// ---------------------------------------------------------------------------
// envOr
// ---------------------------------------------------------------------------

func TestEnvOrReturnsFallback(t *testing.T) {
	// Use a key that almost certainly is not set
	got := envOr("WAYFARE_TEST_NONEXISTENT_KEY_XYZZY", "fallback")
	if got != "fallback" {
		t.Errorf("envOr = %q, want %q", got, "fallback")
	}
}

func TestEnvOrReturnsValue(t *testing.T) {
	key := "WAYFARE_TEST_ENV_OR_EXISTS_" + t.Name()
	t.Setenv(key, "real")
	got := envOr(key, "fallback")
	if got != "real" {
		t.Errorf("envOr = %q, want %q", got, "real")
	}
}

func TestEnvOrEmptyValueUsesFallback(t *testing.T) {
	// An empty string in the env should still use the fallback,
	// because envOr checks for empty string.
	key := "WAYFARE_TEST_ENV_OR_EMPTY_" + t.Name()
	t.Setenv(key, "")
	got := envOr(key, "fallback")
	if got != "fallback" {
		t.Errorf("envOr with empty env = %q, want %q", got, "fallback")
	}
}

// ---------------------------------------------------------------------------
// newLogger
// ---------------------------------------------------------------------------

func TestNewLoggerInfo(t *testing.T) {
	l := newLogger("info")
	if l == nil {
		t.Fatal("newLogger returned nil")
	}
	// Verify it can log without panicking
	l.Info("smoke test")
}

func TestNewLoggerDebug(t *testing.T) {
	l := newLogger("debug")
	if l == nil {
		t.Fatal("newLogger returned nil")
	}
	l.Debug("smoke test")
}

func TestNewLoggerWarn(t *testing.T) {
	l := newLogger("warn")
	if l == nil {
		t.Fatal("newLogger returned nil")
	}
	l.Warn("smoke test")
}

func TestNewLoggerError(t *testing.T) {
	l := newLogger("error")
	if l == nil {
		t.Fatal("newLogger returned nil")
	}
	l.Error("smoke test")
}

func TestNewLoggerDefaultToInfo(t *testing.T) {
	l := newLogger("bogus")
	if l == nil {
		t.Fatal("newLogger returned nil")
	}
	// Should not panic; falls through to default info
	l.Info("smoke test")
}

// ---------------------------------------------------------------------------
// openStore
// ---------------------------------------------------------------------------

func TestOpenStoreEmptyDirReturnsNop(t *testing.T) {
	// When dataDir is empty, openStore returns a Nop store when
	// embedded history is unavailable.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	store, _, err := openStore("", logger)
	if err != nil {
		t.Fatalf("openStore(\"\"): %v", err)
	}
	if store == nil {
		t.Fatal("openStore returned nil store")
	}
	// Verify the store responds (Nop or embedded — both are valid)
	corridors, _ := store.Corridors(context.Background())
	_ = corridors
}

func TestOpenStoreTempDir(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	store, verdict, err := openStore(dir, logger)
	if err != nil {
		t.Fatalf("openStore(%q): %v", dir, err)
	}
	if store == nil {
		t.Fatal("openStore returned nil store")
	}
	// An empty directory opens as a store with nothing to verify, which is
	// still a chain this process checked: it is the fallback that must not
	// claim readiness (issue #704).
	if !verdict.ChainVerified {
		t.Errorf("verdict on a freshly opened store = %+v, want verified", verdict)
	}
	corridors, err := store.Corridors(context.Background())
	if err != nil {
		t.Fatalf("Corridors: %v", err)
	}
	if len(corridors) != 0 {
		t.Errorf("expected 0 corridors in fresh store, got %d", len(corridors))
	}
}

func TestOpenStoreNonexistentDir(t *testing.T) {
	// runstore.Open creates the directory, so a path that cannot be created
	// is what must produce the error. A directory placed under an existing
	// *file* fails MkdirAll on every OS; a POSIX path like /dev/null/x only
	// fails on Unix, so the test silently passed on Windows.
	file := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(file, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(file, "impossible")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	_, _, err := openStore(dir, logger)
	if err == nil {
		t.Fatal("expected error for unwritable dir, got nil")
	}
}

// ---------------------------------------------------------------------------
// verifyStore
// ---------------------------------------------------------------------------

func TestVerifyStoreEmpty(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	store, _, err := openStore(dir, logger)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	code := verifyStore(store, dir, logger)
	if code != 0 {
		t.Errorf("verifyStore on empty store = %d, want 0", code)
	}
}

func TestVerifyStoreReadOnlyBackend(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	rec := &runstore.Record{
		Version:      runstore.Version,
		Seq:          1,
		RecordedAt:   time.Unix(0, 0).UTC(),
		Corridor:     "USDC-NGNC",
		Integrity:    "good",
		DependsOn:    []string{},
		PrevHash:     runstore.GenesisPrevHash,
		Finding:      "ok",
		Rungs:        []runstore.Rung{},
		FloorLossPct: "0",
		FloorSize:    "1",
		WorstLossPct: "0",
		WorstSize:    "1",
	}
	if err := rec.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	payload, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "USDC-NGNC.ndjson"), append(payload, '\n'), 0o644); err != nil {
		t.Fatalf("write record: %v", err)
	}

	store, err := runstore.OpenFS(os.DirFS(dir), ".")
	if err != nil {
		t.Fatalf("OpenFS: %v", err)
	}
	if code := verifyStore(store, dir, logger); code != 0 {
		t.Fatalf("verifyStore on read-only backend = %d, want 0", code)
	}
}

// ---------------------------------------------------------------------------
// verifyStore and record time ordering (#703)
// ---------------------------------------------------------------------------

func TestVerifyStoreReportsBackwardsStep(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	base := time.Date(2026, 8, 21, 22, 0, 0, 0, time.UTC)
	writeTimestampedChain(t, dir, "USDC-NGNC", []time.Time{
		base,
		base.Add(6 * time.Hour),
		// Four hours back: intact, correctly linked, and a trend that would
		// draw this measurement before the one it follows.
		base.Add(2 * time.Hour),
	})

	store, _, err := openStore(dir, logger)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}

	var code int
	out := captureStdout(t, func() { code = verifyStore(store, dir, logger) })
	if code != 1 {
		t.Errorf("verifyStore on an out-of-order chain = %d, want 1\n%s", code, out)
	}
	for _, want := range []string{"FAIL USDC-NGNC", "seq 3", "seq 2", "clock-skew tolerance"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
	// The history is still there for the operator to read; the command reports
	// rather than repairing.
	if _, err := store.Latest(context.Background(), "USDC-NGNC"); err != nil {
		t.Errorf("Latest after a failed verify: %v", err)
	}
}

func TestVerifyStoreAcceptsClockSkewWithinTolerance(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	base := time.Date(2026, 8, 21, 22, 0, 0, 0, time.UTC)
	writeTimestampedChain(t, dir, "USDC-NGNC", []time.Time{
		base,
		base.Add(time.Hour),
		// A sweep that overlapped the previous one and finished slightly
		// earlier: the case the tolerance exists to let through.
		base.Add(time.Hour).Add(-30 * time.Second),
	})

	store, _, err := openStore(dir, logger)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}

	var code int
	out := captureStdout(t, func() { code = verifyStore(store, dir, logger) })
	if code != 0 {
		t.Errorf("verifyStore on a chain inside the tolerance = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "ok   USDC-NGNC") {
		t.Errorf("report missing the ok line:\n%s", out)
	}
}

// writeTimestampedChain lays out a corridor file whose records carry the given
// timestamps, sealing each one against the previous the way Append does.
//
// The store refuses to write an out-of-order record now, so a chain that
// predates the rule can only be built the way an older build built it.
func writeTimestampedChain(t *testing.T, dir, corridor string, times []time.Time) {
	t.Helper()

	prev := runstore.GenesisPrevHash
	var b strings.Builder
	for i, ts := range times {
		r := &runstore.Record{
			Version:      runstore.Version,
			Seq:          int64(i + 1),
			RecordedAt:   ts.UTC().Truncate(time.Second),
			Corridor:     corridor,
			Integrity:    "good",
			DependsOn:    []string{},
			Rungs:        []runstore.Rung{},
			Finding:      "ok",
			FloorLossPct: "0",
			FloorSize:    "1",
			WorstLossPct: "0",
			WorstSize:    "1",
			PrevHash:     prev,
		}
		if err := r.Seal(); err != nil {
			t.Fatalf("sealing record %d: %v", i, err)
		}
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("encoding record %d: %v", i, err)
		}
		b.Write(line)
		b.WriteByte('\n')
		prev = r.Hash
	}

	path := filepath.Join(dir, corridor+runstore.FileExt)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// ---------------------------------------------------------------------------
// newScheduler and -horizon-secondary (#701)
// ---------------------------------------------------------------------------

func TestNewSchedulerRejectsAnUnusableFailoverHost(t *testing.T) {
	// Every value here would name an address no request can match, which is
	// failover configured and failover absent — except that nothing says so.
	// The rejection has to happen at startup: a fallback refused during the
	// outage is one that was not there when it was needed.
	tests := []string{
		"horizon.example.org",
		"https://horizon.example.org/api",
		"ftp://horizon.example.org",
	}
	for _, secondary := range tests {
		sched, err := newScheduler(nil, nil, t.TempDir(), "", secondary, nil, quietLogger(), metrics.New())
		if err == nil {
			t.Errorf("-horizon-secondary=%q was accepted", secondary)
		}
		if sched != nil {
			t.Errorf("-horizon-secondary=%q was accepted and returned a scheduler", secondary)
		}
		// The operator has to learn which setting is wrong and what wrong means;
		// a bare "invalid URI" from net/url would not survive a config file.
		if err != nil && !strings.Contains(err.Error(), "failover") {
			t.Errorf("-horizon-secondary=%q error = %v, want it to name the failover host", secondary, err)
		}
	}
}

func TestNewSchedulerAcceptsAFailoverHostAndRecordsIt(t *testing.T) {
	dataDir := t.TempDir()
	sched, err := newScheduler(nil, nil, dataDir, "https://horizon.example.org",
		"https://backup.example.org", nil, quietLogger(), metrics.New())
	if err != nil {
		t.Fatalf("newScheduler with a configured failover host: %v", err)
	}
	if sched == nil {
		t.Fatal("newScheduler returned no scheduler")
	}
	if got := sched.SnapshotSources.Horizon.BaseURL; got != "https://horizon.example.org" {
		t.Errorf("recorded Horizon source = %q, want the host the run is configured against", got)
	}
	if sched.SnapshotEngineFactory == nil {
		t.Error("a data directory must produce a recording engine factory")
	}
}

func TestNewSchedulerWithNoFailoverHostStillStarts(t *testing.T) {
	// The flag is optional and empty is its default, so "not configured" must
	// not read as a bad value: refusing to start here would take the service
	// down for the absence of a stopgap.
	sched, err := newScheduler(nil, nil, t.TempDir(), "", "", nil, quietLogger(), metrics.New())
	if err != nil {
		t.Fatalf("newScheduler without -horizon-secondary: %v", err)
	}
	if sched.SnapshotEngineFactory == nil {
		t.Error("recording must stay available when failover is off")
	}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
