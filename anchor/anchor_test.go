package anchor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/Wayfare-labs/wayfare/asset"
)

// ngncTOML reproduces the fields observed in the live stellar.toml served by
// ngnc.online on 2026-08-04. It is trimmed to the fields this package reads,
// but nothing has been added: in particular there is no ANCHOR_QUOTE_SERVER,
// because the real file has none.
const ngncTOML = `
VERSION="2.0.0"
NETWORK_PASSPHRASE="Public Global Stellar Network ; September 2015"
WEB_AUTH_ENDPOINT="https://anchor.ngnc.online/auth"
TRANSFER_SERVER_SEP0024="https://anchor.ngnc.online/sep24"

[[CURRENCIES]]
code="NGNC"
issuer="GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"
status="live"
is_asset_anchored=true
anchor_asset="NGN"

[[CURRENCIES]]
code="GHSC"
issuer="GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"
status="live"

[[CURRENCIES]]
code="KESC"
issuer="GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"
status="live"
`

// testanchorTOML reproduces the fields observed at testanchor.stellar.org on
// 2026-08-04. SDF's reference anchor implements the full set, and serves as
// the positive control against which the naira anchor's gaps are visible.
const testanchorTOML = `
NETWORK_PASSPHRASE = "Test SDF Network ; September 2015"
WEB_AUTH_ENDPOINT = "https://testanchor.stellar.org/auth"
KYC_SERVER = "https://testanchor.stellar.org/sep12"
TRANSFER_SERVER = "https://testanchor.stellar.org/sep6"
TRANSFER_SERVER_SEP0024 = "https://testanchor.stellar.org/sep24"
DIRECT_PAYMENT_SERVER = "https://testanchor.stellar.org/sep31"
ANCHOR_QUOTE_SERVER = "https://testanchor.stellar.org/sep38"
ORG_URL = "https://stellar.org"
`

func parseProfile(t *testing.T, domain, raw string) *Profile {
	t.Helper()
	var tm TOML
	if _, err := toml.Decode(raw, &tm); err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	return profileFrom(domain, tm)
}

// TestNairaAnchorIsNotPriceable is the regression test for the central
// finding of this project.
//
// ngnc.online issues NGNC, the token the naira leg of the corridor
// terminates in, and it is a real mainnet anchor in production. It publishes
// no ANCHOR_QUOTE_SERVER, so no program can obtain a rate from it. Any future
// change that reports this anchor as priceable is either a parsing bug or a
// wishful default, and both would cause the router to present an invented
// rate as though it came from the anchor.
func TestNairaAnchorIsNotPriceable(t *testing.T) {
	p := parseProfile(t, "ngnc.online", ngncTOML)

	if p.Priceable {
		t.Error("ngnc.online reported as priceable, but it publishes no ANCHOR_QUOTE_SERVER")
	}
	if !p.SEP24 {
		t.Error("expected SEP-24 support")
	}
	if p.SEP31 {
		t.Error("SEP-31 reported, but the anchor publishes no DIRECT_PAYMENT_SERVER")
	}
	if !p.Mainnet {
		t.Error("expected the mainnet network passphrase")
	}

	if _, err := p.QuoteClientBaseURL(); err == nil {
		t.Fatal("QuoteClientBaseURL should refuse for a non-priceable anchor")
	}
}

func TestNairaAnchorSupportsNGNC(t *testing.T) {
	p := parseProfile(t, "ngnc.online", ngncTOML)

	if !p.SupportsAsset(asset.NGNC()) {
		t.Error("expected the anchor to list NGNC as live")
	}
	// Same code, different issuer, must not match.
	impostor := asset.Stellar("NGNC", "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF")
	if p.SupportsAsset(impostor) {
		t.Error("matched NGNC from an unrelated issuer: code-only matching is unsafe")
	}
	if got, want := len(p.LiveCurrencies()), 3; got != want {
		t.Errorf("LiveCurrencies = %d, want %d", got, want)
	}
}

func TestReferenceAnchorIsPriceable(t *testing.T) {
	p := parseProfile(t, "testanchor.stellar.org", testanchorTOML)

	if !p.Priceable {
		t.Fatal("expected testanchor to be priceable")
	}
	if !p.SEP31 || !p.SEP6 || !p.SEP24 {
		t.Errorf("expected all transfer flows: sep24=%v sep31=%v sep6=%v", p.SEP24, p.SEP31, p.SEP6)
	}
	if p.Mainnet {
		t.Error("testanchor declares the test network, must not be reported as mainnet")
	}

	url, err := p.QuoteClientBaseURL()
	if err != nil {
		t.Fatalf("QuoteClientBaseURL: %v", err)
	}
	if want := "https://testanchor.stellar.org/sep38"; url != want {
		t.Errorf("quote server = %q, want %q", url, want)
	}
}

