package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// fakeStore is an in-memory runstore.Store.
//
// The real store is exercised by the other monitor tests; what this fake
// exists for is the one thing a real store makes awkward to arrange: a
// corridor whose *previous* run recorded a different structure from the one
// about to be written, so the transition detector has two runs to compare.
// A structural change is a comparison, and a comparison needs two sides.
type fakeStore struct {
	mu      sync.Mutex
	records map[string][]*runstore.Record

	// recentErr, when set, is what Recent returns. It stands for a store
	// that accepts a write but cannot be read back.
	recentErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{records: map[string][]*runstore.Record{}}
}

// seed adds a stored run for a corridor, so the next sweep has a previous
// run to compare its own against.
func (f *fakeStore) seed(corridor, integrity string, dependsOn []string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec := &runstore.Record{
		Version:    runstore.Version,
		Seq:        int64(len(f.records[corridor]) + 1),
		RecordedAt: at,
		Corridor:   corridor,
		Integrity:  integrity,
		DependsOn:  dependsOn,
	}
	f.records[corridor] = append(f.records[corridor], rec)
}

func (f *fakeStore) Append(_ context.Context, r *runstore.Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := *r
	stored.Seq = int64(len(f.records[r.Corridor]) + 1)
	f.records[r.Corridor] = append(f.records[r.Corridor], &stored)
	return nil
}

func (f *fakeStore) Latest(_ context.Context, corridor string) (*runstore.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	recs := f.records[corridor]
	if len(recs) == 0 {
		return nil, nil
	}
	return recs[len(recs)-1], nil
}

// Recent returns up to n records, newest first.
func (f *fakeStore) Recent(_ context.Context, corridor string, n int) ([]*runstore.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recentErr != nil {
		return nil, f.recentErr
	}
	recs := f.records[corridor]
	if n > 0 && len(recs) > n {
		recs = recs[len(recs)-n:]
	}
	out := make([]*runstore.Record, 0, len(recs))
	for i := len(recs) - 1; i >= 0; i-- {
		out = append(out, recs[i])
	}
	return out, nil
}

// RecentBefore returns up to n records strictly before sequence number
// 'before' (i.e. Seq < before), newest first. When before <= 0, it
// returns up to n most recent records without cursor filtering.
func (f *fakeStore) RecentBefore(_ context.Context, corridor string, n int, before int64) ([]*runstore.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recentErr != nil {
		return nil, f.recentErr
	}
	if n <= 0 {
		return nil, nil
	}
	recs := f.records[corridor]
	filtered := make([]*runstore.Record, 0, len(recs))
	for _, rec := range recs {
		if before <= 0 || rec.Seq < before {
			filtered = append(filtered, rec)
		}
	}
	if len(filtered) > n {
		filtered = filtered[len(filtered)-n:]
	}
	out := make([]*runstore.Record, 0, len(filtered))
	for i := len(filtered) - 1; i >= 0; i-- {
		out = append(out, filtered[i])
	}
	return out, nil
}

// All returns the corridor's history, oldest first.
func (f *fakeStore) All(_ context.Context, corridor string) ([]*runstore.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*runstore.Record{}, f.records[corridor]...), nil
}

func (f *fakeStore) Verify(context.Context, string) error { return nil }

func (f *fakeStore) Corridors(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.records))
	for c := range f.records {
		out = append(out, c)
	}
	sort.Strings(out)
	return out, nil
}

// capturedLog is a slog.Handler that keeps what was logged, so a test can
// assert on the line an operator would actually read.
type capturedLog struct {
	mu      sync.Mutex
	entries []logEntry
}

// logEntry is one logged record, with its attributes flattened to strings.
type logEntry struct {
	level   slog.Level
	message string
	attrs   map[string]string
}

func (c *capturedLog) Enabled(context.Context, slog.Level) bool { return true }

func (c *capturedLog) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	attrs := map[string]string{}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})
	c.entries = append(c.entries, logEntry{level: r.Level, message: r.Message, attrs: attrs})
	return nil
}

