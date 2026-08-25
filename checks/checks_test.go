package checks

import (
	"context"
	"encoding/base32"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/anchor"
	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

func ctx() context.Context { return context.Background() }

// profileWith builds a resolved anchor profile from currency entries.
func profileWith(domain string, cs ...anchor.Currency) *anchor.Profile {
	return &anchor.Profile{Domain: domain, TOML: anchor.TOML{Currencies: cs}}
}

// linkIO reproduces the three [[CURRENCIES]] entries published at
// ngnc.online, read 2026-08-08. KESC's anchor_asset is verbatim: it names the
// token rather than the shilling, and that defect is the reason this fixture
// is real rather than invented.
func linkIO() *anchor.Profile {
	const iss = asset.LinkIOIssuer
	return profileWith("ngnc.online",
		anchor.Currency{Code: "NGNC", Issuer: iss, Status: "live", AnchorAsset: "NGN"},
		anchor.Currency{Code: "GHSC", Issuer: iss, Status: "pending", AnchorAsset: "GHS"},
		anchor.Currency{Code: "KESC", Issuer: iss, Status: "pending", AnchorAsset: "KESC"},
	)
}

// contract-level behaviour -----------------------------------------------------

// TestUndeterminedIsNotAFailure is the contract's central distinction. A
// result that could not be established must not read as a failing one, and
// Failed() is what every consumer uses to tell them apart.
func TestUndeterminedIsNotAFailure(t *testing.T) {
	d := AnchorAssetISO4217{}.Describe()
	r := Undetermined(d, Subject{Domain: "example.test"}, "nothing was published")

	if r.Determined {
		t.Error("an undetermined result reports Determined true")
	}
	if r.Passed {
		t.Error("Passed must be false when nothing was determined")
	}
	if r.Failed() {
		t.Error("Failed() must be false for an undetermined result — this is the whole " +
			"distinction the package exists to preserve")
	}
	if r.Reason == "" {
		t.Error("an undetermined result must carry a reason")
	}
}

// TestUndeterminedWithoutAReasonIsRepaired covers the contributor who forgets.
// A silent "unknown" tells a reader nothing about whether to look further.
func TestUndeterminedWithoutAReasonIsRepaired(t *testing.T) {
	d := AnchorAssetISO4217{}.Describe()
	r := Undetermined(d, Subject{Domain: "example.test"}, "   ")

	if !strings.Contains(r.Reason, "bug in check") {
		t.Errorf("Reason = %q, want it to name the omission as a bug", r.Reason)
	}
}

// TestDescriptorRequiresItsLimits pins the mandatory CannotDetermine field.
// The likeliest way this misleads is a correct result read as answering more
// than it does.
func TestDescriptorRequiresItsLimits(t *testing.T) {
	cases := map[string]Descriptor{
		"no id":              {Title: "t", CanDetermine: "a", CannotDetermine: "b"},
		"no title":           {ID: "x", CanDetermine: "a", CannotDetermine: "b"},
		"no CanDetermine":    {ID: "x", Title: "t", CannotDetermine: "b"},
		"no CannotDetermine": {ID: "x", Title: "t", CanDetermine: "a"},
	}
	for name, d := range cases {
		t.Run(name, func(t *testing.T) {
			if err := d.Validate(); err == nil {
				t.Error("expected an invalid descriptor to be rejected")
			}
		})
	}

	for _, c := range []Check{AnchorAssetISO4217{}, IssuerAuthFlags{}, SEP10EndpointResponds{}, HomeDomainRoundTrip{}} {
		d := c.Describe()
		if err := d.Validate(); err != nil {
			t.Errorf("%s: %v", d.ID, err)
		}
	}
}

// panicCheck is a contributed check behaving badly.
type panicCheck struct{}

func (panicCheck) Describe() Descriptor {
	return Descriptor{
		ID: "test.panics", Title: "panics",
		CanDetermine: "nothing", CannotDetermine: "anything",
	}
}
func (panicCheck) Run(context.Context, Subject) CheckResult { panic("boom") }

// TestPanicBecomesUndetermined pins that one bad check cannot take down a
// sweep. The other corridors are still worth measuring.
func TestPanicBecomesUndetermined(t *testing.T) {
	r := Run(ctx(), panicCheck{}, Subject{Domain: "example.test"})

	if r.Determined {
		t.Error("a panicking check must not produce a determined result")
	}
	if !strings.Contains(r.Reason, "panicked") {
		t.Errorf("Reason = %q, want it to say the check panicked", r.Reason)
	}
}

// TestCancelledContextIsUndetermined checks a cancelled sweep blames the
// context rather than the subject.
func TestCancelledContextIsUndetermined(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	r := Run(cancelled, AnchorAssetISO4217{}, Subject{Profile: linkIO(), Asset: asset.NGNC()})
	if r.Determined {
		t.Error("a cancelled context must not produce a determined result")
	}
}

// TestFindingsWorstIgnoresUndetermined guards against the collapse this
// package prevents leaking back in through severity. Not knowing something is
// not a finding against the subject.
func TestFindingsWorstIgnoresUndetermined(t *testing.T) {
	d := IssuerAuthFlags{}.Describe() // SeverityCritical
	s := Subject{Domain: "example.test"}

	var f Findings
	f.Add(Undetermined(d, s, "unreachable"))

	if _, any := f.Worst(); any {
		t.Error("an undetermined critical check raised a severity; only failures may")
	}

	f.Add(Fail(d, s, "clawback enabled"))
	worst, any := f.Worst()
	if !any || worst != SeverityCritical {
		t.Errorf("Worst() = %v, %v; want critical, true once something actually failed", worst, any)
	}
}

// TestFindingsCountsAndOrder covers what a reader sees first.
func TestFindingsCountsAndOrder(t *testing.T) {
	s := Subject{Domain: "example.test"}
	crit := IssuerAuthFlags{}.Describe()
	warn := SEP10EndpointResponds{}.Describe()
	note := AnchorAssetISO4217{}.Describe()

	var f Findings
	f.Add(Pass(note, s, "fine"))
	f.Add(Undetermined(warn, s, "not declared"))
	f.Add(Fail(warn, s, "endpoint dead"))
	f.Add(Fail(crit, s, "clawback enabled"))

	passed, failed, undet := f.Counts()
	if passed != 1 || failed != 2 || undet != 1 {
		t.Errorf("counts = %d/%d/%d, want 1 passed, 2 failed, 1 undetermined",
			passed, failed, undet)
	}

	order := f.Sorted()
	if !order[0].Failed() || order[0].Severity != SeverityCritical {
		t.Errorf("first result is %q (sev %v); the most severe failure must lead",
			order[0].ID, order[0].Severity)
	}
	if order[len(order)-1].Failed() || !order[len(order)-1].Determined {
		t.Error("a passing result should sort last")
	}
}

// toml.anchor-asset-iso4217 ----------------------------------------------------

func TestAnchorAssetISO4217(t *testing.T) {
	cases := []struct {
		name       string
		profile    *anchor.Profile
		subject    asset.Asset
		determined bool
		passed     bool
		contains   string
	}{
		{
			name:       "NGNC declares NGN correctly",
			profile:    linkIO(),
			subject:    asset.NGNC(),
			determined: true, passed: true,
			contains: "NGN",
		},
		{
			// The negative case, and it is real: read from ngnc.online on
			// 2026-08-08, KESC names itself rather than the shilling.
			name:       "KESC names its own token, not the currency",
			profile:    linkIO(),
			subject:    asset.KESC(),
			determined: true, passed: false,
			contains: "repeats the token's own code",
		},
		{
			name:       "GHSC declares GHS correctly",
			profile:    linkIO(),
			subject:    asset.GHSC(),
			determined: true, passed: true,
			contains: "GHS",
		},
		{
			name: "a four-letter code is not ISO-4217",
			profile: profileWith("example.test", anchor.Currency{
				Code: "ABCD", Issuer: "GISSUER", AnchorAsset: "EURO"}),
			subject:    asset.Stellar("ABCD", "GISSUER"),
			determined: true, passed: false,
			contains: "not a three-letter alphabetic code",
		},
		{
			name: "an absent anchor_asset is undetermined, not failed",
			profile: profileWith("example.test", anchor.Currency{
				Code: "XXXC", Issuer: "GISSUER"}),
			subject:    asset.Stellar("XXXC", "GISSUER"),
			determined: false,
			contains:   "declares no anchor_asset",
		},
		{
			name:       "an asset the document does not list is undetermined",
			profile:    linkIO(),
			subject:    asset.Stellar("ZARZ", "GSOMEONEELSE"),
			determined: false,
			contains:   "lists no [[CURRENCIES]] entry",
		},
		{
			name:       "no profile at all is undetermined",
			profile:    nil,
			subject:    asset.NGNC(),
			determined: false,
			contains:   "no stellar.toml",
		},
		{
			// Identity: the same code from a different issuer is a
			// different asset, and this document says nothing about it.
			name:       "right code, wrong issuer is undetermined",
			profile:    linkIO(),
			subject:    asset.Stellar("NGNC", "GIMPOSTORXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"),
			determined: false,
			contains:   "lists no [[CURRENCIES]] entry",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Run(ctx(), AnchorAssetISO4217{},
				Subject{Profile: tc.profile, Asset: tc.subject})

			if r.Determined != tc.determined {
				t.Fatalf("Determined = %v, want %v (summary: %s)",
					r.Determined, tc.determined, r.Summary)
			}
			if tc.determined && r.Passed != tc.passed {
				t.Errorf("Passed = %v, want %v (summary: %s)", r.Passed, tc.passed, r.Summary)
			}
			haystack := r.Summary + " " + r.Reason
			if !strings.Contains(haystack, tc.contains) {
				t.Errorf("result %q does not mention %q", haystack, tc.contains)
			}
			if r.Determined && len(r.Evidence) == 0 {
				t.Error("a determined result must carry evidence a reader can check")
			}
		})
	}
}

