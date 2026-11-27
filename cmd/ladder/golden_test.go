package main

// Golden test for the `ladder -json` document (issue #668).
//
// Scripts consume this output, and the other -json tests only compare it with
// route.ToCorridorJSON (same bytes) or check selected fields. Neither notices a
// renamed key, a value that changes type, a decimal that gains or loses
// places, or a block that quietly disappears from both producers at once. This
// test does: it replays a committed snapshot through the real engine, renders
// the document exactly as -json does, and compares every byte with a file on
// disk.
//
// The clock is pinned, so measured_at is frozen in the golden file rather than
// pattern-matched away — a golden file that skipped the timestamp would not
// notice it being dropped or changing format. Everything else in the document
// comes from the recorded bytes, so the test runs offline.

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

var updateLadderGolden = flag.Bool("update-ladder-golden", false,
	"rewrite the cmd/ladder -json golden files from current behaviour")

// goldenClock is the instant the golden documents are stamped with. It is
// deliberately not the snapshot's recording time, so a regression that
// started reading measured_at from a recorded header would show up as a diff.
var goldenClock = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// goldenSnapshot is the recorded USDC -> NGNC ladder: the default corridor,
// priced at every rung of the shared ladder, benchmarked via currency-api.
const goldenSnapshot = "usdc-ngnc-20260821T223040Z"

// replayGoldenLadder measures the recorded corridor through the same engine
// wiring main() builds, with every upstream answered from the snapshot.
func replayGoldenLadder(t *testing.T) *route.LadderResult {
	t.Helper()
	m, err := snapshot.Load(filepath.Join("..", "..", "testdata", "snapshots", goldenSnapshot))
	if err != nil {
		t.Fatalf("loading snapshot %s: %v", goldenSnapshot, err)
	}
	client := &dex.Client{HorizonURL: dex.DefaultHorizonURL, HTTPClient: m.HTTPClient()}
	ref := &refrate.CurrencyAPI{Client: m.HTTPClient()}
	eng := &route.Engine{DEX: client, RefRate: &refrate.Checked{Inner: ref}, Ledgers: client}

	sizes, err := parseSizes(strings.Join(m.Sizes, ","))
	if err != nil {
		t.Fatalf("parsing recorded sizes: %v", err)
	}
	result, err := eng.Ladder(context.Background(), route.LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   asset.NGNC(),
		Sizes:          sizes,
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}
	// A replay with a hole would pin an error string instead of the shape;
	// fail loudly so a snapshot that stopped matching is not mistaken for a
	// corridor that changed.
	if failed := erroredRungs(result); len(failed) > 0 {
		t.Fatalf("replay errored at sizes %v; the snapshot no longer answers every request", failed)
	}
	// Per-rung findings stamp the wall clock as they are derived; pin them as
	// goldenFindings pins the corridor-level block.
	at := goldenClock.Add(-90 * time.Second)
	for i := range result.Rungs {
		f := result.Rungs[i].Findings
		if f == nil {
			continue
		}
		for j := range f.Checks {
			f.Checks[j].At = at
			for k := range f.Checks[j].Evidence {
				f.Checks[j].Evidence[k].ObservedAt = at
			}
		}
	}
	return result
}

// goldenFindings is a findings block with a fixed observation time, spanning
// a pass, a fail, an undetermined check and a determined metric, so the
// golden file pins every shape the block can carry. Checks are not replayed
// from the snapshot — it holds no check traffic — so the block is attached
// the same way main() attaches live findings: through route.WithFindings.
func goldenFindings() *checks.Findings {
	at := goldenClock.Add(-90 * time.Second)
	f := &checks.Findings{}
	f.Add(checks.Pass(
		checks.AnchorAssetISO4217{}.Describe(),
		checks.Subject{Domain: "ngnc.online", Asset: asset.NGNC()},
		"anchor_asset names the naira",
		checks.Evidence{Source: "https://ngnc.online/.well-known/stellar.toml",
			Observed: "NGN", ObservedAt: at}))
	f.Add(checks.Fail(
		checks.IssuerAuthFlags{}.Describe(),
		checks.Subject{Asset: asset.NGNC()},
		"issuer can revoke trustlines",
		checks.Evidence{Source: "https://horizon.stellar.org/accounts/" + asset.NGNC().Issuer,
			Observed: "auth_revocable=true", ObservedAt: at}))
	f.Add(checks.Undetermined(
		checks.SEP10EndpointResponds{}.Describe(),
		checks.Subject{Domain: "ngnc.online", Asset: asset.NGNC()},
		"WEB_AUTH_ENDPOINT did not answer"))
	// The result constructors stamp the wall clock; pin it, as a live run's
	// results would carry the instant each check actually ran.
	for i := range f.Checks {
		f.Checks[i].At = at
	}
	f.AddMetric(checks.MetricResult{
		Observation: checks.Observation{
			ID: "spread.bid-ask", Scope: checks.ScopeAsset, Subject: "NGNC",
			At: at, Determined: true,
			Evidence: []checks.Evidence{{Source: "https://horizon.stellar.org/order_book",
				Observed: "0.0004", ObservedAt: at}},
		},
		Value: decimal.RequireFromString("0.0004"), Unit: checks.UnitRatio,
		Summary: "bid-ask spread on the USDC/NGNC book",
	})
	return f
}

// TestLadderJSONGolden pins the full `ladder -json` document for a replayed
// corridor, with checks skipped (-checks=false) and with a findings block.
func TestLadderJSONGolden(t *testing.T) {
	prev := now
	now = func() time.Time { return goldenClock }
	t.Cleanup(func() { now = prev })

	result := replayGoldenLadder(t)

	for _, tc := range []struct {
		name     string
		file     string
		findings *checks.Findings
	}{
		{"checks skipped", "ladder-usdc-ngnc.json", nil},
		{"with findings", "ladder-usdc-ngnc-findings.json", goldenFindings()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got bytes.Buffer
			if err := encodeCorridorJSON(&got, result, "USD/NGN", tc.findings); err != nil {
				t.Fatalf("encodeCorridorJSON: %v", err)
			}
			if !json.Valid(got.Bytes()) {
				t.Fatalf("-json output is not valid JSON:\n%s", got.Bytes())
			}

			path := filepath.Join("testdata", "golden", tc.file)
			if *updateLadderGolden {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("wrote %s", path)
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading golden file: %v\nRun: go test ./cmd/ladder -run TestLadderJSONGolden -update-ladder-golden", err)
			}
			if got.String() != string(want) {
				t.Errorf("ladder -json output does not match %s.\n\n"+
					"This file pins the CLI's JSON shape, which scripts consume. Confirm the "+
					"change is intended before regenerating with:\n"+
					"  go test ./cmd/ladder -run TestLadderJSONGolden -update-ladder-golden\n\n"+
					"first difference: %s", path, firstDiff(got.String(), string(want)))
			}
		})
	}
}

// firstDiff reports the first line at which two documents disagree, which is
// more useful in a CI log than two thousand-line dumps.
func firstDiff(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(g) || i < len(w); i++ {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return "line " + strconv.Itoa(i+1) + ":\n   got: " + gl + "\n  want: " + wl
		}
	}
	return "(none)"
}
