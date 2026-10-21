package route_test

// Ladder-engine benchmarks (issue #616).
//
// Request cost is bounded per client (#320), but nothing here recorded the
// CPU cost of pricing a ladder, so a change that doubled it would have shown
// up nowhere. These measure it against a recorded snapshot rather than live
// Horizon, for two reasons that are the same reason: the number has to be
// comparable across machines and across runs, and a benchmark that reaches
// the network measures Horizon's day, not this package's arithmetic.
// snapshot.Replayer answers only from recorded bytes and errors on anything
// it has no answer for, so an unrecorded request fails the benchmark instead
// of quietly making it live.
//
// Each case loads the snapshot once and times the pricing, so the cost of
// reading the fixtures off disk is excluded: what is being priced is one
// ladder, not one ladder plus its input.

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// usdcNGNC is the case-study corridor: a real 2026-08-21 mainnet capture with
// enough recorded path queries to answer a full twelve-rung ladder.
//
// It fails rather than skipping when the fixture is missing. A test may skip
// an absent snapshot because skipping is visible in the test output; a
// benchmark that skips publishes nothing and the absence reads as "nothing to
// report", which is how this package went unmeasured in the first place.
func usdcNGNC(b *testing.B) *snapshot.Manifest {
	b.Helper()
	matches, err := filepath.Glob(filepath.Join("../testdata/snapshots", "usdc-ngnc-*"))
	if err != nil || len(matches) == 0 {
		b.Fatal("no usdc-ngnc snapshot under ../testdata/snapshots; capture one with cmd/ladder -record")
	}
	m, err := snapshot.Load(matches[0])
	if err != nil {
		b.Fatalf("loading %s: %v", matches[0], err)
	}
	return m
}

// ngncLadder prices the USDC→NGNC corridor at the default sizes — four orders
// of magnitude, 0.1 to 5000 — which is the ladder a deployment serves and the
// monitor records, so this is the cost that actually repeats.
func ngncLadder(e *route.Engine) (*route.LadderResult, error) {
	return e.Ladder(context.Background(), route.LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   asset.NGNC(),
		Sizes:          route.DefaultSizes,
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
	})
}

// BenchmarkLadderPricedFromSnapshot is the headline number: one full ladder,
// twelve rungs, priced from recorded bytes with no network.
func BenchmarkLadderPricedFromSnapshot(b *testing.B) {
	m := usdcNGNC(b)
	e := engineOver(m, "USD/NGN", "1350.2568")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := ngncLadder(e)
		if err != nil {
			b.Fatalf("Ladder: %v", err)
		}
		if len(res.Rungs) != len(route.DefaultSizes) {
			b.Fatalf("got %d rungs, want %d", len(res.Rungs), len(route.DefaultSizes))
		}
	}
}

// BenchmarkLadderReplayOnly prices the fixture replay on its own: every
// Horizon interaction the snapshot recorded, fetched once through the same
// replayer the ladder uses. Subtracting this from
// BenchmarkLadderPricedFromSnapshot attributes a regression to the decimal
// arithmetic rather than to the replay path, which the headline number
// alone cannot tell you.
func BenchmarkLadderReplayOnly(b *testing.B) {
	m := usdcNGNC(b)
	client := m.HTTPClient()

	// Rebuild each iteration rather than reusing one request: http.Client
	// closes and consumes the request body, and every recorded interaction
	// here is a GET with none, so the cost being measured is the replayer's,
	// not a request object's.
	var urls []string
	for _, in := range m.Interactions {
		if in.Kind == snapshot.KindHorizon {
			urls = append(urls, in.URL)
		}
	}
	if len(urls) == 0 {
		b.Fatal("the snapshot records no horizon interactions to replay")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, u := range urls {
			req, err := http.NewRequest(http.MethodGet, u, nil)
			if err != nil {
				b.Fatalf("building %s: %v", u, err)
			}
			resp, err := client.Do(req)
			if err != nil {
				b.Fatalf("replaying %s: %v", u, err)
			}
			if _, err := io.Copy(io.Discard, resp.Body); err != nil {
				b.Fatalf("draining %s: %v", u, err)
			}
			resp.Body.Close()
		}
	}
}

// BenchmarkCorridorProjection times the wire projection on its own: the same
// twelve rungs converted to the published document, with the snapshot loaded
// once and no HTTP at all. A request that answers from history pays this
// cost, not the pricing cost, so the two are worth separating.
func BenchmarkCorridorProjection(b *testing.B) {
	m := usdcNGNC(b)
	e := engineOver(m, "USD/NGN", "1350.2568")

	res, err := ngncLadder(e)
	if err != nil {
		b.Fatalf("pricing the ladder to project: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc := route.ToCorridorJSON(res, "USD/NGN")
		if doc.ReferencePair != "USD/NGN" {
			b.Fatalf("projection lost the reference pair: %q", doc.ReferencePair)
		}
	}
}

// BenchmarkLadderSingleRung is the floor: one size, priced from the same
// recorded bytes. Dividing the twelve-rung figure by this says how much of
// the ladder's cost is per rung and how much is the wrapper (the parallel
// fan-out, the marginal-cost walk, the verdict aggregation).
func BenchmarkLadderSingleRung(b *testing.B) {
	m := usdcNGNC(b)
	e := engineOver(m, "USD/NGN", "1350.2568")

	one := []decimal.Decimal{decimal.NewFromInt(100)}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := e.Ladder(context.Background(), route.LadderRequest{
			SendAsset:      asset.USDC(),
			ReceiveAsset:   asset.NGNC(),
			Sizes:          one,
			ReferenceBase:  "USD",
			ReferenceQuote: "NGN",
		})
		if err != nil {
			b.Fatalf("Ladder: %v", err)
		}
		if len(res.Rungs) != 1 {
			b.Fatalf("got %d rungs, want 1", len(res.Rungs))
		}
	}
}
