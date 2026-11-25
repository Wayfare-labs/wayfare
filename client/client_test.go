package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"github.com/shopspring/decimal"
)


func TestClientCorridorAndAssets(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/corridor" {
			_ = r.ParseForm()
			_, _ = w.Write([]byte(`{
				"live": false,
				"scored": true,
				"send": {"code": "USDC", "issuer": "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2EGX3QIWIQTDPUYLCIHMZDD"},
				"receive": {"code": "NGNC", "issuer": "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2EGX3QIWIQTDPUYLCIHMZDD"},
				"integrity": "DIRECT",
				"recommended": {
					"size": "100",
					"send_amount": "100",
					"receive": "62900",
					"mid": "1364.5",
					"effective_rate": "629",
					"loss_percent": "53.90",
					"verdict": "POOR"
				},
				"rungs": [
					{
						"size": "100",
						"send_amount": "100",
						"receive": "62900",
						"mid": "1364.5",
						"effective_rate": "629",
						"loss_percent": "53.90",
						"verdict": "POOR"
					}
				]
			`))
			return
		}
		if r.URL.Path == "/api/assets" {
			_, _ = w.Write([]byte(`{"assets": [{"code": "USDC", "issuer": ""}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	res, err := c.Corridor(ctx, CorridorParams{From: "USDC", To: "NGNC"})
	if err != nil {
		t.Fatalf("Corridor error: %v", err)
	}
	if res.Live {
		t.Errorf("want live false")
	}
	if !res.Scored {
		t.Errorf("want scored true")
	}
	if res.Recommended == nil {
		t.Fatalf("want non-nil recommended")
	}
	if !res.Recommended.Size.Equal(decimal.NewFromInt(100)) {
		t.Errorf("recommended size = %s, want 100", res.Recommended.Size)
	}

	assets, err := c.Assets(ctx)
	if err != nil {
		t.Fatalf("Assets error: %v", err)
	}
	if len(assets) != 1 || assets[0].Code != "USDC" {
		t.Errorf("unexpected assets: %+v", assets)
	}
}
func TestClientGetCorridor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"send_asset": "USDC",
			"receive_asset": "NGNC",
			"integrity": "ONE-SIDED",
			"reference_mid": "1350.5",
			"reference_source": "exchangerate-api",
			"scored": true,
			"recommended": null,
			"floor_loss_pct": "25.0",
			"worst_loss_pct": "95.0",
			"live": false,
			"measured_at": "2026-08-23T00:00:00Z"
		}`)) 
		_ = r
	}))
	defer srv.Close()

	c := New(srv.URL)
	resp, err := c.GetCorridor(context.Background(), "USDC", "NGNC", false)
	if err != nil {
		// If any issue occurs, check signature
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Integrity != "ONE-SIDED" {
		t.Errorf("Integrity = %q, want ONE-SIDED", resp.Integrity)
	}
}
