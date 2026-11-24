package runstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/Wayfare-labs/wayfare/route"
)

// Version is the current record schema version.
//
// It is part of the hashed preimage, so a bump invalidates existing chains by
// construction — unless the new fields are added with omitempty, in which case
// a record written by an older build (whose new fields are empty) still
// encodes to exactly the bytes that were hashed when it was written, and
// still verifies. Version 4 adds snapshot this way after every Version 3
// field, and Version 5 adds correction this way after every Version 4 field,
// so older records still verify unchanged. Version 6 adds ledger_range and
// a per-rung ledger the same way (issue #720). See docs/run-store.md.
const Version = 6

// LegacyVersion is the prior record schema, retained for existing chains.
const LegacyVersion = 1

// GenesisPrevHash is the prev_hash of the first record in a chain.
const GenesisPrevHash = "sha256:" + "0000000000000000000000000000000000000000000000000000000000000000"

// Reference records both mids behind a measurement.
//
// Both are carried, always, even when they agree. A corridor's numbers moving
// because the benchmark changed is a completely different event from the
// corridor moving, and a history that records only the mid it scored against
// cannot tell the two apart afterwards.
type Reference struct {
	Mid    string `json:"mid"`
	Source string `json:"source"`
	AsOf   string `json:"as_of"`

	SecondaryMid    string `json:"secondary_mid,omitempty"`
	SecondarySource string `json:"secondary_source,omitempty"`
	SecondaryAsOf   string `json:"secondary_as_of,omitempty"`

	// DivergencePct is the disagreement between the two mids, as a decimal
	// string. Empty when only one provider answered.
	DivergencePct string `json:"divergence_pct,omitempty"`

	// ScoredAgainst names which source produced the verdicts in this
	// record, so a reader can always tell which mid they are looking at.
	ScoredAgainst string `json:"scored_against,omitempty"`

	// FetchedAt is when this project last obtained the rate from the
	// provider, which differs from AsOf: AsOf is the upstream's own stamp,
	// FetchedAt is when we asked. A cached rate has an older FetchedAt and
	// an unchanged AsOf, and a reader needs both to judge how current the
	// benchmark was when a stored reading was taken.
	//
	// Declared omitempty and AFTER every Version 2 field, so a Version 2
	// record (which has no fetched_at) still encodes to byte-for-byte the
	// same JSON — and therefore the same hash — it did before this field
	// existed. This is the Version 3 migration; see docs/run-store.md.
	FetchedAt string `json:"fetched_at,omitempty"`

	// The diagnostic provider's attribution of a DISAGREE (issue #597).
	//
	// These are optional, declared omitempty and after every earlier field,
	// on the same rule as FetchedAt: a record written before the diagnostic
	// existed encodes to exactly the bytes it always did and still verifies.
	// A record written when the third provider answered carries what it
	// observed; a record written when it did not simply omits the block.
	DiagnosticSource             string `json:"diagnostic_source,omitempty"`
	DiagnosticMid                string `json:"diagnostic_mid,omitempty"`
	DiagnosticAsOf               string `json:"diagnostic_as_of,omitempty"`
	DiagnosticOutlier            string `json:"diagnostic_outlier,omitempty"`
	DiagnosticOutlierDistancePct string `json:"diagnostic_outlier_distance_pct,omitempty"`
	DiagnosticOtherDistancePct   string `json:"diagnostic_other_distance_pct,omitempty"`
	DiagnosticReason             string `json:"diagnostic_reason,omitempty"`
}

// Rung is one size's stored result.
type Rung struct {
	SendAmount    string `json:"send_amount"`
	Priced        bool   `json:"priced"`
	Integrity     string `json:"integrity"`
	ReceiveAmount string `json:"receive_amount,omitempty"`
	EffectiveRate string `json:"effective_rate,omitempty"`
	LossPct       string `json:"loss_pct,omitempty"`
	Verdict       string `json:"verdict,omitempty"`
	Path          string `json:"path,omitempty"`

	// Ledger is the chain head observed right after the rung was priced
	// (Version 6, issue #720). omitempty and after every earlier field, so a
	// rung stored before Version 6 encodes to exactly the bytes it did.
	Ledger int64 `json:"ledger,omitempty"`
}

