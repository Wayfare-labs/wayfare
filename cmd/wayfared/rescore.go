package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare"
	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// rescoreAckFile is the acknowledgements file kept beside the committed
// history. It lists records whose re-derived classification is known to differ
// from what was published (for example, because the verdict thresholds were
// intentionally changed), so CI fails only on unacknowledged drift.
//
// An empty file means "no drift is acknowledged": any re-derivation mismatch
// fails the check.
const rescoreAckFile = "rescore-acknowledgements.json"

// acknowledgement records one accepted divergence between the published
// history and the current engine's re-derivation.
type acknowledgement struct {
	Corridor       string `json:"corridor"`
	Seq            int64  `json:"seq"`
	Field          string `json:"field"`
	Reason         string `json:"reason"`
	AcknowledgedAt string `json:"acknowledged_at"`
	AcknowledgedBy string `json:"acknowledged_by"`
}

// drift is one way a stored record's published classification differs from
// what the current engine re-derives from the record's stored inputs (its
// rungs and reference mid).
type drift struct {
	Corridor string
	Seq      int64
	Field    string // e.g. "integrity", "rung[0.1].verdict", "recommended"
	Stored   string // what the record published
	Derived  string // what the current engine derives
	Detail   string // extra context, such as the stored vs derived loss percentage
}

// verdictForCurrent re-implements route.verdictFor using the exported
// thresholds so the rescore tool grades loss exactly as the current engine
// does. route.verdictFor is unexported; this is the current classification
// rule, and if it changes this helper must change with it.
func verdictForCurrent(lossPct decimal.Decimal) route.Verdict {
	switch {
	case lossPct.LessThanOrEqual(route.ThresholdGood):
		return route.VerdictGood
	case lossPct.LessThanOrEqual(route.ThresholdFair):
		return route.VerdictFair
	case lossPct.LessThanOrEqual(route.ThresholdPoor):
		return route.VerdictPoor
	default:
		return route.VerdictUnusable
	}
}

// loadAcknowledgements reads the acknowledgements file. When dataDir is empty,
// it reads from the embedded history; otherwise it reads from the filesystem.
// A missing or empty file means nothing is acknowledged.
func loadAcknowledgements(dataDir string) (map[string]bool, error) {
	return loadAcknowledgementsFromFS(dataDir, wayfare.History)
}

// loadAcknowledgementsFromFS is the testable version that accepts an fs.FS for
// the embedded case.
func loadAcknowledgementsFromFS(dataDir string, embeddedFS fs.FS) (map[string]bool, error) {
	ack := map[string]bool{}
	var data []byte
	var err error
	if dataDir == "" {
		data, err = fs.ReadFile(embeddedFS, "data/rescore-acknowledgements.json")
	} else {
		data, err = os.ReadFile(filepath.Join(dataDir, rescoreAckFile))
	}
	switch {
	case err == nil:
	case os.IsNotExist(err):
		return ack, nil
	default:
		return nil, fmt.Errorf("rescore: reading acknowledgements: %w", err)
	}
	if len(data) == 0 {
		return ack, nil
	}
	var list []acknowledgement
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("rescore: parsing acknowledgements: %w", err)
	}
	for _, e := range list {
		ack[ackKey(e.Corridor, e.Seq, e.Field)] = true
	}
	return ack, nil
}

func ackKey(corridor string, seq int64, field string) string {
	return corridor + "/" + fmt.Sprintf("%d", seq) + "/" + field
}

// acknowledged reports whether this drift is listed in the acknowledgements.
func acknowledged(ack map[string]bool, d drift) bool {
	return ack[ackKey(d.Corridor, d.Seq, d.Field)]
}

