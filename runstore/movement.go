// Package runstore keeps a tamper-evident history of corridor measurements.
//
// This file implements benchmark-vs-corridor movement: comparing consecutive
// stored runs so that "the benchmark moved" and "the corridor moved" stay
// distinguishable afterwards. runstore.Reference records both mids
// deliberately (see Reference and docs/run-store.md); this is the code that
// finally reads them apart.
package runstore

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// Movement describes how a corridor and its benchmark each moved between two
// consecutive stored runs.
//
// Every figure is read off the records in the exact decimal form the writer
// stored, so the comparison can never drift from what the measurement said.
// The two percentage fields answer the issue's question directly: CorridorPct
// is measured against the corridor's own earlier rate, so it is benchmark-free,
// and BenchmarkPct is measured against the benchmark's own earlier mid, so it
// is corridor-free. Read side by side, they separate the two movements; see
// the test file for the exact identity that ties them to the headline change.
//
// All arithmetic is decimal.Decimal. No float64 anywhere.
type Movement struct {
	// Corridor is the corridor key (e.g. "USDC-NGNC").
	Corridor string `json:"corridor"`

	// PreviousRunAt and CurrentRunAt are the two records' RecordedAt stamps.
	PreviousRunAt time.Time `json:"previous_run_at"`
	CurrentRunAt  time.Time `json:"current_run_at"`

	// BenchmarkFrom and BenchmarkTo are the mids the two runs' verdicts
	// were graded against, as stored. Empty when a run had no scorable
	// benchmark.
	BenchmarkFrom string `json:"benchmark_from,omitempty"`
	BenchmarkTo   string `json:"benchmark_to,omitempty"`

	// CorridorFrom and CorridorTo are the effective rates (receive per
	// send unit) of the two runs' smallest priced rung — the floor-size
	// rate the headline loss is built on — as stored. Empty when a run
	// priced nothing.
	CorridorFrom string `json:"corridor_from,omitempty"`
	CorridorTo   string `json:"corridor_to,omitempty"`

	// BenchmarkPct is the benchmark's own move between the two runs,
	// (mid₂ − mid₁) / mid₁ × 100, a signed decimal string; positive means
	// the benchmark rose.
	//
	// It is a measured figure: "0" means the benchmark held still between
	// two scored runs. It is empty — the explicit unknown, never a
	// placeholder zero — when either run had no scorable benchmark: a gap
	// in the series must not masquerade as "the benchmark held still".
	BenchmarkPct string `json:"benchmark_pct,omitempty"`

	// CorridorPct is the corridor's own move between the two runs,
	// (rate₂ − rate₁) / rate₁ × 100, a signed decimal string; positive
	// means the corridor delivered more per unit sent. Because it is
	// measured against the corridor's own earlier rate, a benchmark move
	// cannot appear in it.
	//
	// It is a measured figure: "0" means the corridor's rate held still
	// between two priced runs. It is empty when either run priced nothing:
	// with no price there is no movement, and inventing one would be
	// synthesis.
	CorridorPct string `json:"corridor_pct,omitempty"`

	// LossPctChange is the change in headline floor loss between the two
	// runs, loss₂ − loss₁, a signed decimal string in percentage points;
	// positive means the headline worsened.
	//
	// Empty when either run priced nothing, since a run with no priced
	// rung carries no floor loss at all.
	LossPctChange string `json:"loss_pct_change,omitempty"`
}

// BenchmarkMoved returns how the corridor and its benchmark each moved
// between the two most recent stored runs for a corridor.
//
// A nil result with no error means there is nothing to compare yet — fewer
// than two records, or a pair that establishes no movement at all (neither
// run scorable, neither priced). It is not an error: a missing history is
// the answer, not a failure.
//
// The result is derived on read, never stored: nothing here appends to the
// chain, so it cannot disturb a single hash.
func BenchmarkMoved(ctx context.Context, store Store, corridor string) (*Movement, error) {
	records, err := store.Recent(ctx, corridor, 2)
	if err != nil {
		return nil, fmt.Errorf("runstore: fetching history for %s: %w", corridor, err)
	}
	if len(records) < 2 {
		return nil, nil
	}
	// Recent returns newest first; the movement reads oldest to newest.
	return benchmarkMovement(records[1], records[0]), nil
}

