package runstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The checks in this file all exist for the same reason: a record written by a
// host whose clock moved backwards seals and links perfectly. The hash chain is
// a statement about contents, not about time, so nothing else in the package
// would notice that a trend now draws a measurement before the one it claims to
// follow (issue #703).

// TestAppendRefusesRecordOlderThanTip is the rule itself: the record is dropped
// and the chain is exactly as it was.
func TestAppendRefusesRecordOlderThanTip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 8, 21, 22, 0, 0, 0, time.UTC)
	first := fixedRecord()
	first.RecordedAt = base
	if err := s.Append(ctx(), first); err != nil {
		t.Fatalf("Append of the first record: %v", err)
	}

	back := fixedRecord()
	back.RecordedAt = base.Add(-3 * time.Hour)
	err = s.Append(ctx(), back)
	if err == nil {
		t.Fatal("Append accepted a record timed 3h before the tip")
	}

	var oo *ErrOutOfOrder
	if !errors.As(err, &oo) {
		t.Fatalf("Append error is %T (%v), want *ErrOutOfOrder", err, err)
	}
	if oo.Corridor != "USDC-NGNC" {
		t.Errorf("Corridor = %q, want USDC-NGNC", oo.Corridor)
	}
	if oo.TipSeq != 1 {
		t.Errorf("TipSeq = %d, want 1", oo.TipSeq)
	}
	if !oo.TipRecordedAt.Equal(base) {
		t.Errorf("TipRecordedAt = %s, want %s", oo.TipRecordedAt, base)
	}
	if !oo.RecordedAt.Equal(back.RecordedAt) {
		t.Errorf("RecordedAt = %s, want the value the record carried (%s)", oo.RecordedAt, back.RecordedAt)
	}
	if oo.By != 3*time.Hour {
		t.Errorf("By = %v, want 3h", oo.By)
	}
	if oo.Tolerance != ClockSkewTolerance {
		t.Errorf("Tolerance = %v, want %v", oo.Tolerance, ClockSkewTolerance)
	}

	// The message has to carry the diagnosis on its own: the scheduler logs it
	// and an operator reads the log, not the struct.
	msg := err.Error()
	for _, want := range []string{"USDC-NGNC", "seq 1", "3.0h", "2m", base.Add(-3 * time.Hour).Format(time.RFC3339)} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q: %s", want, msg)
		}
	}

	// Nothing was written, the tip did not move, and the chain still verifies.
	if lines := chainLines(t, dir, "USDC-NGNC"); len(lines) != 1 {
		t.Errorf("the refused append wrote %d lines, want the file to stay at 1", len(lines))
	}
	latest, err := s.Latest(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || latest.Seq != 1 || !latest.RecordedAt.Equal(base) {
		t.Errorf("tip after a refused append = %+v, want the seq-1 record", latest)
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Verify after a refused append: %v", err)
	}

	// The store is still usable afterwards: a record on the right side of the
	// tip appends as the next sequence number, proving the rejection did not
	// corrupt the in-memory chain state.
	forward := fixedRecord()
	forward.RecordedAt = base.Add(time.Hour)
	if err := s.Append(ctx(), forward); err != nil {
		t.Fatalf("Append after a rejection: %v", err)
	}
	if latest, _ := s.Latest(ctx(), "USDC-NGNC"); latest == nil || latest.Seq != 2 {
		t.Errorf("seq after the recovery append = %+v, want 2", latest)
	}
}

// TestAppendClockSkewBoundary pins where the line is. The tolerance is a
// deliberate amount of doubt, so both sides of it have to be exactly right: a
// record one second inside it is an overlapping sweep and belongs in the chain,
// and one second outside it is the failure the issue is about.
func TestAppendClockSkewBoundary(t *testing.T) {
	base := time.Date(2026, 8, 21, 22, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		skew  time.Duration
		allow bool
	}{
		{"far behind", -72 * time.Hour, false},
		{"just past the tolerance", -(ClockSkewTolerance + time.Second), false},
		{"exactly at the tolerance", -ClockSkewTolerance, true},
		{"just inside the tolerance", -(ClockSkewTolerance - time.Second), true},
		{"a minute behind", -time.Minute, true},
		{"the same instant", 0, true},
		{"ahead", time.Hour, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			tip := fixedRecord()
			tip.RecordedAt = base
			if err := s.Append(ctx(), tip); err != nil {
				t.Fatalf("Append of the tip: %v", err)
			}

			next := fixedRecord()
			next.RecordedAt = base.Add(tc.skew)
			err = s.Append(ctx(), next)

			switch {
			case tc.allow && err != nil:
				t.Errorf("Append of a record %v relative to the tip: %v", tc.skew, err)
			case !tc.allow && err == nil:
				t.Errorf("Append accepted a record %v relative to the tip; the tolerance is %v",
					tc.skew, ClockSkewTolerance)
			case !tc.allow:
				var oo *ErrOutOfOrder
				if !errors.As(err, &oo) {
					t.Errorf("error is %T (%v), want *ErrOutOfOrder", err, err)
				}
			}
		})
	}
}

