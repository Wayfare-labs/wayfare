package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/refrate"
)

func TestLadderReferenceProviderEnforcesMaximumAge(t *testing.T) {
	provider := refrate.NewStatic(map[string]decimal.Decimal{"USD/NGN": decimal.NewFromInt(1350)})
	provider.Stamp = time.Now().Add(-refrate.DefaultMaxAge - time.Hour)

	_, err := checkedReferenceProvider(provider, refrate.DefaultMaxAge).Rate(context.Background(), "USD", "NGN")
	var stale *refrate.ErrStaleRate
	if !errors.As(err, &stale) {
		t.Fatalf("reference error = %v, want the typed stale-rate error", err)
	}
}
