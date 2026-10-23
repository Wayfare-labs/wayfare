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

// SEP6InfoListsAsset checks that the asset declared in stellar.toml is also
// present in the anchor's SEP-6 operational metadata.
type SEP6InfoListsAsset struct {
	HTTPClient *http.Client
}

// Describe implements Check.
func (SEP6InfoListsAsset) Describe() Descriptor {
	return Descriptor{
		ID:       "sep6.info-lists-asset",
		Scope:    ScopeAsset,
		Cost:     CostOneRequest,
		Severity: SeverityNotice,
		Title:    "SEP-6 /info lists the TOML asset",
		CanDetermine: "Whether the asset's anchor_asset declared in stellar.toml is listed " +
			"by the anchor's SEP-6 /info endpoint, and whether deposit and withdrawal are enabled.",
		CannotDetermine: "Whether a listed and enabled flow will complete successfully, what " +
			"limits or fees apply, or whether the endpoint remains available after this observation.",
	}
}

func (c SEP6InfoListsAsset) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return GuardedClient(15 * time.Second)
}

type sep6Operation struct {
	Enabled bool `json:"enabled"`
}

type sep6Info struct {
	Deposit  map[string]sep6Operation `json:"deposit"`
	Withdraw map[string]sep6Operation `json:"withdraw"`
}

// Run implements Check.
func (c SEP6InfoListsAsset) Run(ctx context.Context, s Subject) CheckResult {
	d := c.Describe()
	if s.Profile == nil {
		return Undetermined(d, s, "no stellar.toml has been resolved for this anchor, so no SEP-6 server was declared")
	}

	server := strings.TrimSpace(s.Profile.TOML.TransferServer)
	tomlURL := "https://" + s.Profile.Domain + "/.well-known/stellar.toml"
	at := time.Now().UTC()
	if server == "" {
		return Undetermined(d, s,
			"the anchor declares no TRANSFER_SERVER, so it offers no SEP-6 flow to probe",
			Evidence{Source: tomlURL + " -> TRANSFER_SERVER", Observed: "(field absent)", ObservedAt: at})
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
			"the stellar.toml does not list the subject asset, so its SEP-6 asset name cannot be determined",
			Evidence{Source: tomlURL + " -> CURRENCIES", Observed: s.Asset.Code, ObservedAt: at})
	}

	infoURL, err := sep6InfoURL(server)
	if err != nil {
		return Fail(d, s, "the declared TRANSFER_SERVER is not a usable URL: "+err.Error(),
			Evidence{Source: tomlURL + " -> TRANSFER_SERVER", Observed: server, ObservedAt: at})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, infoURL, nil)
	if err != nil {
		return Fail(d, s, "could not build the SEP-6 /info request: "+err.Error(),
			Evidence{Source: infoURL, Observed: "request could not be built", ObservedAt: at})
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client().Do(req)
	if err != nil {
		return Fail(d, s, "the declared SEP-6 /info endpoint did not respond: "+err.Error(),
			Evidence{Source: infoURL, Observed: "request failed", ObservedAt: at})
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Fail(d, s, fmt.Sprintf("the declared SEP-6 /info endpoint returned HTTP %d", resp.StatusCode),
			Evidence{Source: infoURL, Observed: fmt.Sprintf("HTTP %d", resp.StatusCode), ObservedAt: at})
	}

	var info sep6Info
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return Fail(d, s, "the SEP-6 /info response was not valid JSON: "+err.Error(),
			Evidence{Source: infoURL, Observed: "unparseable body", ObservedAt: at})
	}

	deposit, hasDeposit := info.Deposit[currency]
	withdraw, hasWithdraw := info.Withdraw[currency]
	if !hasDeposit && !hasWithdraw {
		return Fail(d, s, fmt.Sprintf("the SEP-6 /info response omits asset %q", currency),
			Evidence{Source: infoURL, Observed: fmt.Sprintf("asset %q absent from deposit and withdraw", currency), ObservedAt: at})
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
	ev := Evidence{Source: infoURL, Observed: fmt.Sprintf("%q: %s", currency, strings.Join(flow, ", ")), ObservedAt: at}
	return Pass(d, s, fmt.Sprintf("SEP-6 /info lists %q (%s)", currency, strings.Join(flow, "; ")), ev)
}

func sep6InfoURL(server string) (string, error) {
	u, err := url.Parse(server)
	if err != nil {
		return "", err
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("expected an http or https URL with a host")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/info"
	u.RawPath = ""
	return u.String(), nil
}
