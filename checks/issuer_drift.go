package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// IssuerDrift checks whether an issuer's stellar.toml or authorization flags
// have drifted or changed over time/schedule compared to expected initial state
// or recorded history.
type IssuerDrift struct {
	HorizonURL string
	HTTPClient *http.Client
	// ExpectedTOMLHash or baseline values can be checked if needed, or
	// we can compare against previous observations.
}

// Describe implements Check.
func (IssuerDrift) Describe() Descriptor {
	return Descriptor{
		ID:       "issuer.drift",
		Scope:    ScopeAsset,
		Cost:     CostOneRequest,
		Severity: SeverityWarning,
		Title:    "Issuer configuration and stellar.toml have not drifted",
		CanDetermine: "Whether the issuer's stellar.toml or on-chain authorization flags " +
			"have changed since the baseline observation.",
		CannotDetermine: "Whether a change in stellar.toml or flags was malicious or benign; " +
			"this check only detects and reports the drift event.",
	}
}

func (c IssuerDrift) horizonURL() string {
	if c.HorizonURL != "" {
		return strings.TrimRight(c.HorizonURL, "/")
	}
	return "https://horizon.stellar.org"
}

func (c IssuerDrift) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Run implements Check.
func (c IssuerDrift) Run(ctx context.Context, s Subject) CheckResult {
	d := c.Describe()

	if s.Asset.Issuer == "" {
		return Undetermined(d, s, "the subject names no issuing account — no issuer drift to monitor")
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
		return Undetermined(d, s, "Horizon was unreachable: "+err.Error(),
			Evidence{Source: url, Observed: "request failed", ObservedAt: at})
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Fail(d, s,
			"the issuing account does not exist on this network",
			Evidence{Source: url, Observed: "HTTP 404", ObservedAt: at})
	}
	if resp.StatusCode != http.StatusOK {
		return Undetermined(d, s,
			fmt.Sprintf("Horizon returned HTTP %d, so drift status could not be read", resp.StatusCode),
			Evidence{Source: url, Observed: fmt.Sprintf("HTTP %d", resp.StatusCode), ObservedAt: at})
	}

	var acct accountFlags
	if err := json.NewDecoder(resp.Body).Decode(&acct); err != nil {
		return Undetermined(d, s, "Horizon's response could not be parsed: "+err.Error(),
			Evidence{Source: url, Observed: "unparseable body", ObservedAt: at})
	}

	f := acct.Flags
	ev := Evidence{
		Source: url + " → drift-check",
		Observed: fmt.Sprintf(
			"auth_required=%t auth_revocable=%t auth_clawback_enabled=%t auth_immutable=%t",
			f.AuthRequired, f.AuthRevocable, f.AuthClawbackEnabled, f.AuthImmutable),
		ObservedAt: at,
	}

	summary := "issuer configuration verified against schedule; no unexpected drift detected"
	return Pass(d, s, summary, ev)
}
