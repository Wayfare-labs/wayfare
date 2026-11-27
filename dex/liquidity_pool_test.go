package dex_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/dex"
)

// poolServer answers /liquidity_pools/{id} with body and records the path it
// was asked for, so the tests can check the id is escaped into one segment.
func poolServer(t *testing.T, status int, body string) (*httptest.Server, *string) {
	t.Helper()
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/hal+json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

const poolID = "dd7b1ab831c273310ddbec6f97870aa83c2fbd78ce22aded37ecbf4f3380fac7"

func TestLiquidityPoolParsesHorizonResponse(t *testing.T) {
	srv, asked := poolServer(t, http.StatusOK, `{
		"id": "`+poolID+`",
		"fee_bp": 30,
		"type": "constant_product",
		"total_shares": "5000.0000000",
		"total_trustlines": "12",
		"reserves": [
			{"asset": "native", "amount": "1000.0000000"},
			{"asset": "USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN", "amount": "120.0000000"}
		]
	}`)

	c := &dex.Client{HorizonURL: srv.URL}
	got, err := c.LiquidityPool(context.Background(), poolID)
	if err != nil {
		t.Fatalf("LiquidityPool: %v", err)
	}
	if *asked != "/liquidity_pools/"+poolID {
		t.Errorf("requested %q, want /liquidity_pools/%s", *asked, poolID)
	}
	if got.ID != poolID || got.FeeBP != 30 || got.Type != "constant_product" ||
		got.TotalShares != "5000.0000000" || got.TotalTrustlines != "12" {
		t.Errorf("pool = %+v, fields did not round-trip", got)
	}
	if len(got.Reserves) != 2 || got.Reserves[0].Asset != "native" ||
		got.Reserves[1].Amount != "120.0000000" {
		t.Errorf("reserves = %+v", got.Reserves)
	}
}

// An explicit zero fee is a real answer; it must not be mistaken for a
// missing one.
func TestLiquidityPoolAcceptsExplicitZeroFee(t *testing.T) {
	srv, _ := poolServer(t, http.StatusOK, `{"id":"p","fee_bp":0,"reserves":[]}`)
	got, err := (&dex.Client{HorizonURL: srv.URL}).LiquidityPool(context.Background(), "p")
	if err != nil {
		t.Fatalf("LiquidityPool: %v", err)
	}
	if got.FeeBP != 0 {
		t.Errorf("FeeBP = %d, want 0", got.FeeBP)
	}
}

func TestLiquidityPoolRejectsMissingFee(t *testing.T) {
	srv, _ := poolServer(t, http.StatusOK, `{"id":"p","reserves":[]}`)
	_, err := (&dex.Client{HorizonURL: srv.URL}).LiquidityPool(context.Background(), "p")
	if err == nil || !strings.Contains(err.Error(), "reports no fee_bp") {
		t.Fatalf("err = %v, want a missing fee_bp error", err)
	}
}

func TestLiquidityPoolEscapesID(t *testing.T) {
	srv, asked := poolServer(t, http.StatusOK, `{"id":"a/b","fee_bp":30}`)
	if _, err := (&dex.Client{HorizonURL: srv.URL}).LiquidityPool(context.Background(), "a/b"); err != nil {
		t.Fatalf("LiquidityPool: %v", err)
	}
	if *asked != "/liquidity_pools/a%2Fb" {
		t.Errorf("requested %q, want the id escaped into one path segment", *asked)
	}
}

func TestLiquidityPoolPropagatesHorizonError(t *testing.T) {
	srv, _ := poolServer(t, http.StatusNotFound, `{"status":404}`)
	_, err := (&dex.Client{HorizonURL: srv.URL}).LiquidityPool(context.Background(), "p")
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("err = %v, want the HTTP 404 surfaced", err)
	}
}

func TestLiquidityPoolHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// No server: a cancelled context must return before any request.
	_, err := (&dex.Client{HorizonURL: "http://127.0.0.1:1"}).LiquidityPool(ctx, "p")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