// issuer.auth-flags ------------------------------------------------------------

func flagServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func flagsJSON(required, revocable, immutable, clawback bool) string {
	return `{"flags":{"auth_required":` + b(required) +
		`,"auth_revocable":` + b(revocable) +
		`,"auth_immutable":` + b(immutable) +
		`,"auth_clawback_enabled":` + b(clawback) + `}}`
}

func b(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func TestIssuerAuthFlags(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		determined bool
		passed     bool
		contains   string
	}{
		{
			// The LINK.IO issuer as measured on 2026-08-22: no flags set.
			name:   "no flags set",
			status: 200, body: flagsJSON(false, false, false, false),
			determined: true, passed: true,
			contains: "neither freeze nor claw back",
		},
		{
			// Circle's USDC issuer as measured on 2026-08-22. The asset
			// most people treat as safest is the one whose issuer retains
			// the power to freeze it.
			name:   "revocable — the real USDC case",
			status: 200, body: flagsJSON(false, true, false, false),
			determined: true, passed: false,
			contains: "freeze your balance",
		},
		{
			name:   "clawback enabled",
			status: 200, body: flagsJSON(false, false, false, true),
			determined: true, passed: false,
			contains: "claw the asset back",
		},
		{
			name:   "auth_required alone is friction, not a failure",
			status: 200, body: flagsJSON(true, false, false, false),
			determined: true, passed: true,
			contains: "auth_required",
		},
		{
			name:   "immutable flags are worth saying",
			status: 200, body: flagsJSON(false, false, true, false),
			determined: true, passed: true,
			contains: "immutable",
		},
		{
			name:   "a missing account is a determined failure",
			status: 404, body: `{"status":404}`,
			determined: true, passed: false,
			contains: "does not exist",
		},
		{
			name:   "a server error is undetermined, not a failure",
			status: 503, body: `{}`,
			determined: false,
			contains:   "HTTP 503",
		},
		{
			name:   "an unparseable body is undetermined",
			status: 200, body: `not json`,
			determined: false,
			contains:   "could not be parsed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := flagServer(t, tc.status, tc.body)
			c := IssuerAuthFlags{HorizonURL: srv.URL, HTTPClient: srv.Client()}

			r := Run(ctx(), c, Subject{Asset: asset.NGNC()})

			if r.Determined != tc.determined {
				t.Fatalf("Determined = %v, want %v (summary: %s, reason: %s)",
					r.Determined, tc.determined, r.Summary, r.Reason)
			}
			if tc.determined && r.Passed != tc.passed {
				t.Errorf("Passed = %v, want %v (summary: %s)", r.Passed, tc.passed, r.Summary)
			}
			if !strings.Contains(r.Summary+" "+r.Reason, tc.contains) {
				t.Errorf("result %q / %q does not mention %q", r.Summary, r.Reason, tc.contains)
			}
		})
	}
}

// TestIssuerAuthFlagsUnreachableIsUndetermined pins that a network problem
// never becomes a finding against the issuer.
func TestIssuerAuthFlagsUnreachableIsUndetermined(t *testing.T) {
	c := IssuerAuthFlags{HorizonURL: "http://127.0.0.1:1"}
	r := Run(ctx(), c, Subject{Asset: asset.NGNC()})

	if r.Determined {
		t.Error("an unreachable Horizon must not produce a determined result — " +
			"that would blame the issuer for a network failure")
	}
	if !strings.Contains(r.Reason, "unreachable") {
		t.Errorf("Reason = %q, want it to name the network as the cause", r.Reason)
	}
}

// TestIssuerAuthFlagsNeedsAnIssuer covers a subject with nothing to read.
func TestIssuerAuthFlagsNeedsAnIssuer(t *testing.T) {
	r := Run(ctx(), IssuerAuthFlags{}, Subject{Asset: asset.Native()})
	if r.Determined {
		t.Error("native XLM has no issuing account; the result must be undetermined")
	}
}

// sep10.endpoint-responds ------------------------------------------------------

func sep10Profile(endpoint string) *anchor.Profile {
	return &anchor.Profile{
		Domain: "example.test",
		TOML:   anchor.TOML{WebAuthEndpoint: endpoint},
	}
}

func TestSEP10EndpointResponds(t *testing.T) {
	challenge := `{"transaction":"AAAAAgAAAABmocked","network_passphrase":"Public Global Stellar Network ; September 2015"}`

	cases := []struct {
		name       string
		status     int
		body       string
		determined bool
		passed     bool
		contains   string
	}{
		{
			name:   "a well-formed challenge",
			status: 200, body: challenge,
			determined: true, passed: true,
			contains: "well-formed challenge",
		},
		{
			name:   "200 with no transaction is not a challenge",
			status: 200, body: `{"network_passphrase":"x"}`,
			determined: true, passed: false,
			contains: "no transaction",
		},
		{
			name:   "a declared endpoint returning 500 fails",
			status: 500, body: `{}`,
			determined: true, passed: false,
			contains: "HTTP 500",
		},
		{
			name:   "a non-JSON body fails",
			status: 200, body: `<html>hello</html>`,
			determined: true, passed: false,
			contains: "not JSON",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := flagServer(t, tc.status, tc.body)
			c := SEP10EndpointResponds{HTTPClient: srv.Client()}

			r := Run(ctx(), c, Subject{Profile: sep10Profile(srv.URL + "/auth")})

			if r.Determined != tc.determined {
				t.Fatalf("Determined = %v, want %v (summary %s, reason %s)",
					r.Determined, tc.determined, r.Summary, r.Reason)
			}
			if tc.determined && r.Passed != tc.passed {
				t.Errorf("Passed = %v, want %v (%s)", r.Passed, tc.passed, r.Summary)
			}
			if !strings.Contains(r.Summary+" "+r.Reason, tc.contains) {
				t.Errorf("result %q / %q does not mention %q", r.Summary, r.Reason, tc.contains)
			}
		})
	}
}