// rescoreStore re-derives every classification in every committed record from
// its stored inputs and reports any record whose published classification
// differs under the current engine. It is read-only: it never writes.
//
// It exits 0 when every record re-derives identically (or every difference is
// acknowledged), 1 when there is unacknowledged drift, and 2 on a tool error.
func rescoreStore(store runstore.Store, dataDir string, logger *slog.Logger) int {
	ctx := context.Background()

	corridors, err := store.Corridors(ctx)
	if err != nil {
		logger.Error("listing corridors", "error", err)
		return 2
	}
	if len(corridors) == 0 {
		fmt.Println("no corridor history to re-score")
		return 0
	}

	ack, err := loadAcknowledgements(dataDir)
	if err != nil {
		logger.Error("loading rescore acknowledgements", "error", err)
		return 2
	}

	var drifts []drift
	checked := 0
	for _, corridor := range corridors {
		records, err := store.All(ctx, corridor)
		if err != nil {
			logger.Error("reading corridor history", "corridor", corridor, "error", err)
			return 2
		}
		for _, rec := range records {
			checked++
			drifts = append(drifts, rederiveRecord(rec, ack)...)
		}
	}

	sort.Slice(drifts, func(i, j int) bool {
		if drifts[i].Corridor != drifts[j].Corridor {
			return drifts[i].Corridor < drifts[j].Corridor
		}
		if drifts[i].Seq != drifts[j].Seq {
			return drifts[i].Seq < drifts[j].Seq
		}
		return drifts[i].Field < drifts[j].Field
	})

	unacknowledged := 0
	for _, d := range drifts {
		if acknowledged(ack, d) {
			fmt.Printf("ACKED %s seq=%d field=%s stored=%q derived=%q %s\n",
				d.Corridor, d.Seq, d.Field, d.Stored, d.Derived, d.Detail)
			continue
		}
		unacknowledged++
		fmt.Printf("DRIFT %s seq=%d field=%s stored=%q derived=%q %s\n",
			d.Corridor, d.Seq, d.Field, d.Stored, d.Derived, d.Detail)
	}

	fmt.Printf("re-scored %d record(s) across %d corridor(s): %d drift(s), %d acknowledged\n",
		checked, len(corridors), len(drifts), len(drifts)-unacknowledged)

	if unacknowledged > 0 {
		fmt.Printf("FAIL: %d unacknowledged drift(s); the published classification no longer matches the current engine\n", unacknowledged)
		return 1
	}
	fmt.Println("ok: no unacknowledged drift")
	return 0
}

// rederiveRecord re-derives one record's classifications from its stored
// inputs and returns every drift it finds against the published conclusions.
func rederiveRecord(rec *runstore.Record, ack map[string]bool) []drift {
	var drifts []drift

	mid, err := decimal.NewFromString(rec.Reference.Mid)
	if err != nil || !mid.IsPositive() {
		// Without a usable reference mid the verdicts cannot be re-derived at
		// all; the structural classification still can be.
		drifts = append(drifts, drift{
			Corridor: rec.Corridor, Seq: rec.Seq, Field: "reference.mid",
			Stored: rec.Reference.Mid, Derived: "unusable",
			Detail: "verdicts and recommendation cannot be re-derived without a positive reference mid",
		})
		return drifts
	}

	// Integrity from the stored paths: a single priced rung whose path avoids
	// fiat intermediaries proves an independent market exists.
	derivedIntegrity, err := classifyFromStoredPaths(rec.Rungs)
	if err == nil && derivedIntegrity.String() != rec.Integrity {
		drifts = append(drifts, drift{
			Corridor: rec.Corridor, Seq: rec.Seq, Field: "integrity",
			Stored: rec.Integrity, Derived: derivedIntegrity.String(),
			Detail: fmt.Sprintf("depends_on=%s", strings.Join(rec.DependsOn, ",")),
		})
	}

	// Recommendation: the first acceptable quote on the ascending ladder.
	derivedRecommended, _, err := rederiveBestRung(rec.Rungs, mid)
	if err == nil {
		if drift := compareRecommendation(rec.Recommended, derivedRecommended); drift != nil {
			drift.Corridor = rec.Corridor
			drift.Seq = rec.Seq
			drift.Field = "recommended"
			drifts = append(drifts, *drift)
		}
	}

	// Every priced rung: re-derive its loss and verdict from receive amount,
	// send amount and the reference mid.
	for _, rung := range rec.Rungs {
		if !rung.Priced {
			continue
		}
		drifts = append(drifts, rederiveRung(rec.Corridor, rec.Seq, rung, mid)...)
	}

	return drifts
}

// classifyFromStoredPaths re-derives corridor integrity from the path strings
// stored on each priced rung, without calling Horizon. It mirrors route.classify:
// a single path that avoids fiat intermediaries proves an independent market
// exists (DIRECT); priced paths that all traverse a fiat token make the
// corridor DERIVATIVE; no priced path at all is NO-MARKET.
func classifyFromStoredPaths(rungs []runstore.Rung) (route.Integrity, error) {
	direct := false
	priced := false
	for _, r := range rungs {
		if !r.Priced || r.Path == "" {
			continue
		}
		priced = true
		if !pathTraversesFiat(r.Path) {
			direct = true
			break
		}
	}
	if !priced {
		return route.IntegrityNoMarket, nil
	}
	if direct {
		return route.IntegrityDirect, nil
	}
	return route.IntegrityDerivative, nil
}

