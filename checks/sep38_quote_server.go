package checks

import (
	"context"
	"strings"
	"time"
)

// SEP38QuoteServerPublished reports whether the anchor publishes
// ANCHOR_QUOTE_SERVER in its stellar.toml, which is the field that
// declares SEP-38 support.
//
// # Why this matters
//
// An anchor that publishes no ANCHOR_QUOTE_SERVER cannot be quoted
// programmatically. Its rate is visible only inside the hosted SEP-24
// flow, to a human, after authenticating. A corridor served only by
// non-priceable anchors cannot be compared, only guessed at — and
// reporting this as a fact lets a reader know they are looking at an
// unpriceable corridor rather than assuming a price exists somewhere.
//
// # What it can determine
//
// Whether the anchor's stellar.toml declares an ANCHOR_QUOTE_SERVER.
//
// # What it cannot determine
//
// Whether the declared server actually works, returns valid responses,
// or supports the specific asset pair. That is a live probe and
// belongs in a separate check.
//
// Costs nothing: it reads a stellar.toml that has already been fetched.
type SEP38QuoteServerPublished struct{}

// Describe implements Check.
func (SEP38QuoteServerPublished) Describe() Descriptor {
	return Descriptor{
		ID:       "sep38.quote-server-published",
		Scope:    ScopeAnchor,
		Cost:     CostFree,
		Severity: SeverityNotice,
		Title:    "Anchor publishes ANCHOR_QUOTE_SERVER for SEP-38 pricing",
		CanDetermine: "Whether the anchor's stellar.toml declares an ANCHOR_QUOTE_SERVER, " +
			"which is the field that enables programmatic pricing via SEP-38.",
		CannotDetermine: "Whether the declared server actually works, returns valid responses, " +
			"or supports the specific asset pair. That is a live probe belonging in a " +
			"separate check.",
	}
}

// Run implements Check.
func (SEP38QuoteServerPublished) Run(_ context.Context, s Subject) CheckResult {
	d := SEP38QuoteServerPublished{}.Describe()

	if s.Profile == nil {
		return Undetermined(d, s,
			"no stellar.toml has been resolved for this anchor, so no quote server was declared to check")
	}

	quoteServer := strings.TrimSpace(s.Profile.TOML.AnchorQuoteServer)
	tomlURL := "https://" + s.Profile.Domain + "/.well-known/stellar.toml"
	at := time.Now().UTC()

	if quoteServer != "" {
		return Pass(d, s,
			"the anchor publishes ANCHOR_QUOTE_SERVER at "+quoteServer,
			Evidence{
				Source:     tomlURL + " → ANCHOR_QUOTE_SERVER",
				Observed:   quoteServer,
				ObservedAt: at,
			},
		)
	}

	return Fail(d, s,
		"the anchor does not publish ANCHOR_QUOTE_SERVER in its stellar.toml, "+
			"so its rails cannot be priced programmatically via SEP-38; "+
			"the rate is visible only inside the hosted SEP-24 flow, to a human",
		Evidence{
			Source:     tomlURL + " → ANCHOR_QUOTE_SERVER",
			Observed:   "(field absent)",
			ObservedAt: at,
		},
	)
}