// TestExplainStatesTheGap checks that a non-priceable anchor is described in
// terms a reader cannot mistake for partial support.
func TestExplainStatesTheGap(t *testing.T) {
	p := parseProfile(t, "ngnc.online", ngncTOML)
	out := p.Explain()

	if !strings.Contains(out, "NONE") {
		t.Errorf("Explain() should state plainly that no quotes are available:\n%s", out)
	}
	if !strings.Contains(out, "ANCHOR_QUOTE_SERVER") {
		t.Errorf("Explain() should name the missing field so the claim is checkable:\n%s", out)
	}
}

// TestNairaAnchorSEPInventory pins the capability inventory issue #184
// introduces against the same real fixture TestNairaAnchorIsNotPriceable
// uses: ngnc.online publishes only WEB_AUTH_ENDPOINT and
// TRANSFER_SERVER_SEP0024, so it must advertise exactly SEP-1, SEP-10 and
// SEP-24 — no more (no KYC_SERVER, TRANSFER_SERVER, DIRECT_PAYMENT_SERVER or
// ANCHOR_QUOTE_SERVER in the fixture) and no less.
func TestNairaAnchorSEPInventory(t *testing.T) {
	p := parseProfile(t, "ngnc.online", ngncTOML)

	if !p.SEP10 {
		t.Error("expected SEP-10 support: the fixture declares WEB_AUTH_ENDPOINT")
	}
	if p.SEP12 {
		t.Error("SEP-12 reported, but the fixture declares no KYC_SERVER")
	}

	got := p.SEPs()
	want := []int{1, 10, 24}
	if !slicesEqual(got, want) {
		t.Errorf("SEPs() = %v, want %v", got, want)
	}
}

