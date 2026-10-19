package route

// MoneyStrings returns every money-valued string in a corridor document, in
// document order, including the per-rung quotes, the recommended quote and
// the cost blocks.
//
// Money crosses the wire as decimal strings — never JSON numbers — and this
// walk is what the boundary tests feed to decimal.NewFromString for every
// producer of the shape: the HTTP API's live document, the stored (stale)
// document, and cmd/ladder's -json mode. The README's "test at the boundary"
// is those tests (backlog #6).
//
// The walk mirrors the wire's omitempty semantics exactly, and that matters
// in both directions. A field declared omitempty is returned only when
// non-empty: an empty one never crosses the wire, so returning it would test
// a string a client never receives. A field declared without omitempty is
// returned even when empty: an empty string there WOULD cross the wire, and
// must fail parsing rather than hide behind the walker.
func MoneyStrings(c CorridorJSON) []string {
	out := make([]string, 0, 20)
	add := func(s string) { out = append(out, s) }

	// Top-level money fields. Those without omitempty are always on the
	// wire, so they are added unconditionally; those with omitempty only
	// when non-empty.
	add(c.ReferenceMid)
	if c.ReferenceSecondaryMid != "" {
		add(c.ReferenceSecondaryMid)
	}
	if c.ReferenceDivergencePct != "" {
		add(c.ReferenceDivergencePct)
	}
	if c.ParallelMid != "" {
		add(c.ParallelMid)
	}
	if c.ParallelGapPct != "" {
		add(c.ParallelGapPct)
	}
	add(c.Floor)
	add(c.FloorSize)
	add(c.WorstLoss)
	add(c.WorstSize)
	if c.RecommendedSize != "" {
		add(c.RecommendedSize)
	}

	if c.Recommended != nil {
		add(c.Recommended.ReceiveAmount)
		add(c.Recommended.EffectiveRate)
		add(c.Recommended.LossPct)
		if c.Recommended.LossAmount != "" {
			add(c.Recommended.LossAmount)
		}
	}

	for _, r := range c.Rungs {
		add(r.SendAmount)
		if r.Quote != nil {
			add(r.Quote.ReceiveAmount)
			add(r.Quote.EffectiveRate)
			add(r.Quote.LossPct)
			if r.Quote.LossAmount != "" {
				add(r.Quote.LossAmount)
			}
		}
		if r.Cost != nil {
			add(r.Cost.TotalLossPct)
			for _, p := range r.Cost.Parts {
				// Undetermined parts carry no amount or pct at all
				// (ToCostBlockJSON omits them), so nothing to walk.
				if p.Amount != "" {
					add(p.Amount)
				}
				if p.Pct != "" {
					add(p.Pct)
				}
			}
		}
	}
	return out
}