// TestSEP10NotDeclaredIsUndetermined is the distinction the check exists for.
// An anchor offering no programmatic authentication has said nothing wrong.
func TestSEP10NotDeclaredIsUndetermined(t *testing.T) {
	r := Run(ctx(), SEP10EndpointResponds{}, Subject{Profile: sep10Profile("")})

	if r.Determined {
		t.Error("an undeclared endpoint must be undetermined, not a failure — " +
			"offering no authentication differs from claiming one that is dead")
	}
	if !strings.Contains(r.Reason, "declares no WEB_AUTH_ENDPOINT") {
		t.Errorf("Reason = %q, want it to say the field was absent", r.Reason)
	}
}

// TestSEP10DeclaredButDeadIsAFailure is the other half, and the more damning
// one: publishing an address that does not answer.
func TestSEP10DeclaredButDeadIsAFailure(t *testing.T) {
	r := Run(ctx(), SEP10EndpointResponds{}, Subject{
		Profile: sep10Profile("https://127.0.0.1:1/auth"),
	})

	if !r.Failed() {
		t.Errorf("a declared endpoint that does not respond must fail, got determined=%v passed=%v",
			r.Determined, r.Passed)
	}
	if !strings.Contains(r.Summary, "did not respond") {
		t.Errorf("Summary = %q, want it to say the endpoint did not respond", r.Summary)
	}
}

// TestSEP10MalformedEndpointFails covers a published value that is not a URL.
func TestSEP10MalformedEndpointFails(t *testing.T) {
	for _, bad := range []string{"not a url", "ftp://example.test/auth", "://missing-scheme"} {
		r := Run(ctx(), SEP10EndpointResponds{}, Subject{Profile: sep10Profile(bad)})
		if !r.Failed() {
			t.Errorf("%q: expected a determined failure, got determined=%v passed=%v",
				bad, r.Determined, r.Passed)
		}
	}
}

// TestChallengeURLCarriesTheAccount pins the SEP-10 request shape.
func TestChallengeURLCarriesTheAccount(t *testing.T) {
	got, err := buildChallengeURL("https://anchor.test/auth?foo=bar", "GABC")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "account=GABC") {
		t.Errorf("URL = %q, want it to carry the account parameter", got)
	}
	if !strings.Contains(got, "foo=bar") {
		t.Errorf("URL = %q, want the endpoint's own query preserved", got)
	}
}

// TestEvidenceIsTimestamped covers the audit property: a reader must be able
// to tell when an observation was made.
func TestEvidenceIsTimestamped(t *testing.T) {
	before := time.Now().UTC().Add(-time.Second)

	r := Run(ctx(), AnchorAssetISO4217{}, Subject{Profile: linkIO(), Asset: asset.NGNC()})
	if len(r.Evidence) == 0 {
		t.Fatal("no evidence recorded")
	}
	for _, e := range r.Evidence {
		if e.Source == "" || e.Observed == "" {
			t.Errorf("evidence %+v is missing a source or an observation", e)
		}
		if e.ObservedAt.Before(before) {
			t.Errorf("evidence timestamp %s predates the run", e.ObservedAt)
		}
	}
}

// TestRunAllKeepsGoing pins that one bad check does not stop the others.
func TestRunAllKeepsGoing(t *testing.T) {
	results := RunAll(ctx(),
		[]Check{panicCheck{}, AnchorAssetISO4217{}},
		Subject{Profile: linkIO(), Asset: asset.NGNC()})

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 — a panic stopped the sweep", len(results))
	}
	if results[1].Determined != true || !results[1].Passed {
		t.Error("the check after the panicking one did not run correctly")
	}
}

// TestSEP10ProbeAccountIsWellFormed pins the checksum.
//
// This constant had an invalid CRC once, so every anchor correctly rejected it
// with HTTP 400 and the check reported a false failure against healthy
// endpoints. A check that blames a subject for the checker's own malformed
// request is worse than no check, so the encoding is verified here rather than
// trusted.
func TestSEP10ProbeAccountIsWellFormed(t *testing.T) {
	const addr = probeAccount

	if len(addr) != 56 {
		t.Fatalf("length = %d, want 56", len(addr))
	}
	if addr[0] != 'G' {
		t.Fatalf("first byte = %q, want 'G' (ed25519 public key)", addr[0])
	}

	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(addr)
	if err != nil {
		t.Fatalf("not valid base32: %v", err)
	}
	if len(raw) != 35 {
		t.Fatalf("decoded length = %d, want 35 (version + 32 payload + 2 checksum)", len(raw))
	}
	if raw[0] != 0x30 {
		t.Errorf("version byte = %#x, want 0x30", raw[0])
	}

	want := crc16XModem(raw[:33])
	got := uint16(raw[33]) | uint16(raw[34])<<8
	if got != want {
		t.Errorf("checksum = %#04x, want %#04x — this address would be rejected "+
			"by every anchor and Horizon", got, want)
	}
}

func crc16XModem(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// TestSEP10RejectedProbeIsUndetermined covers the other half of that bug.
//
// An anchor rejecting our probe account is our defect, not its. Reporting it
// as a failure would blame a healthy endpoint for a request we malformed.
func TestSEP10RejectedProbeIsUndetermined(t *testing.T) {
	for _, msg := range []string{
		`{"error":"Invalid Ed25519 Public Key: GAAA"}`,
		`{"error":"invalid account"}`,
		`{"error":"malformed account id"}`,
	} {
		srv := flagServer(t, 400, msg)
		r := Run(ctx(), SEP10EndpointResponds{HTTPClient: srv.Client()},
			Subject{Profile: sep10Profile(srv.URL + "/auth")})

		if r.Determined {
			t.Errorf("%s: reported a determined result; an endpoint rejecting our "+
				"probe account is the checker's defect, not the anchor's", msg)
		}
		if !strings.Contains(r.Reason, "defect in the probe") {
			t.Errorf("%s: reason %q should name the probe as the problem", msg, r.Reason)
		}
	}
}

// TestSEP10GenuineErrorStillFails is the control: a 500, or a 400 that does
// not name our account, remains a real failure.
func TestSEP10GenuineErrorStillFails(t *testing.T) {
	for _, body := range []string{
		`{"error":"internal server error"}`,
		`{"error":"service temporarily unavailable"}`,
		`{}`,
	} {
		srv := flagServer(t, 400, body)
		r := Run(ctx(), SEP10EndpointResponds{HTTPClient: srv.Client()},
			Subject{Profile: sep10Profile(srv.URL + "/auth")})

		if !r.Failed() {
			t.Errorf("%s: should still be a determined failure, got determined=%v passed=%v",
				body, r.Determined, r.Passed)
		}
	}
}

// sep24.info-lists-asset ------------------------------------------------------

func sep24Profile(server string) *anchor.Profile {
	return &anchor.Profile{
		Domain: "example.test",
		TOML: anchor.TOML{
			TransferServer24: server,
			Currencies:       []anchor.Currency{{Code: "NGNC", Issuer: "GISSUER", AnchorAsset: "NGN"}},
		},
	}
}

func TestSEP24InfoListsAsset(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		determined bool
		passed     bool
		contains   string
	}{
		{name: "deposit and withdrawal enabled", status: 200,
			body:       `{"deposit":{"NGN":{"enabled":true}},"withdraw":{"NGN":{"enabled":true}}}`,
			determined: true, passed: true, contains: "deposit enabled; withdrawal enabled"},
		{name: "deposit enabled and withdrawal disabled", status: 200,
			body:       `{"deposit":{"NGN":{"enabled":true}},"withdraw":{"NGN":{"enabled":false}}}`,
			determined: true, passed: true, contains: "deposit enabled; withdrawal disabled"},
		{name: "withdrawal enabled and deposit disabled", status: 200,
			body:       `{"deposit":{"NGN":{"enabled":false}},"withdraw":{"NGN":{"enabled":true}}}`,
			determined: true, passed: true, contains: "deposit disabled; withdrawal enabled"},
		{name: "asset omitted", status: 200,
			body:       `{"deposit":{"USD":{"enabled":true}},"withdraw":{"USD":{"enabled":true}}}`,
			determined: true, passed: false, contains: "omits asset \"NGN\""},
		{name: "server error", status: 503, body: `{}`,
			determined: true, passed: false, contains: "HTTP 503"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := flagServer(t, tc.status, tc.body)
			r := Run(ctx(), SEP24InfoListsAsset{HTTPClient: srv.Client()},
				Subject{Asset: asset.Stellar("NGNC", "GISSUER"), Profile: sep24Profile(srv.URL)})
			if r.Determined != tc.determined || (tc.determined && r.Passed != tc.passed) {
				t.Fatalf("result determined=%v passed=%v, want %v/%v (%s)", r.Determined, r.Passed, tc.determined, tc.passed, r.Summary)
			}
			if !strings.Contains(r.Summary+" "+r.Reason, tc.contains) {
				t.Errorf("result %q / %q does not mention %q", r.Summary, r.Reason, tc.contains)
			}
			if len(r.Evidence) == 0 {
				t.Error("result has no evidence")
			}
		})
	}
}

