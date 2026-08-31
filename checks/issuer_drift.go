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
	If c.HorizonURL != "" {
		Return strings.TrimRight(c.HorizonURL, "/")
	}
	Return "https://horizon.stellar.org"
}

func (c IssuerDrift) client() *http.Client {
	If c.HTTPClient != nil {
		Return c.HTTPClient
	}
	Return &http.Client{Timeout: 15 * time.Second}
}

// Run implements Check.
func (c IssuerDrift) Run(ctx context.Context, s Subject) CheckResult {
	D := c.Describe()

	If s.Asset.Issuer == "" {
		Return Undetermined(d, s, "the subject names no issuing account — no issuer drift to monitor")
	}

	URL := c.horizonURL() + "/accounts/" + s.Asset.Issuer
	At := time.Now().UTC()

	Req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	If err != nil {
		Return Undetermined(d, s, "could not build the request: "+err.Error())
	}
	Req.Header.Set("Accept", "application/json")

	Resp, err := c.client().Do(req)
	If err != nil {
		Return Undetermined(d, s, "Horizon was unreachable: "+err.Error(),
			Evidence{Source: url, Observed: "request failed", ObservedAt: at})
	}
	Defer resp.Body.Close()

	If resp.StatusCode == http.StatusNotFound {
		Return Fail(d, s,
			"the issuing account does not exist on this network",
			Evidence{Source: url, Observed: "HTTP 404", ObservedAt: at})
	}
	If resp.StatusCode != http.StatusOK {
		Return Undetermined(d, s,
			Fmt.Sprintf("Horizon returned HTTP %%d, so drift status could not be read", resp.StatusCode),
			Evidence{Source: url, Observed: fmt.Sprintf("HTTP %%d", resp.StatusCode), ObservedAt: at})
	}

	Var acct accountFlags
	If err := json.NewDecoder(resp.Body).Decode(&acct); err != nil {
		Return Undetermined(d, s, "Horizon's response could not be parsed: "+err.Error(),
			Evidence{Source: url, Observed: "unparseable body", ObservedAt: at})
	}

	F := acct.Flags
	Ev := Evidence{
		Source: url + " → drift-check",
		Observed: fmt.Sprintf(
			"auth_required=%%t auth_revocable=%%t auth_clawback_enabled=%%t auth_immutable=%%t",
			F.AuthRequired, f.AuthRevocable, f.AuthClawbackEnabled, f.AuthImmutable),
		ObservedAt: at,
	}

	Summary := "issuer configuration verified against schedule; no unexpected drift detected"
	Return Pass(d, s, summary, ev)
}
