package route

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
)

type changingReference struct {
	calls atomic.Int32
}

func (p *changingReference) Name() string { return "changing-reference" }

func (p *changingReference) Rate(_ context.Context, base, quote string) (refrate.Rate, error) {
	call := p.calls.Add(1)
	return refrate.Rate{
		Base: base, Quote: quote,
		Mid:    decimal.NewFromInt(int64(100 + call)),
		Source: p.Name(),
	}, nil
}

type failingReference struct {
	calls atomic.Int32
}

func (p *failingReference) Name() string { return "failed-reference" }

func (p *failingReference) Rate(context.Context, string, string) (refrate.Rate, error) {
	p.calls.Add(1)
	return refrate.Rate{}, errors.New("provider unavailable")
}

type countingParallel struct {
	calls atomic.Int32
}

func (p *countingParallel) Name() string { return "parallel" }

func (p *countingParallel) Rate(_ context.Context, base, quote string) (refrate.Rate, error) {
	p.calls.Add(1)
	return refrate.Rate{
		Base: base, Quote: quote,
		Mid: decimal.NewFromInt(120), Source: p.Name(),
	}, nil
}

type countingPathfinder struct {
	calls atomic.Int32
}

func (p *countingPathfinder) StrictSendPaths(context.Context, asset.Asset, decimal.Decimal, asset.Asset) ([]dex.Path, error) {
	p.calls.Add(1)
	return nil, nil
}

func (p *countingPathfinder) MeasureSlippage(context.Context, asset.Asset, decimal.Decimal, asset.Asset, decimal.Decimal) (*dex.Slippage, error) {
	p.calls.Add(1)
	return nil, nil
}

func TestLadderUsesOneReferenceSnapshotForEveryRung(t *testing.T) {
	reference := &changingReference{}
	parallel := &countingParallel{}
	engine := &Engine{RefRate: reference, Parallel: parallel}

	ladder, err := engine.Ladder(context.Background(), LadderRequest{
		SendAsset: asset.USDC(), ReceiveAsset: asset.NGNC(),
		Sizes:         []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(10), decimal.NewFromInt(100)},
		ReferenceBase: "USD", ReferenceQuote: "NGN",
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}
	if got := reference.calls.Load(); got != 1 {
		t.Errorf("reference provider calls = %d, want exactly one per ladder", got)
	}
	if got := parallel.calls.Load(); got != 1 {
		t.Errorf("parallel provider calls = %d, want exactly one per ladder", got)
	}
	if len(ladder.Rungs) != 3 {
		t.Fatalf("rungs = %d, want 3", len(ladder.Rungs))
	}
	for i, rung := range ladder.Rungs {
		if rung.Result == nil {
			t.Fatalf("rung %d has no result", i)
		}
		if !rung.Result.ReferenceMid.Equal(ladder.ReferenceMid) {
			t.Errorf("rung %d reference mid = %s, ladder reference mid = %s", i, rung.Result.ReferenceMid, ladder.ReferenceMid)
		}
		if rung.Result.Parallel == nil || !rung.Result.Parallel.Mid.Equal(decimal.NewFromInt(120)) {
			t.Errorf("rung %d parallel snapshot = %+v, want the one 120 quote", i, rung.Result.Parallel)
		}
	}
}

func TestLadderReferenceFailurePrecedesPathfinding(t *testing.T) {
	reference := &failingReference{}
	pathfinder := &countingPathfinder{}
	engine := &Engine{RefRate: reference, DEX: pathfinder}

	ladder, err := engine.Ladder(context.Background(), LadderRequest{
		SendAsset: asset.USDC(), ReceiveAsset: asset.NGNC(),
		Sizes:         []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(10)},
		ReferenceBase: "USD", ReferenceQuote: "NGN",
	})
	if err != nil {
		t.Fatalf("Ladder returned an operation error: %v", err)
	}
	if got := reference.calls.Load(); got != 1 {
		t.Errorf("reference provider calls = %d, want one", got)
	}
	if got := pathfinder.calls.Load(); got != 0 {
		t.Errorf("pathfinder calls = %d after the reference failed, want zero", got)
	}
	if !ladder.Failed() {
		t.Error("ladder should report all rungs unmeasured when the reference is unavailable")
	}
}

func TestCancelledLadderDoesNotFetchReference(t *testing.T) {
	reference := &changingReference{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ladder, err := (&Engine{RefRate: reference}).Ladder(ctx, LadderRequest{
		SendAsset: asset.USDC(), ReceiveAsset: asset.NGNC(),
		Sizes:         []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(10)},
		ReferenceBase: "USD", ReferenceQuote: "NGN",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Ladder error = %v, want context.Canceled", err)
	}
	if got := reference.calls.Load(); got != 0 {
		t.Errorf("reference provider calls = %d for a cancelled ladder, want zero", got)
	}
	if len(ladder.Rungs) != 2 || !ladder.Rungs[0].SendAmount.Equal(decimal.NewFromInt(1)) ||
		!ladder.Rungs[1].SendAmount.Equal(decimal.NewFromInt(10)) {
		t.Errorf("cancelled ladder rungs = %+v, want both requested sizes retained", ladder.Rungs)
	}
}

func TestInvalidOnlyLadderDoesNotFetchReference(t *testing.T) {
	reference := &changingReference{}
	ladder, err := (&Engine{RefRate: reference}).Ladder(context.Background(), LadderRequest{
		SendAsset: asset.USDC(), ReceiveAsset: asset.NGNC(),
		Sizes:         []decimal.Decimal{decimal.Zero, decimal.NewFromInt(-1)},
		ReferenceBase: "USD", ReferenceQuote: "NGN",
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}
	if got := reference.calls.Load(); got != 0 {
		t.Errorf("reference provider calls = %d for invalid sizes, want zero", got)
	}
	for i, rung := range ladder.Rungs {
		if rung.Err == nil {
			t.Errorf("rung %d has no invalid-size error", i)
		}
	}
}