func TestSEP24InfoNotDeclaredIsUndetermined(t *testing.T) {
	r := Run(ctx(), SEP24InfoListsAsset{}, Subject{Asset: asset.Stellar("NGNC", "GISSUER"), Profile: sep24Profile("")})
	if r.Determined || !strings.Contains(r.Reason, "declares no TRANSFER_SERVER_SEP0024") {
		t.Fatalf("result = determined=%v reason=%q, want undetermined absent server", r.Determined, r.Reason)
	}
}

// SSRF guard ------------------------------------------------------------------

// TestGuardRejectsInternalTargets is the attack CodeRabbit identified.
//
// These URLs are not user input. They come from stellar.toml documents
// published by the anchors this tool audits, and they are fetched by a server
// on behalf of anyone who asks for a corridor. An anchor that wanted to could
// publish a WEB_AUTH_ENDPOINT pointing at cloud metadata or at whatever else
// the host can reach.
func TestGuardRejectsInternalTargets(t *testing.T) {
	blocked := map[string]string{
		"loopback v4":     "127.0.0.1",
		"loopback name":   "localhost",
		"cloud metadata":  "169.254.169.254",
		"private 10/8":    "10.0.0.1",
		"private 172.16":  "172.16.5.4",
		"private 192.168": "192.168.1.1",
		"unspecified":     "0.0.0.0",
		"loopback v6":     "[::1]",
		"unique-local v6": "[fd00::1]",
	}

	client := GuardedClient(3 * time.Second)
	for name, host := range blocked {
		t.Run(name, func(t *testing.T) {
			_, err := client.Get("http://" + host + "/latest/meta-data/")
			if err == nil {
				t.Fatalf("%s was reachable; an anchor-published URL must never "+
					"make this server probe its own network", host)
			}
			if !strings.Contains(err.Error(), "refusing to") {
				t.Errorf("%s: error %q does not name the guard as the cause", host, err)
			}
		})
	}
}

// TestGuardedCheckRefusesInternalEndpoint covers the guard where it matters:
// a check probing an endpoint the subject declared.
func TestGuardedCheckRefusesInternalEndpoint(t *testing.T) {
	c := SEP10EndpointResponds{} // no client supplied, so the guarded default
	r := Run(ctx(), c, Subject{
		Profile: sep10Profile("http://169.254.169.254/latest/meta-data/"),
	})

	// A refusal is a determined failure about the anchor, not an unknown:
	// publishing an endpoint that points inside the prober's network is a
	// fact about the anchor worth reporting.
	if !r.Failed() {
		t.Errorf("probing a metadata address should fail the check, got determined=%v passed=%v",
			r.Determined, r.Passed)
	}
}

// TestGuardAllowsPublicAddresses is the control. A guard that blocked
// everything would pass the tests above and make every check useless.
func TestGuardAllowsPublicAddresses(t *testing.T) {
	for _, ip := range []string{"93.184.216.34", "8.8.8.8", "2606:4700::1"} {
		parsed := net.ParseIP(strings.Trim(ip, "[]"))
		if parsed == nil {
			t.Fatalf("test bug: %q is not an IP", ip)
		}
		if disallowedIP(parsed) {
			t.Errorf("%s is a public address and must be reachable", ip)
		}
	}
}

// TestErrorBodyIsBounded covers the other finding: an endpoint returning an
// enormous body must not be decoded whole.
func TestErrorBodyIsBounded(t *testing.T) {
	huge := `{"error":"` + strings.Repeat("A", 512<<10) + `"}`
	srv := flagServer(t, 400, huge)

	r := Run(ctx(), SEP10EndpointResponds{HTTPClient: srv.Client()},
		Subject{Profile: sep10Profile(srv.URL + "/auth")})

	// Truncated JSON does not decode, so no message is extracted and the
	// endpoint is still reported as failing — which is correct. What matters
	// is that the whole body was never held in memory.
	if !r.Failed() {
		t.Errorf("an endpoint returning HTTP 400 should still fail, got determined=%v passed=%v",
			r.Determined, r.Passed)
	}
	for _, e := range r.Evidence {
		if len(e.Observed) > maxErrorBody {
			t.Errorf("evidence retained %d bytes, exceeding the %d-byte bound",
				len(e.Observed), maxErrorBody)
		}
	}
}

// spread metric ------------------------------------------------------------------

// loadOrderBookSnapshot loads a snapshot from checks/testdata/snapshots by prefix.
func loadOrderBookSnapshot(t *testing.T, prefix string) *snapshot.Manifest {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("testdata/snapshots", prefix+"-*"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no snapshot matching %q under testdata/snapshots (err=%v)", prefix, err)
	}
	m, err := snapshot.Load(matches[0])
	if err != nil {
		t.Fatalf("loading snapshot %s: %v", matches[0], err)
	}
	return m
}

func TestSpreadMetricFromRecordedBook(t *testing.T) {
	m := loadOrderBookSnapshot(t, "xlm-ngnc-orderbook")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	spread := SpreadMetric{DEX: c}
	r := RunMetric(ctx(), spread, Subject{
		Send:    asset.Native(),
		Receive: asset.NGNC(),
	})

	if !r.Determined {
		t.Fatalf("spread metric undetermined: %s", r.Reason)
	}
	if r.Unit != UnitPercent {
		t.Errorf("unit = %s, want percent", r.Unit)
	}
	if !r.Value.IsPositive() {
		t.Errorf("spread = %s, want positive on a real book", r.Value)
	}
	if len(r.Evidence) == 0 {
		t.Error("no evidence recorded")
	}
	if r.Summary == "" {
		t.Error("summary is empty")
	}
}

func TestSpreadMetricEmptyBook(t *testing.T) {
	m := loadOrderBookSnapshot(t, "xlm-ngnc-orderbook-empty")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	spread := SpreadMetric{DEX: c}
	r := RunMetric(ctx(), spread, Subject{
		Send:    asset.Native(),
		Receive: asset.NGNC(),
	})

	if r.Determined {
		t.Error("empty book must produce an undetermined result, not a zero spread")
	}
	if !strings.Contains(r.Reason, "empty") {
		t.Errorf("reason = %q, want it to name the empty book", r.Reason)
	}
}

func TestSpreadMetricOneSidedBook(t *testing.T) {
	m := loadOrderBookSnapshot(t, "xlm-ngnc-orderbook-onesided")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	spread := SpreadMetric{DEX: c}
	r := RunMetric(ctx(), spread, Subject{
		Send:    asset.Native(),
		Receive: asset.NGNC(),
	})

	if r.Determined {
		t.Error("one-sided book must produce an undetermined result")
	}
	if !strings.Contains(r.Reason, "nobody is buying") {
		t.Errorf("reason = %q, want it to name the missing side", r.Reason)
	}
}

func TestSpreadMetricNilDEX(t *testing.T) {
	spread := SpreadMetric{DEX: nil}
	r := RunMetric(ctx(), spread, Subject{
		Send:    asset.Native(),
		Receive: asset.NGNC(),
	})

	if r.Determined {
		t.Error("nil DEX client must produce an undetermined result")
	}
}

func TestSpreadMetricEmptySubject(t *testing.T) {
	spread := SpreadMetric{DEX: &dex.Client{HorizonURL: "http://example.invalid"}}
	r := RunMetric(ctx(), spread, Subject{})

	if r.Determined {
		t.Error("empty subject must produce an undetermined result")
	}
}

