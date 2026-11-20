package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/anchor"
)

func TestIsFiatCandidate(t *testing.T) {
	tests := []struct {
		name string
		curr anchor.Currency
		want bool
	}{
		{
			name: "explicit fiat anchor_asset_type",
			curr: anchor.Currency{Code: "BRL", AnchorAssetType: "fiat", AnchorAsset: "BRL"},
			want: true,
		},
		{
			name: "known ISO fiat code",
			curr: anchor.Currency{Code: "NGNC", AnchorAsset: "NGN"},
			want: true,
		},
		{
			name: "anchored with 3-letter currency code",
			curr: anchor.Currency{Code: "KESC", AnchorAsset: "KES", IsAssetAnchored: true},
			want: true,
		},
		{
			name: "crypto tethered token",
			curr: anchor.Currency{Code: "yXLM", AnchorAsset: "XLM", AnchorAssetType: "crypto"},
			want: false,
		},
		{
			name: "unanchored meme token",
			curr: anchor.Currency{Code: "DOGE", AnchorAsset: ""},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsFiatCandidate(tc.curr); got != tc.want {
				t.Errorf("IsFiatCandidate() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWriteCSV(t *testing.T) {
	candidates := []CandidateFiatToken{
		{
			AssetCode:       "NGNC",
			AssetIssuer:     "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6",
			HomeDomain:      "ngnc.online",
			Holders:         450,
			AnchorAsset:     "NGN",
			AnchorAssetType: "fiat",
			Status:          "live",
			QuoteServer:     "",
			TOMLURL:         "https://ngnc.online/.well-known/stellar.toml",
			Evidence:        `anchor_asset="NGN"; anchor_asset_type="fiat"; is_asset_anchored=true`,
		},
	}

	var buf bytes.Buffer
	if err := WriteCSV(&buf, candidates); err != nil {
		t.Fatalf("WriteCSV failed: %v", err)
	}

	r := csv.NewReader(&buf)
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("reading generated CSV: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (header + 1 record), got %d", len(rows))
	}
	if rows[0][0] != "asset_code" || rows[0][4] != "anchor_asset" {
		t.Errorf("unexpected header: %v", rows[0])
	}
	if rows[1][0] != "NGNC" || rows[1][4] != "NGN" {
		t.Errorf("unexpected record row: %v", rows[1])
	}
}

func TestDiscoverCandidates_MockServer(t *testing.T) {
	tomlBody := `
VERSION="2.0.0"
NETWORK_PASSPHRASE="Public Global Stellar Network ; September 2015"
TRANSFER_SERVER_SEP0024="https://anchor.example.com/sep24"

[[CURRENCIES]]
code="TESTFIAT"
issuer="GATOKENISSUER123456789"
status="live"
is_asset_anchored=true
anchor_asset="EUR"
anchor_asset_type="fiat"

[[CURRENCIES]]
code="NOTFIAT"
issuer="GATOKENISSUER123456789"
status="live"
anchor_asset="BTC"
anchor_asset_type="crypto"
`
	tomlServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "stellar.toml") {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(tomlBody))
			return
		}
		http.NotFound(w, r)
	}))
	defer tomlServer.Close()

	horizonServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := fmt.Sprintf(`{
			"_embedded": {
				"records": [
					{
						"asset_code": "TESTFIAT",
						"asset_issuer": "GATOKENISSUER123456789",
						"accounts": { "authorized": 120 },
						"_links": {
							"toml": { "href": "%s/.well-known/stellar.toml" }
						}
					},
					{
						"asset_code": "NOTFIAT",
						"asset_issuer": "GATOKENISSUER123456789",
						"accounts": { "authorized": 50 },
						"_links": {
							"toml": { "href": "%s/.well-known/stellar.toml" }
						}
					}
				]
			},
			"_links": {
				"next": { "href": "" }
			}
		}`, tomlServer.URL, tomlServer.URL)
		w.Write([]byte(resp))
	}))
	defer horizonServer.Close()

	cfg := Config{
		HorizonURL:     horizonServer.URL,
		Limit:          10,
		RateLimitDelay: 1 * time.Millisecond,
		HTTPClient:     horizonServer.Client(),
	}

	candidates, err := DiscoverCandidates(context.Background(), cfg)
	if err != nil {
		t.Fatalf("DiscoverCandidates failed: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	c := candidates[0]
	if c.AssetCode != "TESTFIAT" {
		t.Errorf("expected asset code TESTFIAT, got %s", c.AssetCode)
	}
	if c.AnchorAsset != "EUR" {
		t.Errorf("expected anchor asset EUR, got %s", c.AnchorAsset)
	}
	if c.AnchorAssetType != "fiat" {
		t.Errorf("expected anchor asset type fiat, got %s", c.AnchorAssetType)
	}
	if !strings.Contains(c.Evidence, "EUR") {
		t.Errorf("expected evidence to mention EUR, got %s", c.Evidence)
	}
}

func TestDiscoverCandidates_HandlesUnreachableTOML(t *testing.T) {
	horizonServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"_embedded": {
				"records": [
					{
						"asset_code": "UNREACHABLE",
						"asset_issuer": "GATOKENISSUER123456789",
						"accounts": { "authorized": 10 },
						"_links": {
							"toml": { "href": "http://127.0.0.1:1/nonexistent.toml" }
						}
					}
				]
			},
			"_links": {
				"next": { "href": "" }
			}
		}`
		w.Write([]byte(resp))
	}))
	defer horizonServer.Close()

	cfg := Config{
		HorizonURL:     horizonServer.URL,
		Limit:          5,
		RateLimitDelay: 1 * time.Millisecond,
		HTTPClient:     horizonServer.Client(),
	}

	candidates, err := DiscoverCandidates(context.Background(), cfg)
	if err != nil {
		t.Fatalf("DiscoverCandidates failed: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(candidates))
	}
}

func TestDiscoverCandidates_StellarExpertMockServer(t *testing.T) {
	expertServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"_embedded": {
				"records": [
					{
						"asset": "EURC-GATOKENISSUER123456789-1",
						"code": "EURC",
						"domain": "eurc.example.com",
						"trustlines": { "authorized": 80 },
						"tomlInfo": {
							"code": "EURC",
							"issuer": "GATOKENISSUER123456789",
							"anchorAsset": "EUR",
							"anchorAssetType": "fiat",
							"status": "live"
						}
					},
					{
						"asset": "BTC-GABTCISSUER123456789-1",
						"code": "BTC",
						"domain": "btc.example.com",
						"trustlines": { "authorized": 30 },
						"tomlInfo": {
							"code": "BTC",
							"issuer": "GABTCISSUER123456789",
							"anchorAsset": "BTC",
							"anchorAssetType": "crypto",
							"status": "live"
						}
					}
				]
			},
			"_links": {
				"next": { "href": "" }
			}
		}`
		w.Write([]byte(resp))
	}))
	defer expertServer.Close()

	cfg := Config{
		Source:           "stellarexpert",
		StellarExpertURL: expertServer.URL,
		Limit:            10,
		RateLimitDelay:   1 * time.Millisecond,
		HTTPClient:       expertServer.Client(),
	}

	candidates, err := DiscoverCandidates(context.Background(), cfg)
	if err != nil {
		t.Fatalf("DiscoverCandidates failed: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].AssetCode != "EURC" || candidates[0].AnchorAsset != "EUR" {
		t.Errorf("unexpected candidate: %+v", candidates[0])
	}
}
