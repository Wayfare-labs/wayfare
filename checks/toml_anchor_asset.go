package checks

import (
	"context"
	"strings"
	"time"

	"github.com/Wayfare-labs/wayfare/anchor"
)

// AnchorAssetISO4217 checks that a token's declared anchor_asset names the
// currency it tracks, using the ISO-4217 code SEP-1 intends.
//
// # Why this matters
//
// anchor_asset is how a token says what it is worth. Every figure Wayfare
// publishes about a fiat-pegged token is scored against a rate for that
// currency, so a token declaring the wrong code is declaring the wrong
// benchmark — and a reader comparing the token to its peg would be comparing
// it to nothing.
//
// This is not hypothetical. Read from ngnc.online on 2026-08-08, KESC declares
// anchor_asset="KESC", naming its own token rather than the Kenyan shilling it
// tracks. NGNC and GHSC declare "NGN" and "GHS" correctly. One published
// document, both shapes.
//
// # What it can determine
//
// Whether the declared value is a plausible ISO-4217 alphabetic code — three
// letters — and whether it merely repeats the token's own asset code.
//
// # What it cannot determine
//
// Whether the code is the *right* currency. "GHS" on a token that actually
// tracks the naira parses exactly as well as the truth. Nor does it validate
// against the ISO register: a three-letter code that is not a real currency
// passes. Proving the peg is real is a different question, answerable only by
// observing what the anchor actually pays out.
//
// Costs nothing: it reads a stellar.toml that has already been fetched.
type AnchorAssetISO4217 struct{}

// Describe implements Check.
func (AnchorAssetISO4217) Describe() Descriptor {
	return Descriptor{
		ID:       "toml.anchor-asset-iso4217",
		Scope:    ScopeAsset,
		Cost:     CostFree,
		Severity: SeverityNotice,
		Title:    "Declared anchor_asset is an ISO-4217 currency code",
		CanDetermine: "Whether the token's declared anchor_asset is a three-letter " +
			"alphabetic code, and whether it merely repeats the token's own asset code.",
		CannotDetermine: "Whether the code names the currency the token actually tracks. " +
			"A wrong-but-well-formed code parses exactly as well as the right one, and " +
			"this does not validate against the ISO register — only the shape.",
	}
}

// Run implements Check.
func (c AnchorAssetISO4217) Run(_ context.Context, s Subject) CheckResult {
	d := c.Describe()

	if s.Profile == nil {
		return Undetermined(d, s,
			"no stellar.toml has been resolved for this asset, so nothing was declared to check")
	}
	if s.Asset.Code == "" {
		return Undetermined(d, s, "no asset was named in the subject")
	}

	entry, found := findCurrency(s.Profile, s.Asset.Code, s.Asset.Issuer)
	if !found {
		return Undetermined(d, s,
			"the resolved stellar.toml lists no [[CURRENCIES]] entry matching this code and issuer, "+
				"so the anchor declares nothing about it",
			Evidence{
				Source:     anchor.TOMLURL(s.Profile.Domain),
				Observed:   "no matching [[CURRENCIES]] entry",
				ObservedAt: time.Now().UTC(),
			})
	}

	declared := strings.TrimSpace(entry.AnchorAsset)
	ev := Evidence{
		Source:     anchor.TOMLURL(s.Profile.Domain) + " → [[CURRENCIES]] code=" + entry.Code + " anchor_asset",
		Observed:   quoteOrAbsent(declared),
		ObservedAt: time.Now().UTC(),
	}

	// Absent is undetermined, not failed. A token that declares no peg has
	// said nothing wrong; it has said nothing at all, and those differ.
	if declared == "" {
		return Undetermined(d, s,
			"the entry declares no anchor_asset, so the token states no currency to be measured against",
			ev)
	}

	// The specific defect worth naming: the token pointing at itself.
	if strings.EqualFold(declared, entry.Code) {
		return Fail(d, s,
			"anchor_asset is \""+declared+"\", which repeats the token's own code rather than "+
				"naming the currency it tracks",
			ev)
	}

	if !isISO4217Shaped(declared) {
		return Fail(d, s,
			"anchor_asset \""+declared+"\" is not a three-letter alphabetic code, so it is not "+
				"an ISO-4217 currency",
			ev)
	}

	return Pass(d, s,
		"anchor_asset is \""+strings.ToUpper(declared)+"\", a well-formed ISO-4217 code distinct "+
			"from the token's own",
		ev)
}

// findCurrency locates a [[CURRENCIES]] entry by code and issuer.
//
// Both must match. Matching on code alone would let any anchor's declaration
// answer for any issuer's token, which is the identity confusion this project
// refuses everywhere else.
func findCurrency(p *anchor.Profile, code, issuer string) (anchor.Currency, bool) {
	for _, c := range p.TOML.Currencies {
		if c.Code != code {
			continue
		}
		if issuer != "" && c.Issuer != issuer {
			continue
		}
		return c, true
	}
	return anchor.Currency{}, false
}

// isISO4217Shaped reports whether s has the shape of an ISO-4217 alphabetic
// code. Shape only — see the descriptor's CannotDetermine.
func isISO4217Shaped(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}

func quoteOrAbsent(s string) string {
	if s == "" {
		return "(field absent or empty)"
	}
	return "\"" + s + "\""
}