func TestSpreadMetricDescriptorIsValid(t *testing.T) {
	d := SpreadMetric{}.Describe()
	if err := d.Validate(); err != nil {
		t.Errorf("spread metric descriptor: %v", err)
	}
}

// depth metric ------------------------------------------------------------------

func TestDepthObservedMetric(t *testing.T) {
	m := loadOrderBookSnapshot(t, "xlm-ngnc-orderbook")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	depth := DepthMetric{DEX: c}
	r := depth.RunObserved(ctx(), Subject{
		Send:    asset.Native(),
		Receive: asset.NGNC(),
	})

	if !r.Determined {
		t.Fatalf("depth observed metric undetermined: %s", r.Reason)
	}
	if r.Unit != UnitCount {
		t.Errorf("unit = %s, want count", r.Unit)
	}
	if !r.Value.IsPositive() {
		t.Errorf("level count = %s, want positive on a real book", r.Value)
	}
}

func TestDepthObservedEmptyBook(t *testing.T) {
	m := loadOrderBookSnapshot(t, "xlm-ngnc-orderbook-empty")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	depth := DepthMetric{DEX: c}
	r := depth.RunObserved(ctx(), Subject{Send: asset.Native(), Receive: asset.NGNC()})

	if r.Determined {
		t.Error("empty book must produce an undetermined result, not a level count of zero")
	}
}

func TestDepthExecutableMetric(t *testing.T) {
	m := loadOrderBookSnapshot(t, "usdc-ngnc-strictsend")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	depth := DepthMetric{
		DEX:   c,
		Sizes: []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(100)},
	}
	r := depth.RunExecutable(ctx(), Subject{
		Send:    asset.USDC(),
		Receive: asset.NGNC(),
	})

	if !r.Determined {
		t.Fatalf("depth executable metric undetermined: %s", r.Reason)
	}
	if r.Unit != UnitAmount {
		t.Errorf("unit = %s, want amount", r.Unit)
	}
	if !r.Value.IsPositive() {
		t.Errorf("executable amount = %s, want positive", r.Value)
	}
}

func TestDepthNoPathsProducesUndetermined(t *testing.T) {
	m := loadOrderBookSnapshot(t, "usdc-ngnc-strictsend-empty")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	depth := DepthMetric{
		DEX:   c,
		Sizes: []decimal.Decimal{decimal.NewFromInt(1)},
	}
	r := depth.RunExecutable(ctx(), Subject{
		Send:    asset.USDC(),
		Receive: asset.NGNC(),
	})

	if r.Determined {
		t.Error("no paths must produce an undetermined result")
	}
	want := "no path found at any of the 1 sizes probed"
	if r.Reason != want {
		t.Errorf("Reason = %q, want %q", r.Reason, want)
	}
	if len(r.Evidence) == 0 {
		t.Error("expected at least one evidence entry explaining the failure")
	} else {
		e := r.Evidence[0]
		if !strings.Contains(e.Observed, "probed 1 sizes, no path found") {
			t.Errorf("Evidence[0].Observed = %q, want it to contain 'probed 1 sizes, no path found'", e.Observed)
		}
		if e.Source == "" {
			t.Error("Evidence[0].Source is blank — a metric must name its data source")
		}
		if e.ObservedAt.IsZero() {
			t.Error("Evidence[0].ObservedAt is zero — a metric must timestamp its observation")
		}
	}
}

func TestDepthMetricDescriptorIsValid(t *testing.T) {
	d := DepthMetric{}.Describe()
	if err := d.Validate(); err != nil {
		t.Errorf("depth metric descriptor: %v", err)
	}
}

// structural undetermined reasons ----------------------------------------------
//
// GHSC is DERIVATIVE — every path runs through NGNC — and KESC is NO-MARKET.
// Before this coverage existed, every book metric fetched the direct order
// book for either, got the same empty response an idle-but-real market would
// return, and reported the same generic "order book is empty" reason either
// way. These tests pin that the three causes — structural absence,
// empty-market-but-real, and fetch failure — are told apart. See GitHub issue
// #105 / docs/backlog.md.

// TestBookMetricsDistinguishNoMarketFromEmptyBook covers the case a naive
// order-book fetch cannot distinguish on its own: NO-MARKET must be reported
// as a structural fact, not the market fact an idle-but-real book gets, and
// it must be reported without ever calling the DEX client — mutate the code
// to fetch anyway and TestNoMarketNeverFetchesTheBook below catches it.
func TestBookMetricsDistinguishNoMarketFromEmptyBook(t *testing.T) {
	poisoned := &dex.Client{HorizonURL: "http://127.0.0.1:1"} // must never be dialed

	subject := Subject{Send: asset.USDC(), Receive: asset.KESC(), Integrity: "NO-MARKET"}

	cases := []struct {
		name string
		run  func() MetricResult
	}{
		{"spread", func() MetricResult { return RunMetric(ctx(), SpreadMetric{DEX: poisoned}, subject) }},
		{"concentration", func() MetricResult { return RunMetric(ctx(), ConcentrationMetric{DEX: poisoned}, subject) }},
		{"depth.observed", func() MetricResult { return RunMetric(ctx(), DepthMetric{DEX: poisoned}, subject) }},
		{"depth.executable", func() MetricResult {
			return RunMetric(ctx(), depthExecutable{DepthMetric{DEX: poisoned}}, subject)
		}},
		{"price-impact", func() MetricResult { return RunMetric(ctx(), PriceImpactMetric{DEX: poisoned}, subject) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.run()
			if r.Determined {
				t.Fatal("a NO-MARKET corridor must never produce a determined result")
			}
			if !strings.Contains(r.Reason, "NO-MARKET") {
				t.Errorf("reason = %q, want it to name NO-MARKET as a structural fact", r.Reason)
			}
			if strings.Contains(r.Reason, "empty") {
				t.Errorf("reason = %q, reads as a market fact (an empty book) rather than "+
					"the structural fact this corridor actually has", r.Reason)
			}
		})
	}
}

// depthExecutable adapts DepthMetric.RunExecutable to the Metric interface
// so it can go through RunMetric like every other case in the table above.
type depthExecutable struct{ DepthMetric }

func (m depthExecutable) Describe() Descriptor {
	return Descriptor{
		ID: "depth.executable", Title: "executable depth",
		CanDetermine: "see DepthMetric", CannotDetermine: "see DepthMetric",
	}
}
func (m depthExecutable) Run(ctx context.Context, s Subject) MetricResult {
	return m.RunExecutable(ctx, s)
}

// TestBookMetricsDistinguishDerivativeFromEmptyBook covers the other
// structural case: a DERIVATIVE corridor with no Underlying pair supplied
// must report the dependency, not a bare empty book. The order-book metrics
// (spread, concentration, depth.observed) fetch nothing here, because there
// is nothing to fetch — the whole point of DERIVATIVE. depth.executable and
// price-impact are pathfinding metrics, not order-book metrics: a DERIVATIVE
// corridor prices normally through its intermediate asset, so they are
// exercised separately below rather than asserted structural here.
func TestBookMetricsDistinguishDerivativeFromEmptyBook(t *testing.T) {
	poisoned := &dex.Client{HorizonURL: "http://127.0.0.1:1"}
	subject := Subject{Send: asset.USDC(), Receive: asset.GHSC(), Integrity: "DERIVATIVE"}

	cases := []struct {
		name string
		run  func() MetricResult
	}{
		{"spread", func() MetricResult { return RunMetric(ctx(), SpreadMetric{DEX: poisoned}, subject) }},
		{"concentration", func() MetricResult { return RunMetric(ctx(), ConcentrationMetric{DEX: poisoned}, subject) }},
		{"depth.observed", func() MetricResult { return RunMetric(ctx(), DepthMetric{DEX: poisoned}, subject) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.run()
			if r.Determined {
				t.Fatal("a DERIVATIVE corridor with no underlying pair must never produce a determined result")
			}
			if !strings.Contains(r.Reason, "DERIVATIVE") {
				t.Errorf("reason = %q, want it to name DERIVATIVE as a structural fact", r.Reason)
			}
		})
	}
}

