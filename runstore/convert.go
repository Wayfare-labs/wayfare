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
		Reference: Reference{
			Mid:    c.ReferenceMid,
			Source: c.ReferenceSource,
		},
		FloorLossPct:    c.Floor,
		FloorSize:       c.FloorSize,
		WorstLossPct:    c.WorstLoss,
		WorstSize:       c.WorstSize,
		RecommendedSize: c.RecommendedSize,
		Finding:         c.Finding,
		Rungs:           make([]Rung, 0, len(c.Rungs)),
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
