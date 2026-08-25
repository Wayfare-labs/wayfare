// Package runstore keeps a tamper-evident history of corridor measurements.
//
// This file implements integrity transition detection: comparing consecutive
// runs for the same corridor to detect structural changes in market integrity.
package runstore

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// IntegrityTransition represents a structural change in a corridor's integrity
// state between two consecutive runs.
type IntegrityTransition struct {
	// Corridor is the corridor key (e.g., "USDC-NGNC").
	Corridor string `json:"corridor"`

	// PreviousIntegrity is the integrity state before the transition.
	PreviousIntegrity string `json:"previous_integrity"`

	// CurrentIntegrity is the integrity state after the transition.
	CurrentIntegrity string `json:"current_integrity"`

	// PreviousRunAt is when the previous run was recorded.
	PreviousRunAt time.Time `json:"previous_run_at"`

	// CurrentRunAt is when the current run was recorded.
	CurrentRunAt time.Time `json:"current_run_at"`

	// PreviousDependsOn is the dependency set before the transition.
	PreviousDependsOn []string `json:"previous_depends_on"`

	// CurrentDependsOn is the dependency set after the transition.
	CurrentDependsOn []string `json:"current_depends_on"`

	// PreviousReferenceSource is the rate provider used for the previous run.
	PreviousReferenceSource string `json:"previous_reference_source"`

	// CurrentReferenceSource is the rate provider used for the current run.
	CurrentReferenceSource string `json:"current_reference_source"`

	// TransitionType describes the nature of the transition.
	TransitionType TransitionType `json:"transition_type"`
}

// TransitionType categorizes the structural change.
type TransitionType int

const (
	// TransitionIntegrityStateChange means the integrity state itself changed
	// (e.g., DIRECT → DERIVATIVE, DERIVATIVE → NO-MARKET).
	TransitionIntegrityStateChange TransitionType = iota

	// TransitionDependsOnChanged means the integrity state stayed the same
	// (typically DERIVATIVE) but the set of assets it depends on changed.
	TransitionDependsOnChanged
)

func (t TransitionType) String() string {
	switch t {
	case TransitionIntegrityStateChange:
		return "INTEGRITY_STATE_CHANGE"
	case TransitionDependsOnChanged:
		return "DEPENDS_ON_CHANGED"
	default:
		return "UNKNOWN"
	}
}

// Describe returns a human-readable description of the transition.
func (it *IntegrityTransition) Describe() string {
	switch it.TransitionType {
	case TransitionIntegrityStateChange:
		return fmt.Sprintf(
			"integrity changed from %s to %s (previous: %s, current: %s)",
			it.PreviousIntegrity, it.CurrentIntegrity,
			it.PreviousRunAt.UTC().Format(time.RFC3339),
			it.CurrentRunAt.UTC().Format(time.RFC3339))
	case TransitionDependsOnChanged:
		return fmt.Sprintf(
			"depends_on changed from [%s] to [%s] while integrity stayed %s (previous: %s, current: %s)",
			strings.Join(it.PreviousDependsOn, ", "),
			strings.Join(it.CurrentDependsOn, ", "),
			it.CurrentIntegrity,
			it.PreviousRunAt.UTC().Format(time.RFC3339),
			it.CurrentRunAt.UTC().Format(time.RFC3339))
	default:
		return "unknown transition"
	}
}

// DetectTransitions compares consecutive stored runs for a corridor and
// returns any structural transitions found.
//
// UNKNOWN integrity states are deliberately excluded from raising structural
// alerts. IntegrityUnknown means the corridor's structure was not established
// (typically because pricing failed for an unrelated reason like a Horizon
// timeout or rate provider outage). It does not mean the corridor changed.
// Alerting on DIRECT → UNKNOWN would turn every transient network failure
// into a false alarm about a corridor's structure, and a monitor that cries
// wolf gets muted, which costs more than it ever saved.
//
// The detection is idempotent: re-running it over the same history produces
// the same transitions, not duplicates.
func DetectTransitions(ctx context.Context, store Store, corridor string) ([]*IntegrityTransition, error) {
	// Load the complete corridor history to find all transitions.
	// Using Recent(100) would miss older transitions in corridors with
	// more than 100 runs.
	records, err := store.All(ctx, corridor)
	if err != nil {
		return nil, fmt.Errorf("runstore: fetching history for %s: %w", corridor, err)
	}

	// Need at least 2 records to compare.
	if len(records) < 2 {
		return nil, nil
	}

	// All() returns records in chronological order (oldest first).
	var transitions []*IntegrityTransition

	for i := 1; i < len(records); i++ {
		prev := records[i-1]
		curr := records[i]

		transition := compareRuns(prev, curr)
		if transition != nil {
			transitions = append(transitions, transition)
		}
	}

	return transitions, nil
}