// TestAppendFirstRecordIsUnconstrained: with no tip there is nothing to be
// earlier than. A store restored onto a fresh volume, or a corridor measured
// for the first time, must not be blocked by its own clock.
func TestAppendFirstRecordIsUnconstrained(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	old := fixedRecord()
	old.RecordedAt = time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := s.Append(ctx(), old); err != nil {
		t.Fatalf("Append of a first record timed in 2019: %v", err)
	}
	if lines := chainLines(t, dir, "USDC-NGNC"); len(lines) != 1 {
		t.Fatalf("chain has %d records, want 1", len(lines))
	}
}

// TestAppendOrderingIsPerCorridor: each corridor has its own chain and its own
// tip, so a stale chain must not hold back a fresh one — or be protected by it.
func TestAppendOrderingIsPerCorridor(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	recent := fixedRecord()
	recent.Corridor = "USDC-NGNC"
	recent.RecordedAt = time.Date(2026, 8, 21, 22, 0, 0, 0, time.UTC)
	if err := s.Append(ctx(), recent); err != nil {
		t.Fatal(err)
	}

	stale := fixedRecord()
	stale.Corridor = "USDC-GHSC"
	stale.RecordedAt = time.Date(2020, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := s.Append(ctx(), stale); err != nil {
		t.Fatalf("Append to a second corridor with an older timestamp: %v", err)
	}
	if lines := chainLines(t, dir, "USDC-GHSC"); len(lines) != 1 {
		t.Errorf("USDC-GHSC has %d records, want 1", len(lines))
	}

	// And the other direction: the fresh corridor's own history still binds it.
	regressed := fixedRecord()
	regressed.Corridor = "USDC-NGNC"
	regressed.RecordedAt = stale.RecordedAt
	if err := s.Append(ctx(), regressed); err == nil {
		t.Error("Append accepted a USDC-NGNC record older than the USDC-NGNC tip")
	}
}

// TestAppendNormalisesBeforeOrdering: the comparison has to run on the value
// that will actually be stored. A record handed in with a non-UTC zone or sub-
// second precision must be judged after normalisation, not before, or the
// tolerance would be applied to a timestamp the chain never holds.
func TestAppendNormalisesBeforeOrdering(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	zone := time.FixedZone("UTC+3", 3*60*60)
	tip := fixedRecord()
	tip.RecordedAt = time.Date(2026, 8, 21, 22, 0, 0, 0, zone)
	if err := s.Append(ctx(), tip); err != nil {
		t.Fatalf("Append of a non-UTC tip: %v", err)
	}

	// Same instant expressed in UTC: legal, and proof the tip was compared as
	// the UTC second it was stored as rather than as 22:00 in +0300.
	same := fixedRecord()
	same.RecordedAt = time.Date(2026, 8, 21, 19, 0, 0, 0, time.UTC)
	if err := s.Append(ctx(), same); err != nil {
		t.Errorf("Append of the same instant in UTC: %v", err)
	}

	// Three hours behind in the stored frame, which is one hour behind in the
	// input frame: the rule reads the stored one.
	behind := fixedRecord()
	behind.RecordedAt = time.Date(2026, 8, 21, 18, 0, 0, 0, time.UTC)
	if err := s.Append(ctx(), behind); err == nil {
		t.Error("Append accepted a record an hour before the tip")
	}
}

// TestOpenServesAChainWithAHistoricalBackwardsStep locks in the split between
// the two kinds of bad. A step backwards predates #703 in any deployment that
// has been running since then, and the service that hits one at startup should
// keep serving: the records are still cryptographically intact, so the history
// is real, only its times are not trustworthy.
//
// Reporting it is -verify-store's job (see cmd/wayfared) and OrderViolations
// below; refusing to start is nobody's.
func TestOpenServesAChainWithAHistoricalBackwardsStep(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 8, 21, 22, 0, 0, 0, time.UTC)
	writeTimestampedChain(t, dir, "USDC-NGNC", []time.Time{
		base,
		base.Add(6 * time.Hour),
		base.Add(6 * time.Hour).Add(-4 * time.Hour),
	})

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open refused a chain over its clock-skew tolerance: %v", err)
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Verify of an out-of-order but intact chain: %v", err)
	}
	records, err := s.All(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if got := OrderViolations(records); len(got) != 1 {
		t.Fatalf("OrderViolations = %d, want 1: %v", len(got), got)
	}

	// Append still refuses to make it worse from the file's end.
	next := fixedRecord()
	next.RecordedAt = base
	if err := s.Append(ctx(), next); err == nil {
		t.Error("Append accepted a record older than the tip of a rotated-back chain")
	}
}

