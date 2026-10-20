package checks

import (
	"context"
	"strings"
	"time"
)

// SEP12ServerPublished reports whether the anchor declares a KYC_SERVER.
// It does not probe the endpoint; KYC remains the anchor's responsibility.
type SEP12ServerPublished struct{}

// Describe implements Check.
func (SEP12ServerPublished) Describe() Descriptor {
	return Descriptor{
		ID:       "sep12.server-published",
		Scope:    ScopeAnchor,
		Cost:     CostFree,
		Severity: SeverityInfo,
		Title:    "Anchor publishes a SEP-12 KYC server",
		CanDetermine: "Whether the anchor's stellar.toml declares a KYC_SERVER " +
			"for SEP-12 customer identification.",
		CannotDetermine: "Whether the KYC server is reachable, accepts customers, " +
			"or completes an identity check; this check does not contact it.",
	}
}

// Run implements Check.
func (c SEP12ServerPublished) Run(_ context.Context, s Subject) CheckResult {
	d := c.Describe()
	if s.Profile == nil {
		return Undetermined(d, s, "no stellar.toml has been resolved for this anchor")
	}

	const field = "KYC_SERVER"
	tomlURL := "https://" + s.Profile.Domain + "/.well-known/stellar.toml"
	server := strings.TrimSpace(s.Profile.TOML.KYCServer)
	observed := "(field absent)"
	if server != "" {
		observed = server
	}
	evidence := Evidence{
		Source:     tomlURL + " -> " + field,
		Observed:   observed,
		ObservedAt: time.Now().UTC(),
	}
	if server == "" {
		return Fail(d, s, "the anchor does not publish a SEP-12 KYC server", evidence)
	}
	return Pass(d, s, "the anchor publishes a SEP-12 KYC server", evidence)
}
