package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/Wayfare-labs/wayfare/runstore"
)

// exportFormatCSV is the only export format today. It is named explicitly
// rather than assumed so `-format` never silently produces something the
// caller did not ask for.
const exportFormatCSV = "csv"

// csvColumns is the export's column contract, in order.
//
// # Why these columns
//
// The store is NDJSON with a nested `reference` block and a full `rungs` array
// per record. That is the right shape for provenance and the wrong shape for
// a spreadsheet: a researcher has to flatten it before a single pivot table
// works, and every flattening is a chance to drop or reorder a figure. So the
// export publishes a header once and one row per record, with the money and
// rate figures exactly as stored.
//
// The set is deliberately the record's *headline* facts — what was measured,
// how it was classified, how bad it was, and which benchmark it was scored
// against. Per-rung detail stays in the NDJSON history; this export is one row
// per measurement, not one row per size.
//
// # What is NOT here
//
// No computed column, no rounding, no percentage re-derived from other
// columns. Every value is a stored string copied through unchanged, so a row
// can be traced back to the record it came from and a reader can recompute
// anything the store did not already state. In particular the loss figures stay
// decimal strings: parsing them into spreadsheet floats at export time is the
// same binary-floating-point loss this project refuses everywhere else, and it
// would make the exported number differ from the published one.
var csvColumns = []string{
	"recorded_at",
	"corridor",
	"seq",
	"integrity",
	"floor_loss_pct",
	"floor_size",
	"worst_loss_pct",
	"worst_size",
	"reference_mid",
	"reference_source",
	"reference_as_of",
	"reference_fetched_at",
	"reference_scored_against",
	"reference_secondary_mid",
	"reference_secondary_source",
	"reference_secondary_as_of",
	"reference_divergence_pct",
}

// exportChain writes one corridor's verified history as CSV.
//
// # Why verification comes first
//
// The store's central claim is that a published figure can be traced to a
// record nobody edited afterwards. An export is the artifact a journalist or
// researcher takes away from this project, so exporting an unverified chain
// would launder that claim: the CSV would carry no hashes, and a reader
// comparing it with the committed history could not tell a faithful copy from
// a doctored one. Verification is therefore a precondition, not a warning.
//
// Nothing is written before the chain passes. A refused export must produce no
// partial file — a truncated CSV looks like a short history rather than a
// failed verification, and nothing on the artifact itself says which.
//
// Exit codes follow -verify-store: 0 exported, 1 the chain failed
// verification, 2 the export could not run (no store, unknown corridor,
// unsupported format).
func exportChain(store runstore.Store, corridor, format string, w io.Writer, logger *slog.Logger) int {
	if store == nil {
		logger.Error("-export requires a store; none is configured")
		return 2
	}
	if !strings.EqualFold(strings.TrimSpace(format), exportFormatCSV) {
		logger.Error("unsupported export format; only csv is implemented",
			"format", format)
		return 2
	}

	key := strings.ToUpper(strings.TrimSpace(corridor))
	if key == "" {
		logger.Error("-export requires a corridor, e.g. -export USDC-NGNC")
		return 2
	}

	ctx := context.Background()

	// Verify before reading anything for output. Verify re-reads the file, so
	// this checks the bytes on disk rather than a cached tip.
	if err := store.Verify(ctx, key); err != nil {
		logger.Error("refusing to export: chain failed verification",
			"corridor", key, "error", err)
		return 1
	}

	records, err := store.All(ctx, key)
	if err != nil {
		logger.Error("reading corridor history", "corridor", key, "error", err)
		return 2
	}
	if len(records) == 0 {
		// An unexportable corridor is a usage error, not a measurement of
		// nothing. Writing a header-only CSV would look like a corridor that
		// was measured and produced no rows.
		logger.Error("no history for corridor", "corridor", key)
		return 2
	}

	cw := csv.NewWriter(w)
	if err := cw.Write(csvColumns); err != nil {
		logger.Error("writing csv header", "corridor", key, "error", err)
		return 2
	}
	for _, r := range records {
		if err := cw.Write(csvRow(r)); err != nil {
			logger.Error("writing csv row", "corridor", key, "seq", r.Seq, "error", err)
			return 2
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		logger.Error("flushing csv", "corridor", key, "error", err)
		return 2
	}

	logger.Info("exported corridor history",
		"corridor", key, "records", len(records), "format", exportFormatCSV)
	return 0
}

// csvRow renders one record in csvColumns order.
//
// Values are copied from the record verbatim. The only transformations are the
// two that a human-readable export cannot avoid: the timestamp is normalised
// to UTC RFC3339 (the store's own format, and the one every other surface
// publishes), and seq is rendered as a decimal integer.
func csvRow(r *runstore.Record) []string {
	return []string{
		r.RecordedAt.UTC().Format(time.RFC3339),
		r.Corridor,
		fmt.Sprintf("%d", r.Seq),
		r.Integrity,
		r.FloorLossPct,
		r.FloorSize,
		r.WorstLossPct,
		r.WorstSize,
		r.Reference.Mid,
		r.Reference.Source,
		r.Reference.AsOf,
		r.Reference.FetchedAt,
		r.Reference.ScoredAgainst,
		r.Reference.SecondaryMid,
		r.Reference.SecondarySource,
		r.Reference.SecondaryAsOf,
		r.Reference.DivergencePct,
	}
}
