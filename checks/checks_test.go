package checks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/anchor"
	"github.com/Wayfare-labs/wayfare/asset"
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

	for _, c := range []Check{AnchorAssetISO4217{}, IssuerAuthFlags{}, SEP10EndpointResponds{}} {
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
