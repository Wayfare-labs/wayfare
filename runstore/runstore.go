// Package runstore keeps a tamper-evident history of corridor measurements.
//
// # Why a hash chain
//
// The project's central claim is of the form "measured live on 2026-08-08,
// USDC to NGNC lost 25.02% at 0.1 USDC". A reader has to take two things on
// trust: that the measurement happened, and that nobody adjusted it
// afterwards. The first is what snapshots address. This package addresses the
// second.
//
// Each record carries the hash of the one before it, so a stored history is a
// chain rather than a pile. Editing any past record — a loss percentage, a
// timestamp, an integrity state — changes its hash, which breaks every record
// after it. Verify walks the chain and names the first record that does not
// reconcile.
//
// This does not prove a measurement was correct. It proves the stored history
// is the one that was written, which is the part a reader cannot otherwise
// check. A monitor whose past can be quietly rewritten is a monitor whose
// present cannot be trusted either.
//
// # What it is not
//
// Not a blockchain and not a distributed ledger. There is one writer and the
// chain lives in a file. Someone with write access can rewrite the whole chain
// from any point; what they cannot do is edit one record and leave the rest
// intact, which is the realistic failure — a number quietly improved long
// after it was published.
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
)

// Version is the record schema version.
//
// It is part of the hashed preimage, so a bump invalidates existing chains by
// construction. See the preimage rules on Record.
// Version is the record schema version.
//
// It is part of the hashed preimage, so a bump invalidates existing chains by
// construction — unless the new fields are added with omitempty, in which case
// a Version 1 record (whose new blocks are empty) still encodes to exactly the
// bytes that were hashed when it was written, and still verifies. See the
// preimage rules on Record and the migration note in docs/run-store.md.
const Version = 2

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
	// unchanged and still verify under this (Version 2) build. See
	// docs/run-store.md for the migration.
	Checks  []checks.CheckJSON  `json:"checks,omitempty"`
	Metrics []checks.MetricJSON `json:"metrics,omitempty"`

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
