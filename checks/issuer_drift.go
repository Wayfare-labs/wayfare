package checks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// IssuerDriftBaseline is the issuer state captured by the preceding sweep.
type IssuerDriftBaseline struct {
	AuthRequired        bool
	AuthRevocable       bool
	AuthClawbackEnabled bool
	AuthImmutable       bool
	TOMLHash            string
}

// IssuerDrift checks whether an issuer's stellar.toml or authorization flags
// have drifted or changed over time/schedule compared to expected initial state
// or recorded history.
type IssuerDrift struct {
	HorizonURL string
	HTTPClient *http.Client
	Baseline   *IssuerDriftBaseline
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
	if s.Profile == nil || s.Profile.TOMLHash == "" {
		return Undetermined(d, s, "the issuer stellar.toml could not be fingerprinted")
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
	current := IssuerDriftBaseline{
		AuthRequired:        f.AuthRequired,
		AuthRevocable:       f.AuthRevocable,
		AuthClawbackEnabled: f.AuthClawbackEnabled,
		AuthImmutable:       f.AuthImmutable,
		TOMLHash:            s.Profile.TOMLHash,
	}
	ev := Evidence{
		Source:     url + " → drift-check",
		Observed:   issuerDriftObservation(current),
		ObservedAt: at,
	}

	if c.Baseline == nil {
		return Undetermined(d, s, "no stored issuer baseline exists yet", ev)
	}
	if *c.Baseline != current {
		return Fail(d, s, "issuer flags or stellar.toml have changed since the stored baseline", ev)
	}
	return Pass(d, s, "issuer flags and stellar.toml match the stored baseline", ev)
}

func issuerDriftObservation(state IssuerDriftBaseline) string {
	return fmt.Sprintf("auth_required=%t auth_revocable=%t auth_clawback_enabled=%t auth_immutable=%t toml_sha256=%s",
		state.AuthRequired, state.AuthRevocable, state.AuthClawbackEnabled, state.AuthImmutable, state.TOMLHash)
}

// IssuerDriftBaselineFromChecks restores the last valid drift observation
// from findings persisted in a run-store record.
func IssuerDriftBaselineFromChecks(findings []CheckJSON) *IssuerDriftBaseline {
	for _, finding := range findings {
		if finding.ID != "issuer.drift" {
			continue
		}
		for _, evidence := range finding.Evidence {
			if !strings.HasSuffix(evidence.Source, " → drift-check") {
				continue
			}
			state, ok := parseIssuerDriftObservation(evidence.Observed)
			if ok {
				return &state
			}
		}
	}
	return nil
}

func parseIssuerDriftObservation(observed string) (IssuerDriftBaseline, bool) {
	values := make(map[string]string, 5)
	for _, field := range strings.Fields(observed) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return IssuerDriftBaseline{}, false
		}
		values[key] = value
	}
	var state IssuerDriftBaseline
	var err error
	if state.AuthRequired, err = strconv.ParseBool(values["auth_required"]); err != nil {
		return IssuerDriftBaseline{}, false
	}
	if state.AuthRevocable, err = strconv.ParseBool(values["auth_revocable"]); err != nil {
		return IssuerDriftBaseline{}, false
	}
	if state.AuthClawbackEnabled, err = strconv.ParseBool(values["auth_clawback_enabled"]); err != nil {
		return IssuerDriftBaseline{}, false
	}
	if state.AuthImmutable, err = strconv.ParseBool(values["auth_immutable"]); err != nil {
		return IssuerDriftBaseline{}, false
	}
	state.TOMLHash = values["toml_sha256"]
	hash, err := hex.DecodeString(state.TOMLHash)
	if err != nil || len(hash) != sha256.Size {
		return IssuerDriftBaseline{}, false
	}
	return state, true
}
