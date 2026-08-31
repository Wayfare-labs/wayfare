package checks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
	}

	res := check.Run(context.Background(), sub)
	if !res.Determined || !res.Passed {
		t.Fatalf("expected a determined pass, got determined=%v passed=%v: %s",
			res.Determined, res.Passed, res.Summary)
	}
}
