package refrate

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// unwrap resolves Checked → Cached → the concrete provider, which is the shape
// DefaultStack builds: freshness outside, caching inside.
func unwrap(t *testing.T, p Provider) Provider {
	t.Helper()
	checked, ok := p.(*Checked)
	if !ok {
		t.Fatalf("%T is not *Checked; the freshness bound must wrap the cache", p)
	}
	cached, ok := checked.Inner.(*Cached)
	if !ok {
		t.Fatalf("Checked wraps %T, want *Cached", checked.Inner)
	}
	return cached.Inner
}

// TestDefaultStackIsTheSharedBenchmark pins the composition both binaries now
// build: exchangerate-api cross-checked against currency-api, with floatrates
// as the diagnostic, each cached inside the freshness bound. The concrete
// types matter because the diagnostic is what makes a DISAGREE attributable,
// and the cache is what keeps a ladder from spending one free-tier request per
// rung.
func TestDefaultStackIsTheSharedBenchmark(t *testing.T) {
	c := DefaultStack(StackConfig{MaxAge: DefaultMaxAge})

	if c == nil {
		t.Fatal("DefaultStack returned nil")
	}
	if c.Primary == nil || c.Secondary == nil || c.Diagnostic == nil {
		t.Fatalf("stack is incomplete: primary=%v secondary=%v diagnostic=%v",
			c.Primary, c.Secondary, c.Diagnostic)
	}
	if got, want := c.Name(), "exchangerate-api+currency-api"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if got, want := c.Diagnostic.Name(), "floatrates"; got != want {
		t.Errorf("Diagnostic.Name() = %q, want %q", got, want)
	}

	for name, p := range map[string]Provider{
		"primary": c.Primary, "secondary": c.Secondary, "diagnostic": c.Diagnostic,
	} {
		checked, ok := p.(*Checked)
		if !ok {
			t.Fatalf("%s is %T, want *Checked", name, p)
		}
		if checked.MaxAge != DefaultMaxAge {
			t.Errorf("%s MaxAge = %s, want %s", name, checked.MaxAge, DefaultMaxAge)
		}
		switch inner := unwrap(t, p).(type) {
		case *ExchangeRateAPI:
			if name != "primary" {
				t.Errorf("%s wraps exchangerate-api; only the primary should", name)
			}
		case *CurrencyAPI:
			if name != "secondary" {
				t.Errorf("%s wraps currency-api; only the secondary should", name)
			}
		case *FloatRates:
			if name != "diagnostic" {
				t.Errorf("%s wraps floatrates; only the diagnostic should", name)
			}
		default:
			t.Errorf("%s wraps %T, which is not part of the documented stack", name, inner)
		}
	}
}

// TestDefaultStackSharesOneClient is the recording property: a snapshot is
// captured by putting one recorder on one client, so every provider must use
// that client. A provider left with its default client would bypass the
// recorder and make the snapshot incomplete, which only shows up as a replay
// miss much later.
func TestDefaultStackSharesOneClient(t *testing.T) {
	client := &http.Client{}
	c := DefaultStack(StackConfig{Client: client})

	switch p := unwrap(t, c.Primary).(type) {
	case *ExchangeRateAPI:
		if p.Client != client {
			t.Error("primary did not receive the shared client")
		}
	}
	switch p := unwrap(t, c.Secondary).(type) {
	case *CurrencyAPI:
		if p.Client != client {
			t.Error("secondary did not receive the shared client")
		}
	}
	switch p := unwrap(t, c.Diagnostic).(type) {
	case *FloatRates:
		if p.Client != client {
			t.Error("diagnostic did not receive the shared client")
		}
	}
}

// TestDefaultStackPerProviderClients keeps production's per-upstream metrics
// transports possible without a second composition: an explicit provider
// client wins, and the shared client covers the rest.
func TestDefaultStackPerProviderClients(t *testing.T) {
	shared, primary := &http.Client{}, &http.Client{}
	c := DefaultStack(StackConfig{Client: shared, PrimaryClient: primary})

	if p := unwrap(t, c.Primary).(*ExchangeRateAPI); p.Client != primary {
		t.Error("primary did not use its explicit client")
	}
	if p := unwrap(t, c.Secondary).(*CurrencyAPI); p.Client != shared {
		t.Error("secondary did not fall back to the shared client")
	}
	if p := unwrap(t, c.Diagnostic).(*FloatRates); p.Client != shared {
		t.Error("diagnostic did not fall back to the shared client")
	}
}

// TestCrossWithMaxAgeRejectsStaleSources is the freshness policy itself: the
// bound is applied outside the cache, so a cached reply still has to be fresh
// on every use.
func TestCrossWithMaxAgeRejectsStaleSources(t *testing.T) {
	stale := NewStatic(map[string]decimal.Decimal{"USD/NGN": decimal.NewFromInt(1350)})
	stale.Stamp = time.Now().Add(-DefaultMaxAge - time.Hour)

	cross := CrossWithMaxAge(stale, nil, nil, DefaultMaxAge)
	if _, err := cross.Rate(context.Background(), "USD", "NGN"); err == nil {
		t.Fatal("a rate older than MaxAge was accepted")
	}

	fresh := NewStatic(map[string]decimal.Decimal{"USD/NGN": decimal.NewFromInt(1350)})
	cross = CrossWithMaxAge(fresh, nil, nil, DefaultMaxAge)
	if _, err := cross.Rate(context.Background(), "USD", "NGN"); err != nil {
		t.Fatalf("a fresh rate was rejected: %v", err)
	}
}

// TestBothBinariesBuildTheStackHere is the acceptance criterion "one
// constructor builds the reference stack for both binaries", checked where it
// could drift: the two main packages. Neither may construct a Cross of its own
// — a second literal is exactly how cmd/ladder ended up scoring against one
// unchecked provider while the server cross-checked two (issue #869).
func TestBothBinariesBuildTheStackHere(t *testing.T) {
	for _, path := range []string{
		filepath.Join("..", "cmd", "wayfared", "main.go"),
		filepath.Join("..", "cmd", "ladder", "main.go"),
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		src := string(raw)
		if !strings.Contains(src, "refrate.DefaultStack(") {
			t.Errorf("%s does not build its reference benchmark through refrate.DefaultStack", path)
		}
		if strings.Contains(src, "refrate.Cross{") {
			t.Errorf("%s constructs a refrate.Cross directly; the composition belongs in DefaultStack", path)
		}
	}
}