// LedgerRange is the first and last ledger observed across a measurement's
// rungs (Version 6, issue #720). A ladder takes several ledgers to price, so
// its rungs can reflect different books; the range lets a reader discard a
// measurement that straddled a large book change.
type LedgerRange struct {
	First int64 `json:"first"`
	Last  int64 `json:"last"`
}

// Correction records an erratum or correction against an earlier record in the chain (issue #729).
//
// The chain is append-only: earlier records are never edited in place or deleted, which would
// break cryptographic hash linkage. Instead, an erratum is recorded by appending a new
// record with Correction populated, referencing the target sequence number, stating what
// was wrong and why, and carrying the corrected figures.
type Correction struct {
	TargetSeq int64  `json:"target_seq"`
	Reason    string `json:"reason"`
}

// Record is one stored corridor measurement.
//
// # The preimage rule
//
// hash = sha256(preimage), where the preimage is this struct's JSON encoding
// with Hash omitted, produced by encoding/json with SetEscapeHTML(false) and
// no indentation. Go emits struct fields in declaration order, so the field
// set, the field order, and the encoding settings are all part of the hash.
//
// PrevHash is inside the preimage. That is what chains the records: altering
// an earlier record changes its hash, which changes the next record's
// preimage, and so on to the end.
//
// **Adding, removing, or reordering a field changes every hash and is
// therefore a Version bump plus a migration, never a compatible change.**
// TestRecordHashIsPinned exists to make that fail in CI rather than in review.
type Record struct {
	Version    int       `json:"version"`
	Seq        int64     `json:"seq"`
	RecordedAt time.Time `json:"recorded_at"`
	Corridor   string    `json:"corridor"`

	Integrity string   `json:"integrity"`
	DependsOn []string `json:"depends_on"`

	Reference Reference `json:"reference"`

	FloorLossPct string `json:"floor_loss_pct"`
	FloorSize    string `json:"floor_size"`
	WorstLossPct string `json:"worst_loss_pct"`
	WorstSize    string `json:"worst_size"`

	// Recommended is nil when no size produced an acceptable route, which
	// is the normal shape of a broken corridor. Stored as null rather than
	// omitted so a reader cannot mistake absence for an oversight.
	Recommended     *Rung  `json:"recommended"`
	RecommendedSize string `json:"recommended_size,omitempty"`

	Finding string `json:"finding"`
	Rungs   []Rung `json:"rungs"`

	// Checks and Metrics are the findings taken with this measurement:
	// facts about the counterparties a corridor depends on, and measured
	// quantities. They ride on the wire as checks.FindingsJSON and are
	// stored word-for-word so a history-served reading shows the same
	// findings the live one did — the stale path has nowhere else to get
	// them. Absent when no checks ran: a Version 1 record has neither.
	//
	// These two fields are declared with omitempty and sit AFTER every
	// Version 1 field. A record with no findings therefore encodes to
	// byte-for-byte the same JSON — same field order, same contents — as it
	// did before they existed, so a Version 1 chain's hashes are
	// unchanged and still verify under this (Version 3) build. See
	// docs/run-store.md for the migration.
	Checks  []checks.CheckJSON  `json:"checks,omitempty"`
	Metrics []checks.MetricJSON `json:"metrics,omitempty"`

	// Snapshot is the content hash of the exact upstream snapshot artifact.
	// It is optional and follows every Version 3 field to preserve old hashes.
	Snapshot string `json:"snapshot,omitempty"`

	// DependencyChain is the full dependency tree, stored word-for-word
	// from the wire so the stale path can serve it back identically.
	// Absent (nil) for corridors that are not derivative, and absent for
	// records written before this field existed — the stale path treats
	// nil as "chain not available", which is honest rather than fabricated.
	DependencyChain *route.DependencyChainJSON `json:"dependency_chain,omitempty"`

	// Correction describes an erratum against an earlier sequence number (issue #729).
	// Optional; omitted when empty so pre-Version 5 record hashes verify unchanged.
	Correction *Correction `json:"correction,omitempty"`

	// LedgerRange is the span of ledgers the rungs were observed at
	// (Version 6, issue #720). Optional; omitted when unknown, and declared
	// after every Version 5 field, so pre-Version 6 hashes verify unchanged.
	LedgerRange *LedgerRange `json:"ledger_range,omitempty"`

	PrevHash string `json:"prev_hash"`
	Hash     string `json:"hash"`
}

