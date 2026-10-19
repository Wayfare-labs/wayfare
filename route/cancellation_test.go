package route

import (
	"context"
	"testing"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/shopspring/decimal"
)

func TestQuoteHonoursCancelledContextBeforeUpstreams(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := &Engine{RefRate: testRateProvider{}}
	_, err := e.Quote(ctx, Request{
		SendAsset: asset.USDC(), ReceiveAsset: asset.NGNC(),
		SendAmount: decimal.NewFromInt(1), ReferenceBase: "USD", ReferenceQuote: "NGN",
	})
	if err != context.Canceled {
		t.Fatalf("Quote error = %v, want context.Canceled", err)
	}
}

type testRateProvider struct{}

func (testRateProvider) Name() string { return "test" }
func (testRateProvider) Rate(context.Context, string, string) (refrate.Rate, error) {
	return refrate.Rate{Mid: decimal.NewFromInt(1), Source: "test"}, nil
}
