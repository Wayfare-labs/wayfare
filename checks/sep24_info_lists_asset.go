package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SEP24InfoListsAsset checks that the asset declared in stellar.toml is also
// present in the anchor's SEP-24 operational metadata.
type SEP24InfoListsAsset struct {
	HTTPClient *http.Client
}

// Describe implements Check.
func (SEP24InfoListsAsset) Describe() Descriptor {
	return Descriptor{
		ID:       "sep24.info-lists-asset",
		Scope:    ScopeAsset,
		Cost:     CostOneRequest,
		Severity: SeverityNotice,
		Title:    "SEP-24 /info lists the TOML asset",
		CanDetermine: "Whether the asset's anchor_asset declared in stellar.toml is listed " +
			"by the anchor's SEP-24 /info endpoint, and whether deposit and withdrawal are enabled.",
		CannotDetermine: "Whether a listed and enabled flow will complete successfully, what " +
			"limits or fees apply, or whether the endpoint remains available after this observation.",
	}
}

func (c SEP24InfoListsAsset) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return GuardedClient(15 * time.Second)
}

type sep24Operation struct {
	Enabled bool `json:"enabled"`
}

type sep24Info struct {
	Deposit  map[string]sep24Operation `json:"deposit"`
	Withdraw map[string]sep24Operation `json:"withdraw"`
}

// Run implements Check.
func (c SEP24InfoListsAsset) Run(ctx context.Context, s Subject) CheckResult {
	d := c.Describe()
	if s.Profile == nil {
		return Undetermined(d, s, "no stellar.toml has been resolved for this anchor, so no SEP-24 server was declared")
	}

	server := strings.TrimSpace(s.Profile.TOML.TransferServer24)
	tomlURL := "https://" + s.Profile.Domain + "/.well-known/stellar.toml"
	at := time.Now().UTC()
	if server == "" {
		return Undetermined(d, s,
			"the anchor declares no TRANSFER_SERVER_SEP0024, so it offers no SEP-24 flow to probe",
			Evidence{Source: tomlURL + " -> TRANSFER_SERVER_SEP0024", Observed: "(field absent)", ObservedAt: at})
	}

	currency := ""
	for _, entry := range s.Profile.TOML.Currencies {
		if entry.Code == s.Asset.Code && entry.Issuer == s.Asset.Issuer {
			currency = strings.TrimSpace(entry.AnchorAsset)
			if currency == "" {
				currency = entry.Code
			}
			break
		}
	}
	if currency == "" {
		return Undetermined(d, s,
			"the stellar.toml does not list the subject asset, so its SEP-24 asset name cannot be determined",
			Evidence{Source: tomlURL + " -> CURRENCIES", Observed: s.Asset.Code, ObservedAt: at})
	}

	base, err := url.Parse(server)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		if err == nil {
			err = fmt.Errorf("URL must have an http or https scheme and a host")
		}
		return Fail(d, s, "the declared TRANSFER_SERVER_SEP0024 is not a usable URL: "+err.Error(),
			Evidence{Source: tomlURL + " -> TRANSFER_SERVER_SEP0024", Observed: server, ObservedAt: at})
	}
	probeURL := strings.TrimRight(base.String(), "/") + "/info"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return Fail(d, s, "could not build the SEP-24 /info request: "+err.Error(),
			Evidence{Source: probeURL, Observed: "request could not be built", ObservedAt: at})
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client().Do(req)
	if err != nil {
		return Fail(d, s, "the declared SEP-24 /info endpoint did not respond: "+err.Error(),
			Evidence{Source: probeURL, Observed: "request failed", ObservedAt: at})
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Fail(d, s, fmt.Sprintf("the declared SEP-24 /info endpoint returned HTTP %d", resp.StatusCode),
			Evidence{Source: probeURL, Observed: fmt.Sprintf("HTTP %d", resp.StatusCode), ObservedAt: at})
	}

	var info sep24Info
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return Fail(d, s, "the SEP-24 /info response was not valid JSON: "+err.Error(),
			Evidence{Source: probeURL, Observed: "unparseable body", ObservedAt: at})
	}

	deposit, hasDeposit := info.Deposit[currency]
	withdraw, hasWithdraw := info.Withdraw[currency]
	if !hasDeposit && !hasWithdraw {
		return Fail(d, s, fmt.Sprintf("the SEP-24 /info response omits asset %q", currency),
			Evidence{Source: probeURL, Observed: fmt.Sprintf("asset %q absent from deposit and withdraw", currency), ObservedAt: at})
	}

	flow := []string{}
	if hasDeposit && deposit.Enabled {
		flow = append(flow, "deposit enabled")
	} else {
		flow = append(flow, "deposit disabled")
	}
	if hasWithdraw && withdraw.Enabled {
		flow = append(flow, "withdrawal enabled")
	} else {
		flow = append(flow, "withdrawal disabled")
	}
	ev := Evidence{Source: probeURL, Observed: fmt.Sprintf("%q: %s", currency, strings.Join(flow, ", ")), ObservedAt: at}
	return Pass(d, s, fmt.Sprintf("SEP-24 /info lists %q (%s)", currency, strings.Join(flow, "; ")), ev)
}