// pathTraversesFiat reports whether a stored path string routes through a
// registered fiat-pegged token between its endpoints. An unregistered hop is
// treated as a bridge — the known bounded false-negative in route.classify.
func pathTraversesFiat(path string) bool {
	parts := strings.Split(path, " -> ")
	if len(parts) < 3 {
		// No intermediate hops: a direct market, no fiat traversal.
		return false
	}
	for _, code := range parts[1 : len(parts)-1] {
		a, ok := asset.Lookup(code)
		if !ok {
			continue // unknown hop: treated as a bridge, not a fiat dependency
		}
		if asset.IsFiatToken(a) {
			return true
		}
	}
	return false
}

// rederiveBestRung finds the rung the engine would recommend: the first
// acceptable quote on the ascending (smallest-size-first) ladder. Larger
// sizes on a thin corridor only get worse, so the smallest acceptable size is
// the honest one to surface (route/ladder.go). It returns the rung and its
// verdict, or nil when nothing is recommended.
func rederiveBestRung(rungs []runstore.Rung, mid decimal.Decimal) (*runstore.Rung, route.Verdict, error) {
	for _, r := range rungs {
		if !r.Priced {
			continue
		}
		verdict, _, err := rungVerdict(r, mid)
		if err != nil {
			continue
		}
		if verdict.Acceptable() {
			return &r, verdict, nil
		}
	}
	return nil, route.VerdictUnknown, nil
}

// rungVerdict re-derives one priced rung's loss percentage and verdict from
// its stored send/receive amounts against the reference mid, exactly as
// route.Quote.score does.
func rungVerdict(r runstore.Rung, mid decimal.Decimal) (route.Verdict, decimal.Decimal, error) {
	receive, err := decimal.NewFromString(r.ReceiveAmount)
	if err != nil {
		return route.VerdictUnknown, decimal.Zero, err
	}
	send, err := decimal.NewFromString(r.SendAmount)
	if err != nil {
		return route.VerdictUnknown, decimal.Zero, err
	}
	if send.IsZero() {
		return route.VerdictUnknown, decimal.Zero, fmt.Errorf("send amount is zero")
	}
	effectiveRate := receive.Div(send)
	loss := mid.Sub(effectiveRate).Div(mid).Mul(decimal.NewFromInt(100))
	if loss.IsNegative() {
		loss = decimal.Zero
	}
	return verdictForCurrent(loss), loss, nil
}

func rederiveRung(corridor string, seq int64, rung runstore.Rung, mid decimal.Decimal) []drift {
	var drifts []drift
	label := "rung[" + rung.SendAmount + "]"

	verdict, loss, err := rungVerdict(rung, mid)
	if err != nil {
		drifts = append(drifts, drift{
			Corridor: corridor, Seq: seq, Field: label + ".verdict",
			Stored: rung.Verdict, Derived: "unusable",
			Detail: "could not re-derive: " + err.Error(),
		})
		return drifts
	}

	if strings.TrimSpace(rung.Verdict) != "" && rung.Verdict != verdict.String() {
		drifts = append(drifts, drift{
			Corridor: corridor, Seq: seq, Field: label + ".verdict",
			Stored: rung.Verdict, Derived: verdict.String(),
			Detail: fmt.Sprintf("loss_pct stored=%s derived=%s", rung.LossPct, loss.StringFixed(4)),
		})
	}

	return drifts
}

// compareRecommendation reports a drift between the stored recommended rung
// and the re-derived one, or nil when they agree.
func compareRecommendation(stored *runstore.Rung, derived *runstore.Rung) *drift {
	switch {
	case stored == nil && derived == nil:
		return nil
	case stored == nil:
		return &drift{Stored: "(none)", Derived: derived.SendAmount, Detail: "engine recommends size " + derived.SendAmount}
	case derived == nil:
		return &drift{Stored: stored.SendAmount, Derived: "(none)", Detail: "engine recommends nothing"}
	}

	if stored.SendAmount != derived.SendAmount {
		return &drift{
			Stored:  stored.SendAmount,
			Derived: derived.SendAmount,
			Detail:  fmt.Sprintf("stored verdict=%s derived verdict=%s", stored.Verdict, derived.Verdict),
		}
	}
	if stored.Verdict != derived.Verdict {
		return &drift{
			Stored:  stored.SendAmount + " (" + stored.Verdict + ")",
			Derived: derived.SendAmount + " (" + derived.Verdict + ")",
			Detail:  "verdict changed at the same size",
		}
	}
	return nil
}