func (c *capturedLog) WithAttrs([]slog.Attr) slog.Handler { return c }

func (c *capturedLog) WithGroup(string) slog.Handler { return c }

// warns returns every warning logged, in order.
func (c *capturedLog) warns() []logEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []logEntry
	for _, e := range c.entries {
		if e.level == slog.LevelWarn {
			out = append(out, e)
		}
	}
	return out
}

// errors returns every error logged, in order.
func (c *capturedLog) errors() []logEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []logEntry
	for _, e := range c.entries {
		if e.level == slog.LevelError {
			out = append(out, e)
		}
	}
	return out
}

// TestSweepLogsIntegrityTransition is the change issue #579 asks for: after a
// sweep that changes a corridor's structure, the monitor says so — at warn,
// naming both states.
//
// The seeded run says DIRECT and the sweep measures NO-MARKET, which is the
// event the issue calls the most important one the monitor can observe and
// the one that was previously visible only by diffing records by hand.
func TestSweepLogsIntegrityTransition(t *testing.T) {
	store := newFakeStore()
	store.seed("USDC-KESC", "DIRECT", nil, time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))

	logs := &capturedLog{}
	s := schedulerOver(t, "usdc-kesc", asset.KESC(), "KES", "129.4263", store)
	s.Logger = slog.New(logs)

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	warns := logs.warns()
	if len(warns) != 1 {
		t.Fatalf("logged %d warnings, want exactly 1 (the transition): %+v", len(warns), warns)
	}
	w := warns[0]
	if w.message != "corridor integrity transition" {
		t.Errorf("message = %q, want %q", w.message, "corridor integrity transition")
	}
	if w.attrs["corridor"] != "USDC-KESC" {
		t.Errorf("corridor = %q, want USDC-KESC", w.attrs["corridor"])
	}
	if w.attrs["previous_integrity"] != "DIRECT" {
		t.Errorf("previous_integrity = %q, want DIRECT", w.attrs["previous_integrity"])
	}
	if w.attrs["current_integrity"] != "NO-MARKET" {
		t.Errorf("current_integrity = %q, want NO-MARKET", w.attrs["current_integrity"])
	}
	if w.attrs["transition_type"] != "INTEGRITY_STATE_CHANGE" {
		t.Errorf("transition_type = %q, want INTEGRITY_STATE_CHANGE", w.attrs["transition_type"])
	}
}

// TestSweepLogsDependsOnChange pins the other structural change the detector
// finds: a DERIVATIVE corridor whose dependency set moved while its
// integrity state stayed put. The two states are identical here, so the
// dependency sets are what make the log line mean anything.
func TestSweepLogsDependsOnChange(t *testing.T) {
	store := newFakeStore()
	store.seed("USDC-GHSC", "DERIVATIVE", []string{"KESC"}, time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))

	logs := &capturedLog{}
	s := schedulerOver(t, "usdc-ghsc", asset.GHSC(), "GHS", "11.0912", store)
	s.Logger = slog.New(logs)

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	warns := logs.warns()
	if len(warns) != 1 {
		t.Fatalf("logged %d warnings, want exactly 1 (the dependency change): %+v", len(warns), warns)
	}
	w := warns[0]
	if w.attrs["transition_type"] != "DEPENDS_ON_CHANGED" {
		t.Errorf("transition_type = %q, want DEPENDS_ON_CHANGED", w.attrs["transition_type"])
	}
	if w.attrs["previous_integrity"] != "DERIVATIVE" || w.attrs["current_integrity"] != "DERIVATIVE" {
		t.Errorf("integrity = %q -> %q, want DERIVATIVE -> DERIVATIVE",
			w.attrs["previous_integrity"], w.attrs["current_integrity"])
	}
	if !strings.Contains(w.attrs["previous_depends_on"], "KESC") {
		t.Errorf("previous_depends_on = %q, want it to name KESC", w.attrs["previous_depends_on"])
	}
}