// CorridorKey is the stable identifier for a corridor's chain.
func CorridorKey(send, receive string) string {
	return strings.ToUpper(send) + "-" + strings.ToUpper(receive)
}

// Preimage returns the exact bytes that are hashed to produce Hash.
//
// Exported because verification is a claim readers should be able to
// reproduce, and because a caller checking a record needs the same bytes the
// writer used rather than a reimplementation that might differ.
func (r *Record) Preimage() ([]byte, error) {
	clone := *r
	clone.Hash = ""

	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&clone); err != nil {
		return nil, fmt.Errorf("runstore: encoding preimage: %w", err)
	}
	// Encoder appends a newline; it is part of the preimage and must be
	// produced identically by anyone verifying.
	return []byte(b.String()), nil
}

// ComputeHash returns the record's hash without storing it.
func (r *Record) ComputeHash() (string, error) {
	pre, err := r.Preimage()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(pre)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Seal fills in Hash from the record's current contents.
//
// Any change after sealing invalidates the hash, which is the point.
func (r *Record) Seal() error {
	h, err := r.ComputeHash()
	if err != nil {
		return err
	}
	r.Hash = h
	return nil
}

// VerifySelf checks a record's hash against its own contents.
func (r *Record) VerifySelf() error {
	want, err := r.ComputeHash()
	if err != nil {
		return err
	}
	if r.Hash != want {
		return fmt.Errorf(
			"runstore: record seq %d has hash %s but its contents hash to %s; it was modified after it was written",
			r.Seq, short(r.Hash), short(want))
	}
	return nil
}

// hasLedgers reports whether the record carries any Version 6 ledger field.
func (r *Record) hasLedgers() bool {
	if r.LedgerRange != nil {
		return true
	}
	for _, rung := range r.Rungs {
		if rung.Ledger != 0 {
			return true
		}
	}
	return false
}

// IsCorrection reports whether the record is a correction of an earlier record.
func (r *Record) IsCorrection() bool {
	return r != nil && r.Correction != nil && r.Correction.TargetSeq > 0
}

func short(h string) string {
	h = strings.TrimPrefix(h, "sha256:")
	if len(h) > 12 {
		return h[:12] + "…"
	}
	return h
}

// Store is a corridor measurement history.
//
// Implementations are append-only: nothing rewrites a past run.
type Store interface {
	// Append seals r against the current chain tip and writes it.
	Append(ctx context.Context, r *Record) error

	// Latest returns the most recent record for a corridor, or nil when
	// the corridor has no history yet. A missing history is not an error.
	Latest(ctx context.Context, corridor string) (*Record, error)

	// Recent returns up to n most recent records, newest first.
	Recent(ctx context.Context, corridor string, n int) ([]*Record, error)

	// RecentBefore returns up to n records strictly before sequence number
	// 'before' (i.e. Seq < before), newest first. When before <= 0, it
	// returns up to n most recent records without cursor filtering.
	RecentBefore(ctx context.Context, corridor string, n int, before int64) ([]*Record, error)

	// All returns the complete corridor history in chronological order
	// (oldest first). Use this when the full history is needed, e.g.
	// for transition detection across the entire chain.
	All(ctx context.Context, corridor string) ([]*Record, error)

	// Verify walks a corridor's whole chain, recomputing every hash and
	// checking every link. An empty corridor verifies clean.
	Verify(ctx context.Context, corridor string) error

	// Corridors lists the corridors with stored history.
	Corridors(ctx context.Context) ([]string, error)
}
