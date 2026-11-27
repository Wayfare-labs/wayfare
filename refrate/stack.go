package refrate

import (
	"log/slog"
	"net/http"
	"time"
)

// StackConfig describes how the shared reference stack is wired.
//
// The zero value is a usable stack: each provider gets its own default client,
// no freshness bound is applied, and logging falls back to slog.Default.
type StackConfig struct {
	// Client is used by every provider that does not set its own. A snapshot
	// recorder relies on this: one recorder on one client captures every
	// reference request the stack makes, so the snapshot replays offline with
	// the same agreement verdict.
	Client *http.Client

	// PrimaryClient, SecondaryClient and DiagnosticClient override Client for
	// one provider. Production wires per-upstream metrics transports here; a
	// provider left without one of these shares Client.
	PrimaryClient    *http.Client
	SecondaryClient  *http.Client
	DiagnosticClient *http.Client

	// Logger is handed to every provider. Nil means slog.Default.
	Logger *slog.Logger

	// MaxAge is the absolute freshness bound applied outside each cache, so a
	// cached reply is rechecked on every use and one stale source degrades
	// through Cross rather than becoming an apparently fresh hit. Zero
	// disables the bound (Checked passes every rate through).
	MaxAge time.Duration
}

// DefaultStack builds the reference stack the project measures against:
// exchangerate-api as the primary, currency-api as the cross-check, and
// floatrates as the diagnostic, each cached and each behind the freshness
// policy in StackConfig.MaxAge.
//
// # Why one constructor
//
// The benchmark policy is part of the measurement (ADR 001). A figure
// reproduced with cmd/ladder must be scored the same way the server would
// score it, or "reproduce it with the CLI" reproduces a weaker claim: the CLI
// could score a corridor the server would refuse to score. Before issue #869
// the composition lived in cmd/wayfared and cmd/ladder separately, and the two
// had drifted — the CLI emitted reference_agreement SINGLE unconditionally
// because it never had a second provider. Both binaries now build their
// benchmark here, so the composition cannot drift again without changing this
// one function.
func DefaultStack(cfg StackConfig) *Cross {
	client := func(own *http.Client) *http.Client {
		if own != nil {
			return own
		}
		return cfg.Client
	}

	return CrossWithMaxAge(
		&Cached{Inner: &ExchangeRateAPI{Client: client(cfg.PrimaryClient), Logger: cfg.Logger}},
		&Cached{Inner: &CurrencyAPI{Client: client(cfg.SecondaryClient), Logger: cfg.Logger}},
		// The third provider is consulted only when the first two disagree,
		// and only to attribute which of them is the outlier. It never
		// contributes a scored mid (see Cross).
		&Cached{Inner: &FloatRates{Client: client(cfg.DiagnosticClient), Logger: cfg.Logger}},
		cfg.MaxAge,
	)
}

// CrossWithMaxAge applies an absolute freshness bound outside each provider,
// which is deliberately outside its cache: a cache hit is rechecked on every
// use, so a stale source degrades through Cross (usually to SINGLE with a note
// naming it) instead of being served as an apparently fresh answer.
//
// Nil providers are passed through as nil, so a stack without a diagnostic is
// the same shape the composition always had.
func CrossWithMaxAge(primary, secondary, diagnostic Provider, maxAge time.Duration) *Cross {
	checked := func(provider Provider) Provider {
		if provider == nil {
			return nil
		}
		return &Checked{Inner: provider, MaxAge: maxAge}
	}
	return &Cross{
		Primary:    checked(primary),
		Secondary:  checked(secondary),
		Diagnostic: checked(diagnostic),
	}
}