// TestBookMetricSubstitutesUnderlyingPairExplicitly covers the DERIVATIVE
// case where the caller does have a legitimate pair to measure instead — the
// substitution must be a determined result against the underlying book, and
// it must say so in its evidence, never silently.
func TestBookMetricSubstitutesUnderlyingPairExplicitly(t *testing.T) {
	m := loadOrderBookSnapshot(t, "xlm-ngnc-orderbook")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	subject := Subject{
		Send: asset.Native(), Receive: asset.GHSC(),
		Integrity: "DERIVATIVE", Underlying: asset.NGNC(),
	}

	r := RunMetric(ctx(), SpreadMetric{DEX: c}, subject)
	if !r.Determined {
		t.Fatalf("substituting a real underlying book should determine a value, got: %s", r.Reason)
	}
	if len(r.Evidence) == 0 {
		t.Fatal("no evidence recorded")
	}
	src := r.Evidence[0].Source
	if !strings.Contains(src, "XLM/NGNC") {
		t.Errorf("evidence source %q does not name the pair actually measured", src)
	}
	if !strings.Contains(src, "substituted") || !strings.Contains(src, "GHSC") {
		t.Errorf("evidence source %q does not say the substitution happened, or for which "+
			"corridor — a measurement on a different pair than requested must say so explicitly, "+
			"never silently", src)
	}
}

// TestNoMarketNeverFetchesTheBook is the mutation guard for the "without
// ever calling the DEX client" claim above: a NO-MARKET subject pointed at
// an address nothing listens on must still return promptly and
// undetermined, which is only possible if the structural check runs before
// any network call.
func TestNoMarketNeverFetchesTheBook(t *testing.T) {
	poisoned := &dex.Client{HorizonURL: "http://127.0.0.1:1"}
	subject := Subject{Send: asset.USDC(), Receive: asset.KESC(), Integrity: "NO-MARKET"}

	start := time.Now()
	r := RunMetric(ctx(), SpreadMetric{DEX: poisoned}, subject)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %s; a structural short-circuit should return immediately, "+
			"without attempting to dial %s", elapsed, poisoned.HorizonURL)
	}
	if r.Determined {
		t.Fatal("expected an undetermined result")
	}
}

// TestDerivativeCorridorPricesNormallyThroughPathfinding pins the other half
// of the DERIVATIVE distinction: depth.executable and price-impact measure
// via pathfinding, not an order book, and a DERIVATIVE corridor is exactly
// the case pathfinding across an intermediate asset succeeds at. Neither
// metric should ever report GHSC's dependency on NGNC as an obstacle.
func TestDerivativeCorridorPricesNormallyThroughPathfinding(t *testing.T) {
	m := loadOrderBookSnapshot(t, "usdc-ngnc-strictsend")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	// This subject's Send/Receive do not match the snapshot's real GHSC
	// corridor — the fixture only records a USDC/NGNC strict-send path —
	// but the point here is purely that Integrity: DERIVATIVE does not by
	// itself short-circuit a pathfinding metric the way it does the
	// order-book metrics above.
	subject := Subject{Send: asset.USDC(), Receive: asset.NGNC(), Integrity: "DERIVATIVE"}

	depth := DepthMetric{DEX: c, Sizes: []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(100)}}
	if r := depth.RunExecutable(ctx(), subject); !r.Determined {
		t.Errorf("depth.executable on a DERIVATIVE corridor should still price via "+
			"pathfinding, got undetermined: %s", r.Reason)
	}

	impact := PriceImpactMetric{DEX: c, ProbeSize: decimal.NewFromInt(1), FullSize: decimal.NewFromInt(100)}
	if r := RunMetric(ctx(), impact, subject); !r.Determined {
		t.Errorf("price-impact on a DERIVATIVE corridor should still price via "+
			"pathfinding, got undetermined: %s", r.Reason)
	}
}

// concentration metric ------------------------------------------------------------
//
// GitHub issue #106: concentration.liquidity had no dedicated coverage at
// all before this section.

func TestConcentrationMetricFromRecordedBook(t *testing.T) {
	m := loadOrderBookSnapshot(t, "xlm-ngnc-orderbook")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	r := RunMetric(ctx(), ConcentrationMetric{DEX: c}, Subject{Send: asset.Native(), Receive: asset.NGNC()})

	if !r.Determined {
		t.Fatalf("concentration metric undetermined: %s", r.Reason)
	}
	if r.Unit != UnitRatio {
		t.Errorf("unit = %s, want ratio", r.Unit)
	}
	if !r.Value.IsPositive() || r.Value.GreaterThan(decimal.NewFromInt(1)) {
		t.Errorf("HHI = %s, want a value in (0, 1]", r.Value)
	}
	if len(r.Evidence) == 0 {
		t.Error("no evidence recorded")
	}
}

func TestConcentrationMetricEmptyBook(t *testing.T) {
	m := loadOrderBookSnapshot(t, "xlm-ngnc-orderbook-empty")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	r := RunMetric(ctx(), ConcentrationMetric{DEX: c}, Subject{Send: asset.Native(), Receive: asset.NGNC()})

	if r.Determined {
		t.Error("empty book must produce an undetermined result, not an HHI of zero")
	}
	if !strings.Contains(r.Reason, "empty") {
		t.Errorf("reason = %q, want it to name the empty book", r.Reason)
	}
}

func TestConcentrationMetricNilDEX(t *testing.T) {
	r := RunMetric(ctx(), ConcentrationMetric{}, Subject{Send: asset.Native(), Receive: asset.NGNC()})
	if r.Determined {
		t.Error("nil DEX client must produce an undetermined result")
	}
}

func TestConcentrationMetricDescriptorIsValid(t *testing.T) {
	d := ConcentrationMetric{}.Describe()
	if err := d.Validate(); err != nil {
		t.Errorf("concentration metric descriptor: %v", err)
	}
}

// price impact metric ------------------------------------------------------------
//
// GitHub issue #106: price-impact.size had no fixture-backed coverage at
// all before this section — the existing usdc-ngnc-strictsend snapshot
// already records both a probe (1 USDC) and a full (100 USDC) strict-send
// response, which is exactly the shape this metric needs and nothing new
// had to be captured to exercise it.

