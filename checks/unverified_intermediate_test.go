package checks

import (
	"context"
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/asset"
)

func runUnverifiedIntermediate(t *testing.T, hops ...asset.Asset) CheckResult {
	t.Helper()
	return Run(context.Background(), UnverifiedIntermediate{}, Subject{
		Send:    asset.USDC(),
		Receive: asset.NGNC(),
		Hops:    hops,
	})
}

// TestUnverifiedIntermediatePasses keeps the check from reporting a problem
// where there is no issuer to assess.
func TestUnverifiedIntermediatePasses(t *testing.T) {
	cases := []struct {
		name string
		hops []asset.Asset
	}{
		{"no intermediates", nil},
		{"native XLM only", []asset.Asset{asset.Native()}},
		{"registered issuers", []asset.Asset{asset.USDZ(), asset.PYUSD()}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runUnverifiedIntermediate(t, tc.hops...)
			if !res.Determined {
				t.Fatalf("Determined = false, want a local registry lookup to be determined: %s",
					res.Reason)
			}
			if !res.Passed {
				t.Fatalf("Passed = false, want pass: %s", res.Summary)
			}
			if res.ID != "hop.issuer-verified" {
				t.Errorf("ID = %q, want hop.issuer-verified", res.ID)
			}
		})
	}
}

// TestUnverifiedIntermediateFailsAndNamesThem is the finding: an unregistered
// issuer on the path must be reported by asset, not treated as a bridge hop.
func TestUnverifiedIntermediateFailsAndNamesThem(t *testing.T) {
	aqua := asset.Stellar("AQUA",
		"GBNZILSTVQZ4R7IKQDGHYGY2QXL5QOFJYQMXPKWRRM5PAV7Y4M67AQUA")
	cleanshave := asset.Stellar("Cleanshave",
		"GCTZJCIPAJQ5OTNL5MJGA6ETMG7A3GOKYYZUO6JJPWUZCJSSUYNVAISG")

	// Registered and native hops sit between the two unverified ones to
	// prove neither is mistaken for a failure.
	res := runUnverifiedIntermediate(t, aqua, asset.Native(), asset.USDZ(), cleanshave)

	if !res.Determined || res.Passed {
		t.Fatalf("Determined=%t Passed=%t, want a determined failure: %s",
			res.Determined, res.Passed, res.Summary)
	}
	if res.Severity != SeverityWarning {
		t.Errorf("Severity = %s, want warning", res.Severity)
	}
	for _, code := range []string{"AQUA", "Cleanshave"} {
		if !strings.Contains(res.Summary, code) {
			t.Errorf("summary does not name %s: %s", code, res.Summary)
		}
	}
	if strings.Contains(res.Summary, "USDZ") {
		t.Errorf("summary names a registered hop: %s", res.Summary)
	}
	if len(res.Evidence) != 2 {
		t.Fatalf("got %d evidence entries, want 2 (one per unverified hop): %v",
			len(res.Evidence), res.Evidence)
	}
	if res.Evidence[0].Source != aqua.Issuer {
		t.Errorf("evidence[0].Source = %q, want the issuer account %q",
			res.Evidence[0].Source, aqua.Issuer)
	}
}

// TestUnverifiedIntermediateReportsMissingIssuer treats an issued asset with
// no issuing account as unverified rather than skipping it. Identity nothing
// established is not a verified identity.
func TestUnverifiedIntermediateReportsMissingIssuer(t *testing.T) {
	res := runUnverifiedIntermediate(t, asset.Stellar("MYSTERY", ""))

	if !res.Determined || res.Passed {
		t.Fatalf("Determined=%t Passed=%t, want a determined failure: %s",
			res.Determined, res.Passed, res.Summary)
	}
	if !strings.Contains(res.Summary, "MYSTERY") {
		t.Errorf("summary does not name the issuer-less asset: %s", res.Summary)
	}
	if len(res.Evidence) != 1 || !strings.Contains(res.Evidence[0].Observed, "issuer unknown") {
		t.Errorf("evidence does not record the missing issuer: %v", res.Evidence)
	}
}

func TestUnverifiedIntermediateDescriptor(t *testing.T) {
	if err := (UnverifiedIntermediate{}).Describe().Validate(); err != nil {
		t.Fatalf("descriptor invalid: %v", err)
	}
}
