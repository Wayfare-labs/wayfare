package runstore

import (
	"fmt"
	"time"
)

// ClockSkewTolerance is how far a record's timestamp may precede the chain tip
// and still be appended.
//
// The check it exists for catches a host whose clock is wrong: such a record is
// cryptographically valid, so the hash chain accepts it, and every trend then
// draws a measurement that claims to have happened in the past. A tolerance of
// zero would reject that failure perfectly and reject two innocent ones with it,
// so the window is sized from what is genuinely innocent rather than picked for
// a round number:
//
//   - a live measurement is bounded by the server's -timeout, 90s by default, so
//     two sweeps that overlap (the deployed scheduler and a CI -once run) can
//     finish in the opposite order to the one they started in, and the earlier
//     record then legitimately lands a little behind the tip;
//   - a corrected clock slews by seconds, and a VM resumed from suspend steps by
//     about the time it was asleep, which is the failure worth refusing.
//
// Two minutes clears the first with room to spare and is 0.6% of the six-hour
// measurement interval, which is far too small to reorder two real
// measurements. It is not a value the store aims at or a guarantee that anything
// inside it is trustworthy; it is the width of the doubt the check grants.
const ClockSkewTolerance = 2 * time.Minute

// ErrOutOfOrder is returned by Append when a record is timestamped far enough
// before the chain tip to look like a clock that moved backwards rather than a
// measurement that arrived late.
//
// It is a typed error because the caller has to tell this refusal apart from
// every other Append failure. The scheduler logs it and keeps going, and an
// operator reading that log needs to know whether to fix the host's clock
// (#703), wait for the upstream to stop rate-limiting it, or leave it alone.
type ErrOutOfOrder struct {
	Corridor string

	// TipSeq and TipRecordedAt identify the record the new one was compared
	// against: the chain's current end.
	TipSeq        int64
	TipRecordedAt time.Time

	// RecordedAt is the timestamp the rejected record carried, as it would
	// have been stored (UTC, truncated to the second).
	RecordedAt time.Time

	// By is how far RecordedAt precedes TipRecordedAt. It is reported because
	// the distance is the diagnosis: seconds is an overlapping sweep, hours is
	// a clock that stepped, and days is a host that should not be measuring.
	By time.Duration

	// Tolerance is the ClockSkewTolerance in force when the record was
	// rejected, so the message explains the verdict without a lookup.
	Tolerance time.Duration
}

func (e *ErrOutOfOrder) Error() string {
	return fmt.Sprintf(
		"runstore: %s: refusing to append a record timed %s, %s before the chain tip "+
			"(seq %d, timed %s); the clock-skew tolerance is %s",
		e.Corridor,
		e.RecordedAt.UTC().Format(time.RFC3339),
		humanSpan(e.By),
		e.TipSeq,
		e.TipRecordedAt.UTC().Format(time.RFC3339),
		humanSpan(e.Tolerance),
	)
}

// OrderViolation names one step backwards in a stored chain: the record at
// From is timestamped earlier than the record at To that precedes it.
//
// The pair is reported rather than a count because the fix is always specific to
// one record. A reader wants to know which sweep ran on which host and when, and
// the two timestamps are what identify them.
type OrderViolation struct {
	Corridor string

	// FromSeq and FromRecordedAt are the earlier position in the file; ToSeq
	// and ToRecordedAt are the record that steps back from it.
	FromSeq        int64
	FromRecordedAt time.Time
	ToSeq          int64
	ToRecordedAt   time.Time
	By             time.Duration
}

// String renders the violation as one clause, for a report line that already
// names the corridor.
func (v OrderViolation) String() string {
	return fmt.Sprintf(
		"seq %d timed %s is %s before seq %d timed %s, past the %s clock-skew tolerance",
		v.ToSeq, v.ToRecordedAt.UTC().Format(time.RFC3339), humanSpan(v.By),
		v.FromSeq, v.FromRecordedAt.UTC().Format(time.RFC3339), humanSpan(ClockSkewTolerance),
	)
}

// OrderViolations returns every step backwards in an already-stored chain, in
// the order they occur.
//
// It applies the same rule Append does, so a chain that Append could have
// written never reports a violation, and the only ways to get one are history
// written before the rule existed or history written by an older build. That
// equivalence is the point: a check the verifier disagrees with would turn
// -verify-store against the store's own accepted output.
//
// It is a separate function rather than part of verifyChain because a step
// backwards is a different kind of bad than a broken hash. The chain still
// proves nobody edited the records, so the service can serve and rotate them;
// what it cannot do is pretend the sequence of times means anything. Folding
// this into verifyChain would make it fatal at Open, and a deployment would
// then be down to a clock warp in month-old history.
//
// Records with a zero timestamp are skipped rather than reported. Append gives
// one a timestamp, so a zero only reaches here from a hand-edited or partial
// file, and reading a backwards step out of it would be an invention.
func OrderViolations(records []*Record) []OrderViolation {
	var out []OrderViolation

	var prev *Record
	for _, r := range records {
		if r == nil || r.RecordedAt.IsZero() {
			continue
		}
		if prev != nil && r.RecordedAt.Add(ClockSkewTolerance).Before(prev.RecordedAt) {
			out = append(out, OrderViolation{
				Corridor:       r.Corridor,
				FromSeq:        prev.Seq,
				FromRecordedAt: prev.RecordedAt,
				ToSeq:          r.Seq,
				ToRecordedAt:   r.RecordedAt,
				By:             prev.RecordedAt.Sub(r.RecordedAt),
			})
		}
		prev = r
	}
	return out
}

// humanSpan shortens a duration for a report line, keeping the unit that carries
// the diagnosis (seconds, minutes, hours, days) instead of printing 131201s.
func humanSpan(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%.1fd", d.Hours()/24)
	case d >= time.Hour:
		return fmt.Sprintf("%.1fh", d.Hours())
	case d >= time.Minute:
		return fmt.Sprintf("%.0fm", d.Minutes())
	default:
		return d.Round(time.Second).String()
	}
}
