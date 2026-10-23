package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxSEP31InfoBytes = 1 << 20

// SEP31Info checks that the declared SEP-31 server responds to /info and
// advertises the asset under examination.
type SEP31Info struct {
	HTTPClient *http.Client
}

// Describe implements Check.
func (SEP31Info) Describe() Descriptor {
	return Descriptor{
		ID:       "sep31.info",
		Scope:    ScopeAsset,
		Cost:     CostOneRequest,
		Severity: SeverityWarning,
		Title:    "Declared SEP-31 server lists this asset",
		CanDetermine: "Whether the DIRECT_PAYMENT_SERVER declared in the anchor's " +
			"stellar.toml answers /info and enables the examined asset for receiving.",
		CannotDetermine: "Whether a payment will complete, what it will cost, or " +
			"whether the server's other SEP-31 operations work.",
	}
}

func (c SEP31Info) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return GuardedClient(15 * time.Second)
}

// Run implements Check.
func (c SEP31Info) Run(ctx context.Context, s Subject) CheckResult {
	d := c.Describe()
	if s.Profile == nil {
		return Undetermined(d, s, "no stellar.toml has been resolved for this anchor")
	}
	if s.Asset.Code == "" {
		return Undetermined(d, s, "no asset was supplied to check against the SEP-31 info response")
	}

	declared := strings.TrimSpace(s.Profile.TOML.DirectPaymentServer)
	tomlURL := "https://" + s.Profile.Domain + "/.well-known/stellar.toml"
	// Evidence about the cached document carries the time it was originally
	// fetched, not the time this check runs.
	at := s.Profile.FetchedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if declared == "" {
		return Fail(d, s, "the anchor does not publish a DIRECT_PAYMENT_SERVER for SEP-31",
			Evidence{Source: tomlURL + " -> DIRECT_PAYMENT_SERVER",
				Observed: "(field absent)", ObservedAt: at})
	}

	infoURL, err := sep31InfoURL(declared)
	if err != nil {
		return Fail(d, s, "the declared DIRECT_PAYMENT_SERVER is not a usable URL: "+err.Error(),
			Evidence{Source: tomlURL + " -> DIRECT_PAYMENT_SERVER",
				Observed: declared, ObservedAt: at})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, infoURL, nil)
	if err != nil {
		return Undetermined(d, s, "could not build the SEP-31 info request: "+err.Error())
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return Fail(d, s, "the declared SEP-31 /info endpoint did not respond: "+err.Error(),
			Evidence{Source: infoURL, Observed: "request failed", ObservedAt: at})
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Fail(d, s, fmt.Sprintf("the declared SEP-31 /info endpoint returned HTTP %d", resp.StatusCode),
			Evidence{Source: infoURL, Observed: fmt.Sprintf("HTTP %d", resp.StatusCode), ObservedAt: at})
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSEP31InfoBytes+1))
	if err != nil {
		return Fail(d, s, "could not read the SEP-31 /info response: "+err.Error(),
			Evidence{Source: infoURL, Observed: "response body read failed", ObservedAt: at})
	}
	if len(body) > maxSEP31InfoBytes {
		return Fail(d, s, "the SEP-31 /info response exceeded the size limit",
			Evidence{Source: infoURL, Observed: "response body too large", ObservedAt: at})
	}

	var info struct {
		Receive map[string]struct {
			Enabled *bool `json:"enabled"`
		} `json:"receive"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return Fail(d, s, "the SEP-31 /info response was not valid JSON: "+err.Error(),
			Evidence{Source: infoURL, Observed: "unparseable JSON", ObservedAt: at})
	}

	assetInfo, listed := info.Receive[s.Asset.Code]
	if !listed {
		return Fail(d, s, "the SEP-31 /info response does not list "+s.Asset.Code+" as a receive asset",
			Evidence{Source: infoURL + " -> receive", Observed: "asset absent: " + s.Asset.Code, ObservedAt: at})
	}
	if assetInfo.Enabled == nil {
		return Fail(d, s, "the SEP-31 /info entry for "+s.Asset.Code+" has no enabled flag",
			Evidence{Source: infoURL + " -> receive." + s.Asset.Code,
				Observed: "enabled field absent", ObservedAt: at})
	}
	if !*assetInfo.Enabled {
		return Fail(d, s, "the SEP-31 /info response lists "+s.Asset.Code+" but it is disabled",
			Evidence{Source: infoURL + " -> receive." + s.Asset.Code,
				Observed: "enabled=false", ObservedAt: at})
	}
	return Pass(d, s, "the declared SEP-31 server responds and enables "+s.Asset.Code+" for receiving",
		Evidence{Source: infoURL + " -> receive." + s.Asset.Code,
			Observed: "enabled=true", ObservedAt: at})
}

func sep31InfoURL(server string) (string, error) {
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