// TestSweepDoesNotLogWhenStructureIsUnchanged pins the other half of the
// contract: a corridor whose structure held still says nothing at all. The
// absence of a transition is the normal case, and logging it would bury the
// events that matter.
func TestSweepDoesNotLogWhenStructureIsUnchanged(t *testing.T) {
	store := newFakeStore()
	store.seed("USDC-KESC", "NO-MARKET", nil, time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))

	logs := &capturedLog{}
	s := schedulerOver(t, "usdc-kesc", asset.KESC(), "KES", "129.4263", store)
	s.Logger = slog.New(logs)

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if warns := logs.warns(); len(warns) != 0 {
		t.Errorf("logged %d warnings, want none: %+v", len(warns), warns)
	}
}

// TestSweepDoesNotLogTransitionFromUnknown pins the detector's oldest and
// most consequential rule at the place it now has a caller: a run whose
// integrity is UNKNOWN says nothing about structure, so a corridor that
// recovers from a transient upstream failure must not raise a structural
// alarm. A monitor that cries wolf gets muted.
func TestSweepDoesNotLogTransitionFromUnknown(t *testing.T) {
	store := newFakeStore()
	store.seed("USDC-KESC", "UNKNOWN", nil, time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))

	logs := &capturedLog{}
	s := schedulerOver(t, "usdc-kesc", asset.KESC(), "KES", "129.4263", store)
	s.Logger = slog.New(logs)

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if warns := logs.warns(); len(warns) != 0 {
		t.Errorf("logged %d warnings, want none (UNKNOWN cannot establish a change): %+v", len(warns), warns)
	}
}

// TestSweepLogsNothingOnTheFirstRun pins that a corridor with no previous
// run is not a transition. One measurement is a measurement, not an event.
func TestSweepLogsNothingOnTheFirstRun(t *testing.T) {
	logs := &capturedLog{}
	s := schedulerOver(t, "usdc-kesc", asset.KESC(), "KES", "129.4263", newFakeStore())
	s.Logger = slog.New(logs)

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if warns := logs.warns(); len(warns) != 0 {
		t.Errorf("logged %d warnings, want none (there is no previous run to compare): %+v", len(warns), warns)
	}
}

// TestTransitionReadFailureDoesNotFailTheSweep pins that a store that can be
// written to but not read back costs the operator a log line, not a lost
// measurement. The record is already stored; the next sweep will compare
// against it.
func TestTransitionReadFailureDoesNotFailTheSweep(t *testing.T) {
	store := newFakeStore()
	store.seed("USDC-KESC", "DIRECT", nil, time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))
	store.recentErr = fmt.Errorf("simulated read failure")

	logs := &capturedLog{}
	s := schedulerOver(t, "usdc-kesc", asset.KESC(), "KES", "129.4263", store)
	s.Logger = slog.New(logs)

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce must not fail because the transition check could not read: %v", err)
	}

	errs := logs.errors()
	if len(errs) != 1 {
		t.Fatalf("logged %d errors, want exactly 1 (the failed read): %+v", len(errs), errs)
	}
	if errs[0].message != "integrity transition check failed" {
		t.Errorf("message = %q, want %q", errs[0].message, "integrity transition check failed")
	}
	if warns := logs.warns(); len(warns) != 0 {
		t.Errorf("logged %d warnings, want none: a failed read establishes nothing", len(warns))
	}
}

// TestSweepWithoutAStoreLogsNoTransition pins that history stays optional. A
// monitor with no store configured measures corridors and reports nothing
// about transitions, exactly as it reports nothing about history.
func TestSweepWithoutAStoreLogsNoTransition(t *testing.T) {
	logs := &capturedLog{}
	s := schedulerOver(t, "usdc-kesc", asset.KESC(), "KES", "129.4263", nil)
	s.Logger = slog.New(logs)

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce with no store: %v", err)
	}
	if warns := logs.warns(); len(warns) != 0 {
		t.Errorf("logged %d warnings, want none: %+v", len(warns), warns)
	}
	if errs := logs.errors(); len(errs) != 0 {
		t.Errorf("logged %d errors, want none: %+v", len(errs), errs)
	}
}