// TestReferenceAnchorSEPInventory covers the positive control: SDF's test
// anchor declares every field this package reads, so it must advertise every
// SEP number this package knows about.
func TestReferenceAnchorSEPInventory(t *testing.T) {
	p := parseProfile(t, "testanchor.stellar.org", testanchorTOML)

	if !p.SEP10 || !p.SEP12 {
		t.Errorf("expected SEP-10 and SEP-12: sep10=%v sep12=%v", p.SEP10, p.SEP12)
	}

	got := p.SEPs()
	want := []int{1, 6, 10, 12, 24, 31, 38}
	if !slicesEqual(got, want) {
		t.Errorf("SEPs() = %v, want %v", got, want)
	}

	caps := p.SEPCapabilities()
	if len(caps) != len(want) {
		t.Fatalf("SEPCapabilities() has %d entries, want %d: %v", len(caps), len(want), caps)
	}
	for _, want := range []string{"SEP-1 ", "SEP-10 ", "SEP-38 "} {
		found := false
		for _, c := range caps {
			if strings.HasPrefix(c, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("SEPCapabilities() = %v, want an entry starting with %q", caps, want)
		}
	}
}

// TestSEPsIsAscendingAndDeterministic guards the ordering claim: two calls
// against the same profile must agree, and the result must already be
// sorted, so a caller never needs to sort it again or worry a rebuild
// reordered the map this is derived from.
func TestSEPsIsAscendingAndDeterministic(t *testing.T) {
	p := parseProfile(t, "testanchor.stellar.org", testanchorTOML)

	first := p.SEPs()
	for i := 0; i < 5; i++ {
		again := p.SEPs()
		if !slicesEqual(first, again) {
			t.Fatalf("SEPs() is not deterministic: %v then %v", first, again)
		}
	}
	for i := 1; i < len(first); i++ {
		if first[i-1] >= first[i] {
			t.Errorf("SEPs() = %v is not strictly ascending at index %d", first, i)
		}
	}
}

// TestSEPsOnEmptyProfileIsJustSEP1 covers the floor: an anchor that declares
// nothing this package reads beyond the document itself still gets credit
// for SEP-1, because a Profile only exists when a stellar.toml was fetched.
func TestSEPsOnEmptyProfileIsJustSEP1(t *testing.T) {
	p := parseProfile(t, "bare.example", `ORG_NAME="Bare"`)

	got := p.SEPs()
	want := []int{1}
	if !slicesEqual(got, want) {
		t.Errorf("SEPs() = %v, want %v", got, want)
	}
}

// TestExplainListsSEPCapabilities pins that the human-readable summary and
// the machine-readable inventory cannot drift apart: Explain must mention
// every SEP SEPs() reports, by number.
func TestExplainListsSEPCapabilities(t *testing.T) {
	p := parseProfile(t, "ngnc.online", ngncTOML)
	out := p.Explain()

	for _, n := range p.SEPs() {
		tag := fmt.Sprintf("SEP-%d", n)
		if !strings.Contains(out, tag) {
			t.Errorf("Explain() does not mention %s, which SEPs() reports advertised:\n%s", tag, out)
		}
	}
}

func slicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestDeadCurrenciesAreExcluded ensures a retired asset is not routed to.
func TestDeadCurrenciesAreExcluded(t *testing.T) {
	raw := `
[[CURRENCIES]]
code="OLD"
issuer="GABC"
status="dead"

[[CURRENCIES]]
code="NEW"
issuer="GDEF"
status="live"
`
	p := parseProfile(t, "example.com", raw)
	live := p.LiveCurrencies()
	if len(live) != 1 || live[0].Code != "NEW" {
		t.Errorf("expected only the live currency, got %+v", live)
	}
	if p.SupportsAsset(asset.Stellar("OLD", "GABC")) {
		t.Error("a currency marked dead must not be reported as supported")
	}
}

// malformedNGNC reproduces the defect in the live ngnc.online stellar.toml as
// served on 2026-08-04: a stray "s" after a quoted value on the image line.
// A conforming TOML parser rejects the whole document over it.
const malformedNGNC = `
NETWORK_PASSPHRASE="Public Global Stellar Network ; September 2015"
WEB_AUTH_ENDPOINT="https://anchor.ngnc.online/auth"
TRANSFER_SERVER_SEP0024="https://anchor.ngnc.online/sep24"

[[CURRENCIES]]
code="NGNC"
issuer="GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"
status="live"
is_asset_anchored=true

[[CURRENCIES]]
code="KESC"
issuer="GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"
status="pending"
image="https://uploads-ssl.webflow.com/6512d/65f06c_KESc.png" s
`

// TestSalvageRecoversMalformedTOML pins the behaviour that keeps the most
// important anchor in this corridor visible.
//
// Strict parsing must still fail — that is what makes the document malformed
// — but the capability fields have to survive, because "the anchor cannot be
// priced" is a conclusion that must not depend on a stray character in an
// unrelated image URL.
func TestSalvageRecoversMalformedTOML(t *testing.T) {
	var strict TOML
	if _, err := toml.Decode(malformedNGNC, &strict); err == nil {
		t.Fatal("fixture is valid TOML; it no longer reproduces the live defect")
	}

	p := profileFrom("ngnc.online", salvageTOML(malformedNGNC))

	if p.Priceable {
		t.Error("salvaged profile reported as priceable; it publishes no ANCHOR_QUOTE_SERVER")
	}
	if !p.SEP24 {
		t.Error("salvage lost TRANSFER_SERVER_SEP0024")
	}
	if !p.Mainnet {
		t.Error("salvage lost NETWORK_PASSPHRASE")
	}
	if !p.SupportsAsset(asset.NGNC()) {
		t.Error("salvage lost the NGNC currency entry")
	}

	// KESC is status="pending", so it must not be treated as live even
	// though it is the entry carrying the malformation.
	if p.SupportsAsset(asset.Stellar("KESC", asset.NGNCIssuer)) {
		t.Error("KESC is pending, not live, and must not be routable")
	}
	if got, want := len(p.LiveCurrencies()), 1; got != want {
		t.Errorf("LiveCurrencies = %d, want %d", got, want)
	}
}

// TestSalvageHandlesTrailingJunkValues covers the unquote helper directly.
func TestSalvageHandlesTrailingJunkValues(t *testing.T) {
	cases := map[string]string{
		`"https://example.com/a.png" s`: "https://example.com/a.png",
		`"plain"`:                       "plain",
		`"with \"escape\""`:             `with "escape"`,
		`true`:                          "true",
		`2 # trailing comment`:          "2",
		`"unterminated`:                 "unterminated",
	}
	for in, want := range cases {
		if got := unquote(in); got != want {
			t.Errorf("unquote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSalvageIgnoresUnknownSectionsAndKeepsCurrencyBoundaries(t *testing.T) {
	raw := `
NETWORK_PASSPHRASE="Public Global Stellar Network ; September 2015" trailing

[DOCUMENTATION]
ORG_NAME="not a currency"

[[CURRENCIES]]
code="AAA" junk
issuer="GAAA"
status="live"

[[CURRENCIES]]
code="BBB"
status="pending"
`
	got := salvageTOML(raw)
	if got.NetworkPassphrase == "" {
		t.Fatal("salvage lost top-level fields after an unknown section")
	}
	if len(got.Currencies) != 2 {
		t.Fatalf("salvaged %d currencies, want 2", len(got.Currencies))
	}
	if got.Currencies[0].Code != "AAA" || got.Currencies[1].Code != "BBB" {
		t.Fatalf("currency boundaries were not preserved: %+v", got.Currencies)
	}
}

func TestTOMLURL(t *testing.T) {
	for _, in := range []string{"ngnc.online", "https://ngnc.online", "ngnc.online/"} {
		if got, want := TOMLURL(in), "https://ngnc.online/.well-known/stellar.toml"; got != want {
			t.Errorf("TOMLURL(%q) = %q, want %q", in, got, want)
		}
	}
}
