package checks

import "time"

// EvidenceJSON is one recorded observation on the wire.
type EvidenceJSON struct {
	Source     string `json:"source"`
	Observed   string `json:"observed"`
	ObservedAt string `json:"observed_at"`
}

// CheckJSON is one check result on the wire.
//
// Determined and Passed are separate fields here for the same reason they are
// separate in Go: a client that saw only `passed: false` could not tell a
// failure from an unknown, and would render "this anchor has no SEP-10
// endpoint" identically to "this anchor's SEP-10 endpoint is dead".
type CheckJSON struct {
	ID       string `json:"id"`
	Scope    string `json:"scope"`
	Subject  string `json:"subject"`
	Severity string `json:"severity"`

	Determined bool   `json:"determined"`
	Passed     bool   `json:"passed"`
	Reason     string `json:"reason,omitempty"`
	Summary    string `json:"summary"`

	Evidence   []EvidenceJSON `json:"evidence"`
	ObservedAt string         `json:"observed_at"`
}

// MetricJSON is one metric result on the wire.
type MetricJSON struct {
	ID      string `json:"id"`
	Scope   string `json:"scope"`
	Subject string `json:"subject"`

	Determined bool   `json:"determined"`
	Reason     string `json:"reason,omitempty"`
	Value      string `json:"value,omitempty"`
	Unit       string `json:"unit"`

	// Venue names the liquidity source the metric observed —
	// "order-book" or "pathfinding". A consumer must never reconcile two
	// figures with different venues by arithmetic: the book excludes AMM
	// liquidity while pathfinding includes it, so the two describe different
	// markets. See docs/liquidity-venues.md for the reconciliation rule.
	// Empty for anchor and asset metrics.
	Venue string `json:"venue,omitempty"`

	Summary string `json:"summary"`

	Evidence   []EvidenceJSON `json:"evidence"`
	ObservedAt string         `json:"observed_at"`
}

// FindingsJSON is a corridor's check results.
//
// It carries counts so a client can render a summary without walking the list,
// and deliberately carries nothing that could be mistaken for a verdict or an
// integrity state. Those stay where they are computed.
type FindingsJSON struct {
	Checks []CheckJSON `json:"checks"`

	Passed       int `json:"passed"`
	Failed       int `json:"failed"`
	Undetermined int `json:"undetermined"`

	// WorstSeverity is the highest severity among *failed* checks, empty
	// when nothing failed. Undetermined results never contribute.
	WorstSeverity string `json:"worst_severity,omitempty"`

	// Metrics are measured quantities, reported alongside checks.
	Metrics []MetricJSON `json:"metrics,omitempty"`
}

// ToJSON renders findings for the wire, ordered for display.
func (f *Findings) ToJSON() FindingsJSON {
	out := FindingsJSON{Checks: make([]CheckJSON, 0, len(f.Checks))}
	out.Passed, out.Failed, out.Undetermined = f.Counts()

	if worst, any := f.Worst(); any {
		out.WorstSeverity = worst.String()
	}

	for _, r := range f.Sorted() {
		c := CheckJSON{
			ID:         r.ID,
			Scope:      r.Scope.String(),
			Subject:    r.Subject,
			Severity:   r.Severity.String(),
			Determined: r.Determined,
			Passed:     r.Determined && r.Passed,
			Reason:     r.Reason,
			Summary:    r.Summary,
			Evidence:   make([]EvidenceJSON, 0, len(r.Evidence)),
			ObservedAt: r.At.UTC().Format(time.RFC3339),
		}
		for _, e := range r.Evidence {
			c.Evidence = append(c.Evidence, EvidenceJSON{
				Source:     e.Source,
				Observed:   e.Observed,
				ObservedAt: e.ObservedAt.UTC().Format(time.RFC3339),
			})
		}
		out.Checks = append(out.Checks, c)
	}

	if len(f.Metrics) > 0 {
		out.Metrics = make([]MetricJSON, 0, len(f.Metrics))
		for _, m := range f.Metrics {
			mj := MetricJSON{
				ID:         m.ID,
				Scope:      m.Scope.String(),
				Subject:    m.Subject,
				Determined: m.Determined,
				Reason:     m.Reason,
				Unit:       string(m.Unit),
				Venue:      string(m.Venue),
				Summary:    m.Summary,
				Evidence:   make([]EvidenceJSON, 0, len(m.Evidence)),
				ObservedAt: m.At.UTC().Format(time.RFC3339),
			}
			if m.Determined {
				mj.Value = m.Value.String()
			}
			for _, e := range m.Evidence {
				mj.Evidence = append(mj.Evidence, EvidenceJSON{
					Source:     e.Source,
					Observed:   e.Observed,
					ObservedAt: e.ObservedAt.UTC().Format(time.RFC3339),
				})
			}
			out.Metrics = append(out.Metrics, mj)
		}
	}

	return out
}
