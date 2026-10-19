package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// IssuerFlagImmutability reports whether an issuer has permanently disabled
// changes to its authorization flags. This is capability context, not a
// finding about whether the issuer has behaved responsibly.
type IssuerFlagImmutability struct {
	HorizonURL string
	HTTPClient *http.Client
}

func (IssuerFlagImmutability) Describe() Descriptor {
	return Descriptor{
		ID:       "issuer.auth-immutable",
		Scope:    ScopeAsset,
		Cost:     CostOneRequest,
		Severity: SeverityInfo,
		Title:    "Issuer authorization flags cannot change",
		CanDetermine: "Whether the issuing account has set auth_immutable, read directly " +
			"from the Horizon account resource.",
		CannotDetermine: "Whether the issuer will exercise its current powers. A mutable " +
			"account may still be operated responsibly; this check reports capability, " +
			"not conduct.",
	}
}

func (c IssuerFlagImmutability) horizonURL() string {
	if c.HorizonURL != "" {
		return strings.TrimRight(c.HorizonURL, "/")
	}
	return "https://horizon.stellar.org"
}

func (c IssuerFlagImmutability) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (c IssuerFlagImmutability) Run(ctx context.Context, s Subject) CheckResult {
	d := c.Describe()
	if s.Asset.Issuer == "" {
		return Undetermined(d, s, "the subject names no issuing account — there are no issuer flags to read")
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
		return Fail(d, s, "the issuing account does not exist on this network",
			Evidence{Source: url, Observed: "HTTP 404", ObservedAt: at})
	}
	if resp.StatusCode != http.StatusOK {
		return Undetermined(d, s,
			fmt.Sprintf("Horizon returned HTTP %d, so immutability could not be read", resp.StatusCode),
			Evidence{Source: url, Observed: fmt.Sprintf("HTTP %d", resp.StatusCode), ObservedAt: at})
	}

	var account struct {
		Flags struct {
			AuthImmutable       bool `json:"auth_immutable"`
			AuthRevocable       bool `json:"auth_revocable"`
			AuthClawbackEnabled bool `json:"auth_clawback_enabled"`
		} `json:"flags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&account); err != nil {
		return Undetermined(d, s, "Horizon's response could not be parsed: "+err.Error(),
			Evidence{Source: url, Observed: "unparseable body", ObservedAt: at})
	}

	f := account.Flags
	restrictive := f.AuthRevocable || f.AuthClawbackEnabled
	immutability := "mutable"
	if f.AuthImmutable {
		immutability = "immutable"
	}
	posture := "clean"
	if restrictive {
		posture = "restrictive"
	}
	ev := Evidence{
		Source:     url + " → flags",
		Observed:   fmt.Sprintf("auth_revocable=%t auth_clawback_enabled=%t auth_immutable=%t", f.AuthRevocable, f.AuthClawbackEnabled, f.AuthImmutable),
		ObservedAt: at,
	}
	summary := fmt.Sprintf("issuer authorization posture is %s and %s", posture, immutability)
	if f.AuthImmutable {
		summary += "; authorization flags cannot be changed"
	} else {
		summary += "; the issuer can still change authorization flags later"
	}
	if f.AuthImmutable {
		return Pass(d, s, summary, ev)
	}
	return Fail(d, s, summary, ev)
}