func TestOrderViolations(t *testing.T) {
	base := time.Date(2026, 8, 21, 22, 0, 0, 0, time.UTC)

	at := func(seq int64, offset time.Duration) *Record {
		r := fixedRecord()
		r.Seq = seq
		r.RecordedAt = base.Add(offset)
		return r
	}

	tests := []struct {
		name    string
		records []*Record
		want    [][2]int64
	}{
		{"no records", nil, nil},
		{"one record", []*Record{at(1, 0)}, nil},
		{"ascending", []*Record{at(1, 0), at(2, time.Hour), at(3, 2*time.Hour)}, nil},
		{"equal timestamps", []*Record{at(1, 0), at(2, 0)}, nil},
		{"inside the tolerance", []*Record{at(1, 0), at(2, -ClockSkewTolerance)}, nil},
		{"past the tolerance", []*Record{at(1, 0), at(2, -ClockSkewTolerance-time.Second)}, [][2]int64{{1, 2}}},
		{"every step reported in order", []*Record{
			at(1, 0),
			at(2, -3*time.Hour),
			at(3, -6*time.Hour),
			at(4, -5*time.Hour),
		}, [][2]int64{{1, 2}, {2, 3}}},
		{"zero timestamps are skipped", []*Record{
			at(1, 0),
			{Version: Version, Seq: 2, Corridor: "USDC-NGNC"},
			at(3, -12*time.Hour),
		}, [][2]int64{{1, 3}}},
		{"nil records are skipped", []*Record{at(1, 0), nil, at(3, -12*time.Hour)}, [][2]int64{{1, 3}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := OrderViolations(tc.records)
			if len(got) != len(tc.want) {
				t.Fatalf("OrderViolations = %d %v, want %d", len(got), got, len(tc.want))
			}
			for i, w := range tc.want {
				if got[i].FromSeq != w[0] || got[i].ToSeq != w[1] {
					t.Errorf("violation %d = seq %d -> %d, want seq %d -> %d",
						i, got[i].FromSeq, got[i].ToSeq, w[0], w[1])
				}
				if got[i].Corridor != "USDC-NGNC" {
					t.Errorf("violation %d Corridor = %q, want USDC-NGNC", i, got[i].Corridor)
				}
				if got[i].By <= 0 {
					t.Errorf("violation %d By = %v, want a positive distance", i, got[i].By)
				}
			}
		})
	}
}

// TestOrderViolationString: the violation is the line an operator reads in a
// -verify-store report, so it has to name both records and the distance.
func TestOrderViolationString(t *testing.T) {
	v := OrderViolation{
		Corridor:       "USDC-NGNC",
		FromSeq:        12,
		FromRecordedAt: time.Date(2026, 8, 21, 22, 0, 0, 0, time.UTC),
		ToSeq:          13,
		ToRecordedAt:   time.Date(2026, 8, 21, 19, 0, 0, 0, time.UTC),
		By:             3 * time.Hour,
	}
	got := v.String()
	for _, want := range []string{"seq 13", "seq 12", "3.0h", "2m", "2026-08-21T19:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Errorf("violation string missing %q: %s", want, got)
		}
	}
}

// TestOrderViolationsAgreesWithAppend is the property that keeps the two halves
// of #703 from drifting apart: the verifier applies the rule Append applied, so
// any chain a store wrote can never be reported as out of order.
func TestOrderViolationsAgreesWithAppend(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 8, 21, 22, 0, 0, 0, time.UTC)
	// Deltas from the previous record, deliberately ragged and including every
	// step the tolerance lets through. They compose rather than accumulate
	// against a fixed grid because the rule binds each record to the tip, which
	// is the record before it.
	deltas := []time.Duration{
		0,
		6 * time.Hour,
		-ClockSkewTolerance,
		-time.Second,
		time.Second,
		-90 * time.Second,
		24 * time.Hour,
	}
	at := base
	for i, delta := range deltas {
		at = at.Add(delta)
		r := fixedRecord()
		r.RecordedAt = at
		if err := s.Append(ctx(), r); err != nil {
			t.Fatalf("Append %d (delta %v): %v", i, delta, err)
		}
	}

	records, err := s.All(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != len(deltas) {
		t.Fatalf("chain has %d records, want %d", len(records), len(deltas))
	}
	if got := OrderViolations(records); len(got) != 0 {
		t.Errorf("OrderViolations reported a chain Append accepted: %v", got)
	}
}

// chainLines reads a corridor file back as the lines it holds.
func chainLines(t *testing.T, dir, corridor string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, corridor+FileExt))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("reading %s: %v", corridor, err)
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// writeTimestampedChain seals one record per timestamp into a corridor file without going
// through Append.
//
// A chain with a backwards step is history an older build wrote, and the only
// way to have one now: Append refuses to produce it. The records are sealed the
// same way Append seals them, so what the test hands Open is byte-identical to
// what that build committed.
func writeTimestampedChain(t *testing.T, dir, corridor string, times []time.Time) {
	t.Helper()

	prev := GenesisPrevHash
	var b strings.Builder
	for i, ts := range times {
		r := fixedRecord()
		r.Version = Version
		r.Corridor = corridor
		r.RecordedAt = ts.UTC().Truncate(time.Second)
		r.Seq = int64(i + 1)
		r.DependsOn = []string{}
		r.PrevHash = prev
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

	path := filepath.Join(dir, corridor+FileExt)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
