package checks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/anchor"
	"github.com/Wayfare-labs/wayfare/asset"
)

func TestIssuerDrift_Run(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"flags":{"auth_required":false,"auth_revocable":false,"auth_immutable":true,"auth_clawback_enabled":false}}`))
	}))
	defer ts.Close()

	check := IssuerDrift{
		HorizonURL: ts.URL,
		HTTPClient: ts.Client(),
	}

	sub := Subject{
		Asset: asset.Asset{
			Code:   "USDC",
			Issuer: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
		},
		Profile: &anchor.Profile{TOMLHash: strings.Repeat("a", 64)},
	}

	res := check.Run(context.Background(), sub)
	if res.Determined || res.Passed {
		t.Fatalf("expected undetermined without a baseline, got determined=%v passed=%v: %s",
			res.Determined, res.Passed, res.Summary)
	}

	check.Baseline = &IssuerDriftBaseline{
		AuthImmutable: true,
		TOMLHash:      strings.Repeat("a", 64),
	}
	res = check.Run(context.Background(), sub)
	if !res.Determined || !res.Passed {
		t.Fatalf("expected a determined pass against the matching baseline, got determined=%v passed=%v: %s",
			res.Determined, res.Passed, res.Summary)
	}

	check.Baseline.TOMLHash = strings.Repeat("b", 64)
	res = check.Run(context.Background(), sub)
	if !res.Determined || res.Passed {
		t.Fatalf("expected TOML drift to fail, got determined=%v passed=%v: %s",
			res.Determined, res.Passed, res.Summary)
	}

	check.Baseline.TOMLHash = strings.Repeat("a", 64)
	check.Baseline.AuthRevocable = true
	res = check.Run(context.Background(), sub)
	if !res.Determined || res.Passed {
		t.Fatalf("expected flag drift to fail, got determined=%v passed=%v: %s",
			res.Determined, res.Passed, res.Summary)
	}
}

func TestIssuerDriftBaselineFromChecks(t *testing.T) {
	want := IssuerDriftBaseline{
		AuthRequired:        true,
		AuthClawbackEnabled: true,
		TOMLHash:            strings.Repeat("a", 64),
	}
	findings := []CheckJSON{{
		ID: "issuer.drift",
		Evidence: []EvidenceJSON{{
			Source:   "https://horizon.stellar.org/accounts/issuer → drift-check",
			Observed: issuerDriftObservation(want),
		}},
	}}
	got := IssuerDriftBaselineFromChecks(findings)
	if got == nil || *got != want {
		t.Fatalf("restored baseline = %#v, want %#v", got, want)
	}
	if got := IssuerDriftBaselineFromChecks(nil); got != nil {
		t.Fatalf("baseline without stored findings = %#v, want nil", got)
	}
}
