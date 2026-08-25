package runstore

import (
	"context"

	"github.com/Wayfare-labs/wayfare/route"
)

// FromCorridorJSON builds a record from the shared wire shape.
//
// Deriving from route.CorridorJSON rather than from LadderResult directly is
// deliberate: the HTTP API and cmd/ladder -json already emit that shape, so
// the stored record and the served record cannot drift into two schemas that
// disagree about the same measurement.
func FromCorridorJSON(c route.CorridorJSON) *Record {
	r := &Record{
		Version:   Version,
		Corridor:  CorridorKey(c.SendAsset.Code, c.ReceiveAsset.Code),
		Integrity: c.Integrity,
		DependsOn: []string{},
		// SecondaryMid, SecondarySource and DivergencePct are carried
		// through unconditionally: the wire shape already reports them
		// whenever a cross-check ran, and dropping them here would mean a
		// stale replay of a corridor that was cross-checked reads back as
		// one that was not — see TestFromCorridorJSONCarriesReferenceCrossCheck.
		//
		// ScoredAgainst mirrors c.ReferenceSource only when the corridor was
		// scorable at all: an unscorable rate never produced a verdict, so
		// naming a source here would claim one did.
		//
		// SecondaryAsOf and the parallel/street-market block have no home in
		// this record shape yet — see the note on Record's field-addition
		// rule. Adding them is a Record layout change with its own migration
		// and review bar, so it is flagged here rather than done as part of
		// this fix.
		Reference: Reference{
			Mid:             c.ReferenceMid,
			Source:          c.ReferenceSource,
			SecondaryMid:    c.ReferenceSecondaryMid,
			SecondarySource: c.ReferenceSecondarySource,
			DivergencePct:   c.ReferenceDivergencePct,
			ScoredAgainst:   scoredAgainst(c),
		},
		FloorLossPct:    c.Floor,
		FloorSize:       c.FloorSize,
		WorstLossPct:    c.WorstLoss,
		WorstSize:       c.WorstSize,
		RecommendedSize: c.RecommendedSize,
		Finding:         c.Finding,
		Rungs:           make([]Rung, 0, len(c.Rungs)),
	}

	// Findings are carried into storage word-for-word, so a history-served
	// reading shows exactly the checks and metrics the live one did. Absent
	// (nil) when no checks ran — the wire shape's `findings` block is
	// itself omitempty, mirroring that absence here.
	if c.Findings != nil {
		r.Checks = c.Findings.Checks
		r.Metrics = c.Findings.Metrics
	}

	for _, d := range c.DependsOn {
		r.DependsOn = append(r.DependsOn, d.Code)
	}

	for _, rung := range c.Rungs {
		out := Rung{
			SendAmount: rung.SendAmount,
			Priced:     rung.Priced,
			Integrity:  rung.Integrity,
		}
		if rung.Quote != nil {
			out.ReceiveAmount = rung.Quote.ReceiveAmount
			out.EffectiveRate = rung.Quote.EffectiveRate
			out.LossPct = rung.Quote.LossPct
			out.Verdict = rung.Quote.Verdict
			out.Path = rung.Quote.Description
		}
		r.Rungs = append(r.Rungs, out)
	}

	// Recommended stays nil when no size was acceptable. That is the normal
	// shape of a broken corridor and must survive into storage: a history
	// that filled the gap with the best-scoring quote would record a
	// recommendation the monitor refused to make.
	if c.Recommended != nil {
		r.Recommended = &Rung{
			SendAmount:    c.RecommendedSize,
			Priced:        true,
			Integrity:     c.Integrity,
			ReceiveAmount: c.Recommended.ReceiveAmount,
			EffectiveRate: c.Recommended.EffectiveRate,
			LossPct:       c.Recommended.LossPct,
			Verdict:       c.Recommended.Verdict,
			Path:          c.Recommended.Description,
		}
	}
	return r
}

// scoredAgainst names the source a corridor's verdicts were graded against,
// or empty when the corridor was never scorable — an unscorable rate never
// produced a verdict, so this must not claim one did.
func scoredAgainst(c route.CorridorJSON) string {
	if !c.Scored {
		return ""
	}
	return c.ReferenceSource
}

// Nop is a Store that discards everything.
//
// It exists so callers never branch on a nil store. wayfared with no history
// configured serves live measurements exactly as it did before this package
// existed, and the scheduler runs without one.
type Nop struct{}

func (Nop) Append(context.Context, *Record) error                  { return nil }
func (Nop) Latest(context.Context, string) (*Record, error)        { return nil, nil }
func (Nop) Recent(context.Context, string, int) ([]*Record, error) { return nil, nil }
func (Nop) All(context.Context, string) ([]*Record, error)         { return nil, nil }
func (Nop) Verify(context.Context, string) error                   { return nil }
func (Nop) Corridors(context.Context) ([]string, error)            { return nil, nil }