func TestPriceImpactMetricFromRecordedPaths(t *testing.T) {
	m := loadOrderBookSnapshot(t, "usdc-ngnc-strictsend")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	impact := PriceImpactMetric{
		DEX:       c,
		ProbeSize: decimal.NewFromInt(1),
		FullSize:  decimal.NewFromInt(100),
	}
	r := RunMetric(ctx(), impact, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if !r.Determined {
		t.Fatalf("price impact metric undetermined: %s", r.Reason)
	}
	if r.Unit != UnitPercent {
		t.Errorf("unit = %s, want percent", r.Unit)
	}
	if r.Value.IsNegative() {
		t.Errorf("impact = %s, must never be reported negative (see the clamp in Run)", r.Value)
	}
	if len(r.Evidence) == 0 {
		t.Error("no evidence recorded")
	}
	if r.Summary == "" {
		t.Error("summary is empty")
	}
}

func TestPriceImpactMetricNoPathAtProbeSize(t *testing.T) {
	m := loadOrderBookSnapshot(t, "usdc-ngnc-strictsend-empty")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	impact := PriceImpactMetric{DEX: c, ProbeSize: decimal.NewFromInt(1), FullSize: decimal.NewFromInt(100)}
	r := RunMetric(ctx(), impact, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

	if r.Determined {
		t.Error("no path at either size must produce an undetermined result")
	}
}

func TestPriceImpactMetricNilDEX(t *testing.T) {
	impact := PriceImpactMetric{}
	r := RunMetric(ctx(), impact, Subject{Send: asset.USDC(), Receive: asset.NGNC()})
	if r.Determined {
		t.Error("nil DEX client must produce an undetermined result")
	}
}

func TestPriceImpactMetricDescriptorIsValid(t *testing.T) {
	d := PriceImpactMetric{}.Describe()
	if err := d.Validate(); err != nil {
		t.Errorf("price impact metric descriptor: %v", err)
	}
}

// deviation metric --------------------------------------------------------------
//
// See GitHub issue #103: the gap between the direct book's implied mid and
// the independent reference mid, as a metric separate from route loss.

func TestDeviationMetricFromRecordedBook(t *testing.T) {
	m := loadOrderBookSnapshot(t, "xlm-ngnc-orderbook")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	dev := DeviationMetric{
		DEX: c,
		Reference: refrate.Rate{
			Base: "USD", Quote: "NGN",
			Mid:    decimal.RequireFromString("1350.2568"),
			Source: "exchangerate-api",
			AsOf:   time.Now().UTC(),
		},
	}
	r := RunMetric(ctx(), dev, Subject{Send: asset.Native(), Receive: asset.NGNC()})

	if !r.Determined {
		t.Fatalf("deviation metric undetermined: %s", r.Reason)
	}
	if r.Unit != UnitPercent {
		t.Errorf("unit = %s, want percent", r.Unit)
	}
	if len(r.Evidence) != 2 {
		t.Fatalf("evidence entries = %d, want 2 (book and reference)", len(r.Evidence))
	}
	for _, e := range r.Evidence {
		if e.Source == "" || e.Observed == "" {
			t.Errorf("evidence %+v is missing a source or an observation", e)
		}
	}
	if r.Summary == "" {
		t.Error("summary is empty")
	}
}

func TestDeviationMetricSignReflectsDirection(t *testing.T) {
	m := loadOrderBookSnapshot(t, "xlm-ngnc-orderbook")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}
	subject := Subject{Send: asset.Native(), Receive: asset.NGNC()}

	// The book mid does not move between these two runs; only the reference
	// does. A reference far below the book mid must deviate positive, and
	// one far above must deviate negative — otherwise the sign is backwards
	// or the arithmetic has been mangled.
	low := RunMetric(ctx(), DeviationMetric{DEX: c, Reference: refrate.Rate{
		Base: "USD", Quote: "NGN", Mid: decimal.RequireFromString("1"), Source: "static", AsOf: time.Now().UTC(),
	}}, subject)
	high := RunMetric(ctx(), DeviationMetric{DEX: c, Reference: refrate.Rate{
		Base: "USD", Quote: "NGN", Mid: decimal.RequireFromString("100000000"), Source: "static", AsOf: time.Now().UTC(),
	}}, subject)

	if !low.Determined || !high.Determined {
		t.Fatalf("expected both determined, got low.Determined=%v (%s) high.Determined=%v (%s)",
			low.Determined, low.Reason, high.Determined, high.Reason)
	}
	if !low.Value.IsPositive() {
		t.Errorf("a reference far below the book mid should deviate positive, got %s", low.Value)
	}
	if !high.Value.IsNegative() {
		t.Errorf("a reference far above the book mid should deviate negative, got %s", high.Value)
	}
}

func TestDeviationMetricEmptyBook(t *testing.T) {
	m := loadOrderBookSnapshot(t, "xlm-ngnc-orderbook-empty")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	dev := DeviationMetric{
		DEX: c,
		Reference: refrate.Rate{
			Base: "USD", Quote: "NGN", Mid: decimal.RequireFromString("1350.2568"), Source: "exchangerate-api",
		},
	}
	r := RunMetric(ctx(), dev, Subject{Send: asset.Native(), Receive: asset.NGNC()})

	if r.Determined {
		t.Error("empty book must produce an undetermined result, not a zero deviation")
	}
	if !strings.Contains(r.Reason, "empty") {
		t.Errorf("reason = %q, want it to name the empty book", r.Reason)
	}
}

func TestDeviationMetricOneSidedBook(t *testing.T) {
	m := loadOrderBookSnapshot(t, "xlm-ngnc-orderbook-onesided")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	dev := DeviationMetric{
		DEX: c,
		Reference: refrate.Rate{
			Base: "USD", Quote: "NGN", Mid: decimal.RequireFromString("1350.2568"), Source: "exchangerate-api",
		},
	}
	r := RunMetric(ctx(), dev, Subject{Send: asset.Native(), Receive: asset.NGNC()})

	if r.Determined {
		t.Error("one-sided book must produce an undetermined result")
	}
	if !strings.Contains(r.Reason, "nobody is buying") {
		t.Errorf("reason = %q, want it to name the missing side", r.Reason)
	}
}

func TestDeviationMetricNoDirectPair(t *testing.T) {
	// GHSC: DERIVATIVE, no Underlying supplied — never even attempts a fetch.
	dev := DeviationMetric{
		DEX: &dex.Client{HorizonURL: "http://127.0.0.1:1"},
		Reference: refrate.Rate{
			Base: "USD", Quote: "GHS", Mid: decimal.RequireFromString("11.09"), Source: "exchangerate-api",
		},
	}
	r := RunMetric(ctx(), dev, Subject{Send: asset.USDC(), Receive: asset.GHSC(), Integrity: "DERIVATIVE"})

	if r.Determined {
		t.Error("a DERIVATIVE corridor with no underlying pair must not produce a determined result")
	}
	if !strings.Contains(r.Reason, "DERIVATIVE") {
		t.Errorf("reason = %q, want it to name DERIVATIVE as a structural fact", r.Reason)
	}
}

func TestDeviationMetricUnscorableReference(t *testing.T) {
	m := loadOrderBookSnapshot(t, "xlm-ngnc-orderbook")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	dev := DeviationMetric{
		DEX: c,
		Reference: refrate.Rate{
			Base: "USD", Quote: "NGN",
			Mid:       decimal.RequireFromString("1350.2568"),
			Source:    "exchangerate-api",
			Agreement: refrate.AgreementMalfunction,
			Note:      "providers differ by 400%, further apart than either can be trusted",
		},
	}
	r := RunMetric(ctx(), dev, Subject{Send: asset.Native(), Receive: asset.NGNC()})

	if r.Determined {
		t.Error("an unscorable reference must not produce a determined result")
	}
	if !strings.Contains(r.Reason, "cross-check") {
		t.Errorf("reason = %q, want it to name the cross-check as the cause", r.Reason)
	}
}

func TestDeviationMetricNoReference(t *testing.T) {
	m := loadOrderBookSnapshot(t, "xlm-ngnc-orderbook")
	c := &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}

	dev := DeviationMetric{DEX: c, ReferenceUnavailable: "exchangerate-api: rate limited"}
	r := RunMetric(ctx(), dev, Subject{Send: asset.Native(), Receive: asset.NGNC()})

	if r.Determined {
		t.Error("no reference rate must not produce a determined result")
	}
	if !strings.Contains(r.Reason, "rate limited") {
		t.Errorf("reason = %q, want it to carry the caller's unavailability reason", r.Reason)
	}
}

func TestDeviationMetricNilDEX(t *testing.T) {
	dev := DeviationMetric{Reference: refrate.Rate{
		Base: "USD", Quote: "NGN", Mid: decimal.RequireFromString("1350"), Source: "static",
	}}
	r := RunMetric(ctx(), dev, Subject{Send: asset.Native(), Receive: asset.NGNC()})
	if r.Determined {
		t.Error("nil DEX client must produce an undetermined result")
	}
}

func TestDeviationMetricDescriptorIsValid(t *testing.T) {
	d := DeviationMetric{}.Describe()
	if err := d.Validate(); err != nil {
		t.Errorf("deviation metric descriptor: %v", err)
	}
}

// RunMetric validation tests ---------------------------------------------------

// panicMetric is a metric whose Describe method panics.
type panicMetric struct{}

func (panicMetric) Describe() Descriptor { panic("boom") }
func (panicMetric) Run(context.Context, Subject) MetricResult {
	return MetricValue(Descriptor{ID: "never"}, Subject{}, decimal.Zero, UnitPercent, "never reached")
}

