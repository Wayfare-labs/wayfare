package checks

import (
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/anchor"
)

func TestSEP38QuoteServerPublished(t *testing.T) {
	cases := []struct {
		name       string
		profile    *anchor.Profile
		determined bool
		passed     bool
		contains   string
	}{
		{
			name: "NGNC does not publish a quote server",
			profile: &anchor.Profile{
				Domain: "ngnc.online",
				TOML: anchor.TOML{
					WebAuthEndpoint:   "https://ngnc.online/auth",
					TransferServer24:  "https://ngnc.online/sep24",
					AnchorQuoteServer: "", // deliberately absent
				},
			},
			determined: true,
			passed:     false,
			contains:   "does not publish ANCHOR_QUOTE_SERVER",
		},
		{
			name: "an anchor that publishes a quote server passes",
			profile: &anchor.Profile{
				Domain: "test.anchor.com",
				TOML: anchor.TOML{
					AnchorQuoteServer: "https://test.anchor.com/sep38",
				},
			},
			determined: true,
			passed:     true,
			contains:   "ANCHOR_QUOTE_SERVER at https://test.anchor.com/sep38",
		},
		{
			name:       "no profile at all is undetermined",
			profile:    nil,
			determined: false,
			contains:   "no stellar.toml",
		},
		{
			name: "whitespace-only quote server is treated as absent",
			profile: &anchor.Profile{
				Domain: "test.anchor.com",
				TOML: anchor.TOML{
					AnchorQuoteServer: "   ",
				},
			},
			determined: true,
			passed:     false,
			contains:   "does not publish ANCHOR_QUOTE_SERVER",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Run(ctx(), SEP38QuoteServerPublished{},
				Subject{Profile: tc.profile, Domain: "test"})

			if r.Determined != tc.determined {
				t.Fatalf("Determined = %v, want %v (summary: %s)", r.Determined, tc.determined, r.Summary)
			}
			if tc.determined && r.Passed != tc.passed {
				t.Errorf("Passed = %v, want %v (summary: %s)", r.Passed, tc.passed, r.Summary)
			}
			haystack := r.Summary + " " + r.Reason
			if !strings.Contains(haystack, tc.contains) {
				t.Errorf("result %q does not mention %q", haystack, tc.contains)
			}

			if r.Determined && len(r.Evidence) == 0 {
				t.Error("a determined result must carry at least one evidence item")
			}
		})
	}
}

func TestSEP38CheckRunsInDefaultSuite(t *testing.T) {
	r := &Runner{}
	checks := r.Default()
	found := false
	for _, c := range checks {
		if _, ok := c.(SEP38QuoteServerPublished); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("SEP38QuoteServerPublished is not in the default check set")
	}
}
