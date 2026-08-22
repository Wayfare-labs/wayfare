package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// IssuerAuthFlags reports the control an issuer retains over balances of its
// asset, read from the issuing account's flags on-chain.
//
// # Why this is the most consequential check here
//
// Every other signal Wayfare produces is about price. This one is about
// whether the money stays yours after it arrives.
//
//	auth_required           the issuer must approve a trustline before you can
//	                        hold the asset at all
//	auth_revocable          the issuer can freeze an existing balance, making
//	                        it unspendable
//	auth_clawback_enabled   the issuer can take the asset back out of your
//	                        account, without your involvement
//
// A corridor can price perfectly and still be unusable if the destination
// asset can be clawed back on arrival. That fact belongs next to the rate, not
// buried in a block explorer.
//
// # What it can determine
//
// Which flags are set on the issuing account at the moment of observation.
// This is a fact on the ledger, not a claim by anyone.
//
// # What it cannot determine
//
// Whether the issuer will ever exercise these powers, or has in the past.
// Flags are capability, not conduct — a regulated issuer with clawback enabled
// may be entirely trustworthy, and a scrupulous one may still be compelled.
//
// Nor is the absence of flags a guarantee of anything: flags can be set later,
// so a passing result describes the account now and says nothing about
// tomorrow. Detecting an issuer that *changes* its flags requires history,
// which is a different question the run store makes answerable over time.
type IssuerAuthFlags struct {
	// HorizonURL defaults to Horizon mainnet.
	HorizonURL string

	// HTTPClient is the seam. Leaving it nil uses a client with a short
	// timeout; a snapshot replayer here makes the check offline-testable.
	HTTPClient *http.Client
}

// Describe implements Check.
func (IssuerAuthFlags) Describe() Descriptor {
	return Descriptor{
		ID:       "issuer.auth-flags",
		Scope:    ScopeAsset,
		Cost:     CostOneRequest,
		Severity: SeverityCritical,
		Title:    "Issuer cannot freeze or claw back the asset",
		CanDetermine: "Which of auth_required, auth_revocable and auth_clawback_enabled " +
			"are set on the issuing account right now, read from the ledger.",
		CannotDetermine: "Whether the issuer will ever use these powers, or has before. " +
			"Flags are capability, not conduct. A passing result also describes only the " +
			"present: flags can be set at any time afterwards.",
	}
}

func (c IssuerAuthFlags) horizonURL() string {
	if c.HorizonURL != "" {
		return strings.TrimRight(c.HorizonURL, "/")
	}
	return "https://horizon.stellar.org"
}

func (c IssuerAuthFlags) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// accountFlags is the subset of Horizon's account resource this reads.
type accountFlags struct {
	Flags struct {
		AuthRequired        bool `json:"auth_required"`
		AuthRevocable       bool `json:"auth_revocable"`
		AuthImmutable       bool `json:"auth_immutable"`
		AuthClawbackEnabled bool `json:"auth_clawback_enabled"`
	} `json:"flags"`
}

// Run implements Check.
func (c IssuerAuthFlags) Run(ctx context.Context, s Subject) CheckResult {
	d := c.Describe()

	if s.Asset.Issuer == "" {
		return Undetermined(d, s,
			"the subject names no issuing account — a native or fiat asset has no issuer flags to read")
	}

	url := c.horizonURL() + "/accounts/" + s.Asset.Issuer
	at := time.Now().UTC()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Undetermined(d, s, "could not build the request: "+err.Error())
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client().Do(req)
	if err != nil {
		// An unreachable Horizon says nothing about the issuer. Reporting
		// this as a failure would blame the subject for a network problem.
		return Undetermined(d, s, "Horizon was unreachable: "+err.Error(),
			Evidence{Source: url, Observed: "request failed", ObservedAt: at})
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// A genuine finding rather than an unknown: the ledger was asked
		// and answered that no such account exists. An asset whose issuer
		// does not exist cannot be issued.
		return Fail(d, s,
			"the issuing account does not exist on this network",
			Evidence{Source: url, Observed: "HTTP 404", ObservedAt: at})
	}
	if resp.StatusCode != http.StatusOK {
		return Undetermined(d, s,
			fmt.Sprintf("Horizon returned HTTP %d, so the flags could not be read", resp.StatusCode),
			Evidence{Source: url, Observed: fmt.Sprintf("HTTP %d", resp.StatusCode), ObservedAt: at})
	}

	var acct accountFlags
	if err := json.NewDecoder(resp.Body).Decode(&acct); err != nil {
		return Undetermined(d, s, "Horizon's response could not be parsed: "+err.Error(),
			Evidence{Source: url, Observed: "unparseable body", ObservedAt: at})
	}

	f := acct.Flags
	ev := Evidence{
		Source: url + " → flags",
		Observed: fmt.Sprintf(
			"auth_required=%t auth_revocable=%t auth_clawback_enabled=%t auth_immutable=%t",
			f.AuthRequired, f.AuthRevocable, f.AuthClawbackEnabled, f.AuthImmutable),
		ObservedAt: at,
	}

	// Only the two powers that can take an asset away after you hold it
	// count as a failure. auth_required gates acquiring the asset, which is
	// friction rather than risk to a balance already held, so it is
	// reported without failing the check.
	var powers []string
	if f.AuthClawbackEnabled {
		powers = append(powers, "claw the asset back out of your account (auth_clawback_enabled)")
	}
	if f.AuthRevocable {
		powers = append(powers, "freeze your balance (auth_revocable)")
	}

	if len(powers) > 0 {
		summary := "the issuer can " + strings.Join(powers, ", and ")
		if f.AuthRequired {
			summary += "; it also gates trustlines (auth_required)"
		}
		return Fail(d, s, summary, ev)
	}

	summary := "the issuer can neither freeze nor claw back this asset"
	if f.AuthRequired {
		summary += ", though it must approve a trustline before you can hold it (auth_required)"
	}
	if f.AuthImmutable {
		summary += "; the flags are immutable, so this cannot change"
	}
	return Pass(d, s, summary, ev)
}
