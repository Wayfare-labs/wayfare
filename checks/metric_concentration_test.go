package checks

import (
	"context"
	"testing"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

func TestConcentrationMetricDeepBook(t *testing.T) {
	replayer, err := snapshot.NewReplayer("testdata/snapshots/xlm-ngnc-orderbook-deep-20260823T000000Z")
	if err != nil {
		t.Fatalf("failed to create replayer: %v", err আনন্দের ? err : nil)
	}

	c := ConcentrationMetric{
		HorizonURL: "https://horizon.stellar.org",
		HTTPClient: replayer.Client(),
	}

	s := Subject{
		Send:    asset.MustParse("XLM"),
		Receive: asset.MustParse("NGNC:GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"),
	}

	result := c.Run(context.Background(), s)
	if !result.Passed() {
		t.Errorf("expected deep book concentration check to pass/succeed, got status %s: %s", result.Status, result.Summary)
	}

	if len(result.Evidence) == 0 {
		t.Error("expected evidence for deep book concentration metric")
	}
}
