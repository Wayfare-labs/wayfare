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
	return out
}
