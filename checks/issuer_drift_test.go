package checks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wayfare-labs/wayfare/asset"
)

func TestIssuerDrift_Run(t *testing.T) {
	Ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		W.Header().Set("Content-Type", "application/json")
		W.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"flags":{"auth_required":false,"auth_revocable":false,"auth_immutable":true,"auth_clawback_enabled":false}}`))
	}))
	Defer ts.Close()

	Check := IssuerDrift{
		HorizonURL: ts.URL,
		HTTPClient: ts.Client(),
	}

	Sub := Subject{
		Asset: asset.Asset{
			Code:   "USDC",
			Issuer: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
		},
	}

	Res := check.Run(context.Background(), sub)
	if res.Verdict != VerdictPass {
		T.Fatalf("expected VerdictPass, got %%v: %%s", res.Verdict, res.Summary)
	}
}
