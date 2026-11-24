package checks

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

func TestFloorLossMetricFromNGNCDustSnapshot(t *testing.T) {
	manifest, err := snapshot.Load(filepath.Join(
		"..", "testdata", "snapshots", "usdc-ngnc-20260821T223040Z"))
	if err != nil {
		t.Fatalf("loading NGNC dust snapshot: %v", err)
	}
	client := &dex.Client{
		HorizonURL: "https://horizon.stellar.org",
		HTTPClient: manifest.HTTPClient(),
	}
	path, err := client.BestPath(
		context.Background(), asset.USDC(), decimal.RequireFromString("0.1"), asset.NGNC())
	if err != nil {
		t.Fatalf("reading the 0.1 USDC snapshot rung: %v", err)
	}
	if path == nil {
		t.Fatal("snapshot returned no best path for the 0.1 USDC rung")
	}
	if got := path.DestAmount.String(); got != "96.9351231" {
		t.Fatalf("snapshot destination amount = %s, want 96.9351231", got)
	}

	mid := decimal.RequireFromString("1349.669672")
	metric := FloorLossMetric{
		Size:          decimal.RequireFromString("0.1"),
		EffectiveRate: path.Rate(),
		Reference: refrate.Rate{
			Base: "USD", Quote: "NGN", Mid: mid, Source: "snapshot-reference",
			AsOf: time.Date(2026, 8, 22, 0, 2, 31, 0, time.UTC),
		},
	}
	result := RunMetric(context.Background(), metric, Subject{
		Send: asset.USDC(), Receive: asset.NGNC(),
	})

	want := mid.Sub(path.Rate()).Div(mid).Mul(decimal.NewFromInt(100))
	if !result.Determined {
		t.Fatalf("floor loss was undetermined: %s", result.Reason)
	}
	if !result.Value.Equal(want) {
		t.Errorf("floor loss = %s, want %s", result.Value, want)
	}
	if len(result.Evidence) != 2 {
		t.Fatalf("evidence has %d items, want rung and reference evidence", len(result.Evidence))
	}
	if !strings.Contains(result.Evidence[0].Observed, "size=0.1") ||
		!strings.Contains(result.Evidence[0].Observed, "effective_rate=969.351231") {
		t.Errorf("rung evidence = %q, missing measured size or rate", result.Evidence[0].Observed)
	}
	if !strings.Contains(result.Evidence[1].Source, "snapshot-reference") ||
		!strings.Contains(result.Evidence[1].Observed, "mid=1349.669672") {
		t.Errorf("reference evidence = %+v, missing provider or mid", result.Evidence[1])
	}
}

func TestFloorLossMetricRequiresScorableReference(t *testing.T) {
	metric := FloorLossMetric{
		Size:          decimal.RequireFromString("0.1"),
		EffectiveRate: decimal.RequireFromString("100"),
		Reference: refrate.Rate{
			Base: "USD", Quote: "NGN", Mid: decimal.RequireFromString("1350"),
			Source: "broken-reference", Agreement: refrate.AgreementMalfunction,
			Note: "providers disagree",
		},
	}
	result := RunMetric(context.Background(), metric, Subject{
		Send: asset.USDC(), Receive: asset.NGNC(),
	})
	if result.Determined {
		t.Fatal("unscorable reference produced a determined floor loss")
	}
	if !strings.Contains(result.Reason, "providers disagree") {
		t.Errorf("reason = %q, want the reference's unscorable note", result.Reason)
	}
}

func TestFloorLossMetricDoesNotInventMissingRungOrReference(t *testing.T) {
	validReference := refrate.Rate{
		Base: "USD", Quote: "NGN", Mid: decimal.NewFromInt(1350), Source: "provider",
	}
	for _, tc := range []struct {
		name   string
		metric FloorLossMetric
	}{
		{
			name:   "missing rung",
			metric: FloorLossMetric{Reference: validReference},
		},
		{
			name: "missing reference mid",
			metric: FloorLossMetric{
				Size:          decimal.RequireFromString("0.1"),
				EffectiveRate: decimal.NewFromInt(100),
				Reference:     refrate.Rate{Source: "provider"},
			},
		},
		{
			name: "missing reference pair",
			metric: FloorLossMetric{
				Size:          decimal.RequireFromString("0.1"),
				EffectiveRate: decimal.NewFromInt(100),
				Reference: refrate.Rate{
					Mid: decimal.NewFromInt(1350), Source: "provider",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := RunMetric(context.Background(), tc.metric, Subject{
				Send: asset.USDC(), Receive: asset.NGNC(),
			})
			if result.Determined {
				t.Fatal("missing measurement input produced a determined floor loss")
			}
		})
	}
}
