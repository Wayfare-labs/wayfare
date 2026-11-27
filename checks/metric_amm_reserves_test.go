package checks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

func TestAmmPoolReservesUndeterminedWhenNoPoolIDs(t *testing.T) {
	m := AmmPoolReservesMetric{}
	s := Subject{
		Send:    asset.USDC(),
		Receive: asset.NGNC(),
	}

	res := m.Run(context.Background(), s)
	if res.Determined {
		t.Errorf("expected undetermined when path does not expose pool IDs, got determined")
	}
	if !strings.Contains(res.Reason, "path does not expose liquidity pool IDs") {
		t.Errorf("Reason = %q, want mention that path does not expose pool IDs", res.Reason)
	}
	if res.Venue != VenuePathfinding {
		t.Errorf("Venue = %s, want %s", res.Venue, VenuePathfinding)
	}
}

func TestAmmPoolReservesDeterminedWithEvidence(t *testing.T) {
	poolID := "6b72e6b2413a9675eb5e3fc45ba1a64627b0c9ad3b6d05f3ad39b563be2cffb4"
	mockHorizon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/liquidity_pools/"+poolID {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id": "6b72e6b2413a9675eb5e3fc45ba1a64627b0c9ad3b6d05f3ad39b563be2cffb4",
				"fee_bp": 30,
				"type": "constant_product",
				"total_shares": "1234.5678000",
				"reserves": [
					{"asset": "native", "amount": "15000.5000000"},
					{"asset": "USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN", "amount": "2500.2500000"}
				]
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockHorizon.Close()

	dClient := &dex.Client{HorizonURL: mockHorizon.URL}
	metric := AmmPoolReservesMetric{
		DEX:     dClient,
		PoolIDs: []string{poolID},
	}
	s := Subject{
		Send:    asset.USDC(),
		Receive: asset.NGNC(),
	}

	res := metric.Run(context.Background(), s)
	if !res.Determined {
		t.Fatalf("expected determined result, got undetermined: %s", res.Reason)
	}
	if res.Venue != VenuePathfinding {
		t.Errorf("Venue = %s, want %s", res.Venue, VenuePathfinding)
	}
	if res.Unit != UnitCount || res.Value.String() != "1" {
		t.Errorf("value = %s %s, want 1 %s; reserves of different assets must not be summed", res.Value, res.Unit, UnitCount)
	}
	if len(res.Evidence) != 1 {
		t.Fatalf("len(Evidence) = %d, want 1", len(res.Evidence))
	}
	ev := res.Evidence[0]
	if !strings.Contains(ev.Observed, "fee_bp=30") {
		t.Errorf("evidence missing fee: %s", ev.Observed)
	}
	if !strings.Contains(ev.Observed, "15000.5000000") || !strings.Contains(ev.Observed, "2500.2500000") {
		t.Errorf("evidence missing decimal reserves: %s", ev.Observed)
	}
}

// Incomplete pool data is unknown, never a partial or zero figure.
func TestAmmPoolReservesUndeterminedWhenIncomplete(t *testing.T) {
	for name, body := range map[string]string{
		"unreadable reserve": `{"id":"p","fee_bp":30,"reserves":[{"asset":"native","amount":"10"},{"asset":"USDC:G","amount":"unavailable"}]}`,
		"no reserves":        `{"id":"p","fee_bp":30}`,
		"no fee":             `{"id":"p","reserves":[{"asset":"native","amount":"10"},{"asset":"USDC:G","amount":"5"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			m := AmmPoolReservesMetric{DEX: &dex.Client{HorizonURL: srv.URL}, PoolIDs: []string{"p"}}
			if res := m.Run(context.Background(), Subject{Send: asset.USDC(), Receive: asset.NGNC()}); res.Determined {
				t.Errorf("determined %s %s from incomplete data", res.Value, res.Unit)
			}
		})
	}
}

func TestAmmPoolReservesSnapshotBacked(t *testing.T) {
	manifestPath := "../testdata/snapshots/usdc-ngnc-20260821T223040Z"
	m, err := snapshot.Load(manifestPath)
	if err != nil {
		t.Fatalf("loading snapshot: %v", err)
	}

	dClient := &dex.Client{
		HorizonURL: "https://horizon.stellar.org",
		HTTPClient: m.HTTPClient(),
	}

	// For standard snapshot where paths do not expose pool IDs
	metric := AmmPoolReservesMetric{
		DEX: dClient,
	}
	s := Subject{
		Send:    asset.USDC(),
		Receive: asset.NGNC(),
	}
	res := metric.Run(context.Background(), s)
	if res.Determined {
		t.Errorf("standard snapshot paths should be undetermined for pool IDs")
	}
}
