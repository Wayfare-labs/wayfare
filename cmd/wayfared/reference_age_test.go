package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/refrate"
)

type namedStaticReference struct {
	name string
	data *refrate.Static
}

func (p namedStaticReference) Name() string { return p.name }

func (p namedStaticReference) Rate(ctx context.Context, base, quote string) (refrate.Rate, error) {
	rate, err := p.data.Rate(ctx, base, quote)
	rate.Source = p.name
	return rate, err
}

func staticReference(name string, age time.Duration) refrate.Provider {
	p := refrate.NewStatic(map[string]decimal.Decimal{"USD/NGN": decimal.NewFromInt(1350)})
	p.Stamp = time.Now().Add(-age)
	return namedStaticReference{name: name, data: p}
}

func TestWayfaredReferenceCrossRejectsStaleSources(t *testing.T) {
	maxAge := refrate.DefaultMaxAge
	t.Run("one stale source degrades to single", func(t *testing.T) {
		cross := refrate.CrossWithMaxAge(
			staticReference("old-primary", maxAge+time.Hour),
			staticReference("fresh-secondary", time.Hour),
			nil,
			maxAge,
		)
		rate, err := cross.Rate(context.Background(), "USD", "NGN")
		if err != nil {
			t.Fatalf("Cross.Rate: %v", err)
		}
		if rate.Agreement != refrate.AgreementSingle {
			t.Errorf("Agreement = %s, want SINGLE", rate.Agreement)
		}
		if !strings.Contains(rate.Note, "old-primary") {
			t.Errorf("Note = %q, want it to name the stale provider", rate.Note)
		}
	})

	t.Run("two stale sources return typed failure", func(t *testing.T) {
		cross := refrate.CrossWithMaxAge(
			staticReference("old-primary", maxAge+time.Hour),
			staticReference("old-secondary", maxAge+time.Hour),
			nil,
			maxAge,
		)
		_, err := cross.Rate(context.Background(), "USD", "NGN")
		var stale *refrate.ErrStaleRate
		if !errors.As(err, &stale) {
			t.Fatalf("Cross.Rate error = %v, want typed stale-rate error", err)
		}
	})
}
