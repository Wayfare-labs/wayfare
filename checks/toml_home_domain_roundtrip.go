package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wayfare-labs/wayfare/anchor"
)

// HomeDomainRoundTrip checks that an issuer's home_domain and stellar.toml
// make the same asset claim.
type HomeDomainRoundTrip struct {
	HorizonURL string
	HTTPClient *http.Client
	Resolver   *anchor.Resolver
}

func (HomeDomainRoundTrip) Describe() Descriptor {
	return Descriptor{
		ID:       "toml.home-domain-roundtrip",
		Scope:    ScopeAsset,
		Cost:     CostExpensive,
		Severity: SeverityNotice,
		Title:    "Issuer home_domain round-trips to the same stellar.toml asset",
		CanDetermine: "Whether the issuing account declares a home_domain and the " +
			"stellar.toml at that domain lists the same asset code and issuer.",
		CannotDetermine: "Whether an issuer without a home_domain has an off-chain " +
			"association, or whether a network failure reflects a problem with the issuer.",
	}
}

func (c HomeDomainRoundTrip) horizonURL() string {
	if c.HorizonURL != "" {
		return strings.TrimRight(c.HorizonURL, "/")
	}
	return "https://horizon.stellar.org"
}

func (c HomeDomainRoundTrip) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

type accountHomeDomain struct {
	HomeDomain string `json:"home_domain"`
}

func (c HomeDomainRoundTrip) resolver() *anchor.Resolver {
	if c.Resolver != nil {
		return c.Resolver
	}
	return &anchor.Resolver{HTTPClient: c.client()}
}

func (c HomeDomainRoundTrip) Run(ctx context.Context, s Subject) CheckResult {
	d := c.Describe()
	if s.Asset.Issuer == "" {
		return Undetermined(d, s, "the subject names no issuing account, so no home_domain can be read")
	}

	accountURL := c.horizonURL() + "/accounts/" + s.Asset.Issuer
	at := time.Now().UTC()
	accountEvidence := Evidence{Source: accountURL + " -> home_domain", ObservedAt: at}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, accountURL, nil)
	if err != nil {
		return Undetermined(d, s, "could not build the Horizon request: "+err.Error(), accountEvidence)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		accountEvidence.Observed = "request failed"
		return Undetermined(d, s, "Horizon was unreachable: "+err.Error(), accountEvidence)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		accountEvidence.Observed = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return Undetermined(d, s,
			fmt.Sprintf("Horizon returned HTTP %d, so the issuer home_domain could not be read", resp.StatusCode),
			accountEvidence)
	}

	var account accountHomeDomain
	if err := json.NewDecoder(resp.Body).Decode(&account); err != nil {
		accountEvidence.Observed = "unparseable body"
		return Undetermined(d, s, "Horizon's account response could not be parsed: "+err.Error(), accountEvidence)
	}
	domain := strings.TrimSpace(account.HomeDomain)
	accountEvidence.Observed = quoteOrAbsent(domain)
	if domain == "" {
		return Undetermined(d, s, "the issuing account declares no home_domain", accountEvidence)
	}

	tomlURL := anchor.TOMLURL(domain)
	profile, err := c.resolver().Resolve(ctx, domain)
	if err != nil {
		return Undetermined(d, s, "the issuer home_domain document could not be fetched: "+err.Error(),
			accountEvidence, Evidence{Source: tomlURL, Observed: "request failed", ObservedAt: time.Now().UTC()})
	}

	entry, found := findCurrency(profile, s.Asset.Code, s.Asset.Issuer)
	if !found {
		return Fail(d, s,
			"the account declares home_domain="+domain+", but its stellar.toml lists no [[CURRENCIES]] entry matching both code and issuer",
			accountEvidence,
			Evidence{Source: tomlURL + " → [[CURRENCIES]]", Observed: "no matching code and issuer", ObservedAt: time.Now().UTC()})
	}

	return Pass(d, s,
		"the account home_domain and stellar.toml both identify "+entry.Code+" issued by "+entry.Issuer,
		accountEvidence,
		Evidence{Source: tomlURL + " → [[CURRENCIES]] code=" + entry.Code + " issuer", Observed: entry.Issuer, ObservedAt: time.Now().UTC()})
}