// BenchmarkMovement walks a corridor's whole history and returns every
// consecutive-pair movement, oldest pair first — the same contract as
// DetectTransitions, which needs the full chain rather than Recent's tail
// for exactly the reason given there.
//
// A nil slice with no error means no movement could be established anywhere
// in the history: fewer than two records, or a chain in which no consecutive
// pair carries a scorable benchmark or a priced rung. That is a legitimate
// answer for a young store, and the caller decides what to serve.
func BenchmarkMovement(ctx context.Context, store Store, corridor string) ([]*Movement, error) {
	records, err := store.All(ctx, corridor)
	if err != nil {
		return nil, fmt.Errorf("runstore: fetching history for %s: %w", corridor, err)
	}
	if len(records) < 2 {
		return nil, nil
	}

	var out []*Movement
	for i := 1; i < len(records); i++ {
		if m := benchmarkMovement(records[i-1], records[i]); m != nil {
			out = append(out, m)
		}
	}
	return out, nil
}

// benchmarkMovement derives the benchmark's move, the corridor's own move and
// the headline change from two consecutive records.
//
// Wherever a figure is absent from the records, the corresponding field stays
// empty — the explicit unknown, never zero, never a default. A nil return
// means the pair establishes no movement at all.
func benchmarkMovement(prev, curr *Record) *Movement {
	prevRate, prevOK := storedEffectiveRate(prev)
	currRate, currOK := storedEffectiveRate(curr)
	prevMid, prevMidOK := scoredMid(prev.Reference)
	currMid, currMidOK := scoredMid(curr.Reference)

	m := &Movement{
		Corridor:      curr.Corridor,
		PreviousRunAt: prev.RecordedAt,
		CurrentRunAt:  curr.RecordedAt,
		BenchmarkFrom: prevMid,
		BenchmarkTo:   currMid,
		CorridorFrom:  prevRate,
		CorridorTo:    currRate,
	}

	// Corridor movement and headline change. Both runs must have priced
	// something: a run with no priced rung carries no floor loss and no
	// effective rate, and no loss is not a zero loss.
	if prevOK && currOK {
		a, err1 := decimal.NewFromString(prevRate)
		b, err2 := decimal.NewFromString(currRate)
		if err1 == nil && err2 == nil {
			m.CorridorPct = pctChange(b, a)
		}
		if prevLoss, ok := storedFloorLoss(prev); ok {
			if currLoss, ok2 := storedFloorLoss(curr); ok2 {
				p, err3 := decimal.NewFromString(prevLoss)
				q, err4 := decimal.NewFromString(currLoss)
				if err3 == nil && err4 == nil {
					m.LossPctChange = q.Sub(p).String()
				}
			}
		}
	}

	// Benchmark movement. Absent when either mid is missing or the run
	// was scored against nothing — a gap must not read as stillness.
	if prevMidOK && currMidOK {
		a, err1 := decimal.NewFromString(prevMid)
		b, err2 := decimal.NewFromString(currMid)
		if err1 == nil && err2 == nil {
			m.BenchmarkPct = pctChange(b, a)
		}
	}

	if m.CorridorPct == "" && m.BenchmarkPct == "" {
		return nil
	}
	return m
}

// scoredMid returns the stored mid a record's verdicts were graded against,
// and whether that mid is scorable. A rate no verdict was graded against is
// not a benchmark; treating it as one would let an unscored run pose as data.
func scoredMid(ref Reference) (string, bool) {
	if ref.ScoredAgainst == "" || ref.Source == "" || ref.Mid == "" {
		return "", false
	}
	return ref.Mid, true
}

// storedEffectiveRate returns the stored effective rate (receive per send
// unit) of a run's smallest priced rung — the floor-size rate the headline
// loss is built on — and whether the run priced anything at all. A run may
// have its first sizes fail while later ones priced (a partial ladder), so
// the scan cannot assume the first rung answered.
func storedEffectiveRate(r *Record) (string, bool) {
	for i := range r.Rungs {
		if r.Rungs[i].Priced && r.Rungs[i].EffectiveRate != "" {
			return r.Rungs[i].EffectiveRate, true
		}
	}
	return "", false
}

// storedFloorLoss returns the stored floor loss percentage — the headline the
// movement figures hang off — and whether the run has one. The gate on the
// priced rung keeps the "0.00" a NO-MARKET record stores for an unpriced
// curve from reading as a real zero-percent loss.
func storedFloorLoss(r *Record) (string, bool) {
	if _, priced := storedEffectiveRate(r); !priced {
		return "", false
	}
	if r.FloorLossPct == "" {
		return "", false
	}
	return r.FloorLossPct, true
}

// pctChange returns (to − from) / from × 100 as a decimal string.
// from must be non-zero; callers only invoke it after checking.
func pctChange(to, from decimal.Decimal) string {
	if from.IsZero() {
		return ""
	}
	return to.Sub(from).Div(from).Mul(decimal.NewFromInt(100)).String()
}