func TestRunMetricDescribePanic(t *testing.T) {
	r := RunMetric(ctx(), panicMetric{}, Subject{Domain: "example.test"})
	if r.Determined {
		t.Error("a metric whose Describe panics must produce an undetermined result")
	}
	if !strings.Contains(r.Reason, "panicked") {
		t.Errorf("reason = %q, want it to mention the panic", r.Reason)
	}
}

// evidenceBlankSource is a metric that returns determined with blank Source.
type evidenceBlankSource struct{}

func (evidenceBlankSource) Describe() Descriptor {
	return Descriptor{ID: "test.blank-source", Title: "blank source", CanDetermine: "always", CannotDetermine: "never"}
}
func (evidenceBlankSource) Run(context.Context, Subject) MetricResult {
	return MetricValue(
		Descriptor{ID: "test.blank-source"},
		Subject{},
		decimal.NewFromInt(42), UnitCount, "ok",
		Evidence{Source: "", Observed: "something", ObservedAt: time.Now().UTC()},
	)
}

func TestRunMetricBlankSourceProducesUndetermined(t *testing.T) {
	r := RunMetric(ctx(), evidenceBlankSource{}, Subject{})
	if r.Determined {
		t.Error("blank Source must produce an undetermined result")
	}
	if !strings.Contains(r.Reason, "Source") {
		t.Errorf("reason = %q, want it to mention Source", r.Reason)
	}
}

// evidenceBlankObserved is a metric that returns determined with blank Observed.
type evidenceBlankObserved struct{}

func (evidenceBlankObserved) Describe() Descriptor {
	return Descriptor{ID: "test.blank-observed", Title: "blank observed", CanDetermine: "always", CannotDetermine: "never"}
}
func (evidenceBlankObserved) Run(context.Context, Subject) MetricResult {
	return MetricValue(
		Descriptor{ID: "test.blank-observed"},
		Subject{},
		decimal.NewFromInt(42), UnitCount, "ok",
		Evidence{Source: "somewhere", Observed: "", ObservedAt: time.Now().UTC()},
	)
}

func TestRunMetricBlankObservedProducesUndetermined(t *testing.T) {
	r := RunMetric(ctx(), evidenceBlankObserved{}, Subject{})
	if r.Determined {
		t.Error("blank Observed must produce an undetermined result")
	}
	if !strings.Contains(r.Reason, "Observed") {
		t.Errorf("reason = %q, want it to mention Observed", r.Reason)
	}
}

// evidenceZeroTime is a metric that returns determined with zero ObservedAt.
type evidenceZeroTime struct{}

func (evidenceZeroTime) Describe() Descriptor {
	return Descriptor{ID: "test.zero-time", Title: "zero time", CanDetermine: "always", CannotDetermine: "never"}
}
func (evidenceZeroTime) Run(context.Context, Subject) MetricResult {
	return MetricValue(
		Descriptor{ID: "test.zero-time"},
		Subject{},
		decimal.NewFromInt(42), UnitCount, "ok",
		Evidence{Source: "somewhere", Observed: "something", ObservedAt: time.Time{}},
	)
}

func TestRunMetricZeroObservedAtProducesUndetermined(t *testing.T) {
	r := RunMetric(ctx(), evidenceZeroTime{}, Subject{})
	if r.Determined {
		t.Error("zero ObservedAt must produce an undetermined result")
	}
	if !strings.Contains(r.Reason, "ObservedAt") {
		t.Errorf("reason = %q, want it to mention ObservedAt", r.Reason)
	}
}

// evidenceValid is a metric with perfectly valid evidence — ensures no false rejections.
type evidenceValid struct{}

func (evidenceValid) Describe() Descriptor {
	return Descriptor{ID: "test.valid", Title: "valid", CanDetermine: "always", CannotDetermine: "never"}
}
func (evidenceValid) Run(context.Context, Subject) MetricResult {
	return MetricValue(
		Descriptor{ID: "test.valid"},
		Subject{},
		decimal.NewFromInt(42), UnitCount, "ok",
		Evidence{Source: "somewhere", Observed: "something", ObservedAt: time.Now().UTC()},
	)
}

func TestRunMetricValidEvidence(t *testing.T) {
	r := RunMetric(ctx(), evidenceValid{}, Subject{})
	if !r.Determined {
		t.Errorf("valid evidence should produce a determined result, got reason: %s", r.Reason)
	}
}

type recordedResponse struct {
	status int
	body   string
	err    error
}

type recordedTransport map[string]recordedResponse

func (t recordedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r, ok := t[req.Method+" "+req.URL.String()]
	if !ok {
		return nil, fmt.Errorf("unrecorded request: %s %s", req.Method, req.URL)
	}
	if r.err != nil {
		return nil, r.err
	}
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return &http.Response{
		StatusCode: r.status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Request:    req,
	}, nil
}

func TestHomeDomainRoundTrip(t *testing.T) {
	const (
		issuer = "GISSUER"
		code   = "TOK"
		domain = "issuer.example"
	)
	accountURL := "https://horizon.example/accounts/" + issuer
	tomlURL := anchor.TOMLURL(domain)

	cases := []struct {
		name       string
		account    string
		toml       string
		tomlStatus int
		err        error
		determined bool
		passed     bool
		contains   string
	}{
		{
			name:       "matching account and document",
			account:    `{"home_domain":"` + domain + `"}`,
			toml:       `[[CURRENCIES]]` + "\ncode=\"" + code + "\"\nissuer=\"" + issuer + "\"\n",
			determined: true, passed: true, contains: "both identify",
		},
		{
			name:       "document lists code for another issuer",
			account:    `{"home_domain":"` + domain + `"}`,
			toml:       `[[CURRENCIES]]` + "\ncode=\"" + code + "\"\nissuer=\"GOTHER\"\n",
			determined: true, passed: false, contains: "no [[CURRENCIES]] entry matching both code and issuer",
		},
		{
			name:       "account declares no home domain",
			account:    `{}`,
			determined: false, contains: "declares no home_domain",
		},
		{
			name:       "Horizon is unavailable",
			err:        fmt.Errorf("offline"),
			determined: false, contains: "Horizon was unreachable",
		},
		{
			name:       "stellar toml is unavailable",
			account:    `{"home_domain":"` + domain + `"}`,
			tomlStatus: http.StatusNotFound,
			determined: false, contains: "document could not be fetched",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			responses := recordedTransport{}
			if tc.err != nil {
				responses[http.MethodGet+" "+accountURL] = recordedResponse{err: tc.err}
			} else {
				responses[http.MethodGet+" "+accountURL] = recordedResponse{body: tc.account}
			}
			if tc.toml != "" || tc.tomlStatus != 0 {
				responses[http.MethodGet+" "+tomlURL] = recordedResponse{status: tc.tomlStatus, body: tc.toml}
			}

			check := HomeDomainRoundTrip{
				HorizonURL: "https://horizon.example",
				HTTPClient: &http.Client{Transport: responses},
			}
			r := Run(ctx(), check, Subject{Asset: asset.Stellar(code, issuer)})
			if r.Determined != tc.determined {
				t.Fatalf("Determined = %v, want %v: %s", r.Determined, tc.determined, r.Summary)
			}
			if tc.determined && r.Passed != tc.passed {
				t.Errorf("Passed = %v, want %v: %s", r.Passed, tc.passed, r.Summary)
			}
			if !strings.Contains(r.Summary+" "+r.Reason, tc.contains) {
				t.Errorf("result %q does not mention %q", r.Summary+" "+r.Reason, tc.contains)
			}
			if len(r.Evidence) == 0 {
				t.Error("result must record account evidence")
			}
			if tc.determined && len(r.Evidence) != 2 {
				t.Fatalf("determined result has %d evidence entries, want account and document", len(r.Evidence))
			}
			if tc.determined {
				if !strings.Contains(r.Evidence[0].Source, "/accounts/"+issuer) {
					t.Errorf("account evidence source = %q, want issuer account", r.Evidence[0].Source)
				}
				if !strings.Contains(r.Evidence[1].Source, tomlURL) {
					t.Errorf("document evidence source = %q, want %s", r.Evidence[1].Source, tomlURL)
				}
			}
		})
	}
}
