package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/refrate"
)

// TestPlanReferenceDefaultsToTheServerStack pins issue #869's default: no
// -ref means the same cross-checked composition wayfared builds, not one
// unchecked provider. The concrete type matters — a *refrate.Cross is what
// produces a secondary mid, a divergence and a possible MALFUNCTION.
func TestPlanReferenceDefaultsToTheServerStack(t *testing.T) {
	plan, err := planReference("")
	if err != nil {
		t.Fatalf("planReference(\"\"): %v", err)
	}
	if plan.single {
		t.Error("the default must not be a single-provider plan")
	}
	if got, want := plan.source.Provider, "exchangerate-api"; got != want {
		t.Errorf("manifest reference provider = %q, want %q", got, want)
	}
	if plan.source.BaseURL != refrate.DefaultExchangeRateAPI {
		t.Errorf("manifest reference base URL = %q, want %q",
			plan.source.BaseURL, refrate.DefaultExchangeRateAPI)
	}
	for _, want := range []string{refrate.DefaultCurrencyAPI, refrate.DefaultFloatRates} {
		found := false
		for _, note := range plan.notes {
			if strings.Contains(note, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("manifest notes do not name %q; a reader could not tell what the stack called", want)
		}
	}

	provider, warning := plan.build(nil, refrate.DefaultMaxAge)
	if warning != "" {
		t.Errorf("default plan produced a warning: %q", warning)
	}
	cross, ok := provider.(*refrate.Cross)
	if !ok {
		t.Fatalf("default provider is %T, want *refrate.Cross", provider)
	}
	if cross.Primary == nil || cross.Secondary == nil || cross.Diagnostic == nil {
		t.Error("default stack is incomplete")
	}
	// The freshness bound is applied outside each cache, as the server does.
	for name, p := range map[string]refrate.Provider{
		"primary": cross.Primary, "secondary": cross.Secondary, "diagnostic": cross.Diagnostic,
	} {
		checked, ok := p.(*refrate.Checked)
		if !ok {
			t.Errorf("%s is %T, want *refrate.Checked wrapping its cache", name, p)
			continue
		}
		if checked.MaxAge != refrate.DefaultMaxAge {
			t.Errorf("%s MaxAge = %s, want %s", name, checked.MaxAge, refrate.DefaultMaxAge)
		}
	}
}

// TestPlanReferenceSharesTheRecordingClient is the -record half of the
// default: one client feeds all three providers, so one recorder on it
// captures every reference request and the snapshot replays offline without a
// gap.
func TestPlanReferenceSharesTheRecordingClient(t *testing.T) {
	plan, err := planReference("")
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{}
	provider, _ := plan.build(client, refrate.DefaultMaxAge)
	cross := provider.(*refrate.Cross)

	for name, p := range map[string]refrate.Provider{
		"primary": cross.Primary, "secondary": cross.Secondary, "diagnostic": cross.Diagnostic,
	} {
		checked, ok := p.(*refrate.Checked)
		if !ok {
			t.Fatalf("%s is %T, want *refrate.Checked", name, p)
		}
		cached, ok := checked.Inner.(*refrate.Cached)
		if !ok {
			t.Fatalf("%s wraps %T, want *refrate.Cached", name, checked.Inner)
		}
		var got *http.Client
		switch inner := cached.Inner.(type) {
		case *refrate.ExchangeRateAPI:
			got = inner.Client
		case *refrate.CurrencyAPI:
			got = inner.Client
		case *refrate.FloatRates:
			got = inner.Client
		}
		if got != client {
			t.Errorf("%s does not use the recording client; its responses would not be recorded", name)
		}
	}
}

// TestPlanReferenceOverrideIsSingleAndSaysSo covers the explicit -ref escape
// hatch: it still works, it wraps the provider in Checked as before, and it
// carries a warning that the figure is uncorroborated rather than silently
// presented as the server's benchmark.
func TestPlanReferenceOverrideIsSingleAndSaysSo(t *testing.T) {
	plan, err := planReference("  currency-api  ")
	if err != nil {
		t.Fatalf("planReference: %v", err)
	}
	if !plan.single {
		t.Fatal("-ref NAME must produce a single-provider plan")
	}
	if got, want := plan.source.Provider, "currency-api"; got != want {
		t.Errorf("manifest reference provider = %q, want %q", got, want)
	}
	if got, want := plan.source.BaseURL, refrate.DefaultCurrencyAPI; got != want {
		t.Errorf("manifest reference base URL = %q, want %q", got, want)
	}
	if len(plan.notes) != 1 || !strings.Contains(plan.notes[0], "uncorroborated") {
		t.Errorf("manifest notes = %v, want one note marking the override uncorroborated", plan.notes)
	}

	provider, warning := plan.build(nil, refrate.DefaultMaxAge)
	if _, ok := provider.(*refrate.Checked); !ok {
		t.Fatalf("override provider is %T, want *refrate.Checked", provider)
	}
	if provider.Name() != "currency-api" {
		t.Errorf("override provider Name() = %q, want currency-api", provider.Name())
	}
	for _, want := range []string{"-ref currency-api", "uncorroborated", "cross-checks"} {
		if !strings.Contains(warning, want) {
			t.Errorf("stderr warning %q does not mention %q", warning, want)
		}
	}
}

// TestPlanReferenceOverrideUnknownStillFails keeps the old contract: an
// unknown -ref is an error, never a silent fallback to the default stack.
func TestPlanReferenceOverrideUnknownStillFails(t *testing.T) {
	if _, err := planReference("bogus"); err == nil {
		t.Fatal("planReference(\"bogus\") returned no error")
	}
}

// TestPlanReferenceMetadataMatchesTheChosenProvider is a small guard against
// a manifest that names a provider the run did not score against, which would
// make the snapshot's provenance decorative.
func TestPlanReferenceMetadataMatchesTheChosenProvider(t *testing.T) {
	for _, name := range []string{"exchangerate-api", "currency-api"} {
		plan, err := planReference(name)
		if err != nil {
			t.Fatalf("planReference(%q): %v", name, err)
		}
		provider, _ := plan.build(nil, refrate.DefaultMaxAge)
		if plan.source.Provider != provider.Name() {
			t.Errorf("planReference(%q) records provider %q but scores against %q",
				name, plan.source.Provider, provider.Name())
		}
	}

	plan, err := planReference("")
	if err != nil {
		t.Fatal(err)
	}
	if plan.source.Provider != "exchangerate-api" {
		t.Errorf("default manifest provider = %q, want the primary it scores from on agreement",
			plan.source.Provider)
	}
}