// DetectLatestTransition compares only the two most recent runs for a corridor.
// This is the efficient path for the scheduled monitor that checks after each
// measurement.
func DetectLatestTransition(ctx context.Context, store Store, corridor string) (*IntegrityTransition, error) {
	// We need exactly 2 records: the latest and the one before it.
	records, err := store.Recent(ctx, corridor, 2)
	if err != nil {
		return nil, fmt.Errorf("runstore: fetching history for %s: %w", corridor, err)
	}

	// Need at least 2 records to compare.
	if len(records) < 2 {
		return nil, nil
	}

	// Recent returns newest first, so [0] is current, [1] is previous.
	return compareRuns(records[1], records[0]), nil
}

// compareRuns compares two consecutive runs and returns a transition if one
// occurred. Returns nil if no structural change was detected.
//
// Transitions involving UNKNOWN are filtered out with deliberate care:
//   - UNKNOWN → X: The previous state was unknown, so we cannot determine if
//     a structural change occurred. This is common after transient failures.
//   - X → UNKNOWN: The current state is unknown, so we cannot determine the
//     new structure. This is likely a transient failure, not a real change.
//   - UNKNOWN → UNKNOWN: Nothing was learned either way.
func compareRuns(prev, curr *Record) *IntegrityTransition {
	// Skip transitions involving UNKNOWN on either side. See the doc
	// comment on DetectTransitions for why this matters.
	if prev.Integrity == "UNKNOWN" || curr.Integrity == "UNKNOWN" {
		return nil
	}

	// No change in integrity state — check if DependsOn changed while
	// staying in DERIVATIVE.
	if prev.Integrity == curr.Integrity {
		if curr.Integrity == "DERIVATIVE" && !depsetsEqual(prev.DependsOn, curr.DependsOn) {
			return &IntegrityTransition{
				Corridor:                curr.Corridor,
				PreviousIntegrity:       prev.Integrity,
				CurrentIntegrity:        curr.Integrity,
				PreviousRunAt:           prev.RecordedAt,
				CurrentRunAt:            curr.RecordedAt,
				PreviousDependsOn:       prev.DependsOn,
				CurrentDependsOn:        curr.DependsOn,
				PreviousReferenceSource: prev.Reference.Source,
				CurrentReferenceSource:  curr.Reference.Source,
				TransitionType:          TransitionDependsOnChanged,
			}
		}
		return nil
	}

	// Integrity state changed.
	return &IntegrityTransition{
		Corridor:                curr.Corridor,
		PreviousIntegrity:       prev.Integrity,
		CurrentIntegrity:        curr.Integrity,
		PreviousRunAt:           prev.RecordedAt,
		CurrentRunAt:            curr.RecordedAt,
		PreviousDependsOn:       prev.DependsOn,
		CurrentDependsOn:        curr.DependsOn,
		PreviousReferenceSource: prev.Reference.Source,
		CurrentReferenceSource:  curr.Reference.Source,
		TransitionType:          TransitionIntegrityStateChange,
	}
}

// depsetsEqual compares two sorted string slices for equality.
func depsetsEqual(a, b []string) bool {
	// Deduplicate each input before comparing, so duplicate values
	// are treated as a single dependency.
	a = dedup(a)
	b = dedup(b)
	if len(a) != len(b) {
		return false
	}
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// dedup removes duplicate strings from a sorted or unsorted slice,
// returning a sorted slice of unique values.
func dedup(in []string) []string {
	if len(in) <= 1 {
		return in
	}
	// Sort first so duplicates are adjacent.
	sorted := make([]string, len(in))
	copy(sorted, in)
	sort.Strings(sorted)
	out := sorted[:1]
	for _, s := range sorted[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}
