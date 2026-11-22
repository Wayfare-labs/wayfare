package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/asset"
)

// newTestServer creates an httptest.Server that serves pre-recorded responses
// from the testdata directory, routing by URL path and query string patterns.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// Horizon account endpoint.
	mux.HandleFunc("/accounts/", func(w http.ResponseWriter, r *http.Request) {
		body := readTestdata(t, "account_ngnc_issuer.json")
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})

	// Strict-send paths: dispatch by source_amount query param.
	mux.HandleFunc("/paths/strict-send", func(w http.ResponseWriter, r *http.Request) {
		amt := r.URL.Query().Get("source_amount")
		var file string
		switch amt {
		case "1":
			file = "paths_ngnc_1usdc.json"
		case "100":
			file = "paths_ngnc_100usdc.json"
		case "1000":
			file = "paths_ngnc_1000usdc.json"
		default:
			file = "paths_ngnc_1usdc.json"
		}
		body := readTestdata(t, file)
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})

	// Order book endpoint.
	mux.HandleFunc("/order_book", func(w http.ResponseWriter, r *http.Request) {
		body := readTestdata(t, "orderbook_ngnc.json")
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})

	// stellar.toml served at the well-known path.
	mux.HandleFunc("/.well-known/stellar.toml", func(w http.ResponseWriter, r *http.Request) {
		body := readTestdata(t, "stellar_toml_ngnc.txt")
		w.Header().Set("Content-Type", "text/plain")
		w.Write(body)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading testdata/%s: %v", name, err)
	}
	return b
}

// TestProbeUsesRecordedResponses confirms the tool produces output from
// testdata without hitting the real network.
func TestProbeUsesRecordedResponses(t *testing.T) {
	srv := newTestServer(t)
	const (
		code   = "NGNC"
		issuer = "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"
	)
	r := probe(context.Background(), code, issuer, srv.URL, srv.Client())

	if r.AccountErr != nil {
		t.Fatalf("unexpected AccountErr: %v", r.AccountErr)
	}
	if r.Account == nil {
		t.Fatal("Account is nil")
	}
	if r.Account.HomeDomain == "" {
		t.Error("Account.HomeDomain is empty")
	}
	if r.ProfileErr != nil {
		t.Fatalf("unexpected ProfileErr: %v", r.ProfileErr)
	}
	if r.Profile == nil {
		t.Fatal("Profile is nil")
	}
	// stellar.toml fixture declares mainnet passphrase.
	if !r.Profile.Mainnet {
		t.Error("Profile.Mainnet should be true for testdata fixture")
	}
	// stellar.toml fixture lists NGNC as a live currency for the given issuer.
	if !r.Profile.SupportsAsset(asset.Stellar(code, issuer)) {
		t.Error("Profile.SupportsAsset(NGNC) should be true")
	}
	if len(r.Paths) != 3 {
		t.Fatalf("expected 3 path probes, got %d", len(r.Paths))
	}
	for i, pp := range r.Paths {
		if pp.Err != nil {
			t.Errorf("Paths[%d].Err = %v", i, pp.Err)
		}
		if len(pp.Paths) == 0 {
			t.Errorf("Paths[%d]: expected at least one path in fixture response", i)
		}
	}
	if r.BookErr != nil {
		t.Fatalf("unexpected BookErr: %v", r.BookErr)
	}
	if r.DirectBook == nil {
		t.Fatal("DirectBook is nil")
	}
	if r.DirectBook.BidLevels == 0 {
		t.Error("DirectBook.BidLevels should be > 0 from fixture")
	}
}

// TestProbeOutputContainsKeyFacts confirms the printed template skeleton
// includes the minimum facts a researcher needs: issuer, home domain, SEPs,
// and path table rows.
func TestProbeOutputContainsKeyFacts(t *testing.T) {
	srv := newTestServer(t)
	const (
		code   = "NGNC"
		issuer = "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"
	)
	r := probe(context.Background(), code, issuer, srv.URL, srv.Client())
	// Repoint the stellar.toml URLs to the test server so printTemplate can
	// resolve them.  The home_domain is taken from the recorded account
	// response, which names "ngnc.online"; override it so the template prints
	// the test server URL for reproducibility.
	if r.Account != nil {
		r.Account.HomeDomain = strings.TrimPrefix(srv.URL, "http://")
	}

	f, err := os.CreateTemp(t.TempDir(), "corridor-research-*.md")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	printTemplate(f, r)
	f.Seek(0, 0)
	content, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	wantSubstrings := []string{
		"NGNC",
		issuer,
		"home domain",
		"auth_required",
		"SEP-10",
		"SEP-24",
		"NETWORK_PASSPHRASE",
		"mainnet",
		"paths/strict-send",
		"1 |",
		"100 |",
		"1000 |",
		"order book",
		"Finding",
		"Recommendation",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(string(content), want) {
			t.Errorf("output missing %q", want)
		}
	}
}

// TestProbeNoHomeDomain confirms that a missing home_domain is reported
// clearly rather than panicking.
func TestProbeNoHomeDomain(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/", func(w http.ResponseWriter, r *http.Request) {
		// Return an account with no home_domain.
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"home_domain":"","flags":{"auth_required":false,"auth_revocable":false,"auth_immutable":false,"auth_clawback_enabled":false}}`))
	})
	mux.HandleFunc("/paths/strict-send", func(w http.ResponseWriter, r *http.Request) {
		body := readTestdata(t, "paths_ngnc_1usdc.json")
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
	mux.HandleFunc("/order_book", func(w http.ResponseWriter, r *http.Request) {
		body := readTestdata(t, "orderbook_ngnc.json")
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	r := probe(context.Background(), "NGNC", "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6",
		srv.URL, srv.Client())

	if r.Account == nil {
		t.Fatal("Account should not be nil even with empty home_domain")
	}
	if r.Profile != nil {
		t.Error("Profile should be nil when home_domain is empty")
	}
	// printTemplate must not panic.
	f, _ := os.CreateTemp(t.TempDir(), "*.md")
	defer f.Close()
	printTemplate(f, r) // must not panic
}

// TestProbeAccountError confirms that an HTTP 404 from Horizon surfaces as
// AccountErr and does not panic.
func TestProbeAccountError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"status":404,"title":"Resource Missing"}`))
	})
	mux.HandleFunc("/paths/strict-send", func(w http.ResponseWriter, r *http.Request) {
		body := readTestdata(t, "paths_ngnc_1usdc.json")
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
	mux.HandleFunc("/order_book", func(w http.ResponseWriter, r *http.Request) {
		body := readTestdata(t, "orderbook_ngnc.json")
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	r := probe(context.Background(), "NGNC", "GNOPE",
		srv.URL, srv.Client())

	if r.AccountErr == nil {
		t.Error("expected AccountErr for 404 response")
	}
	// printTemplate must not panic even with a failed account fetch.
	f, _ := os.CreateTemp(t.TempDir(), "*.md")
	defer f.Close()
	printTemplate(f, r)
}

// TestProbePathsNOMarket confirms the output labels a no-path response as
// NO-MARKET, not as a parse error.
func TestProbePathsNOMarket(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/", func(w http.ResponseWriter, r *http.Request) {
		body := readTestdata(t, "account_ngnc_issuer.json")
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
	mux.HandleFunc("/.well-known/stellar.toml", func(w http.ResponseWriter, r *http.Request) {
		body := readTestdata(t, "stellar_toml_ngnc.txt")
		w.Header().Set("Content-Type", "text/plain")
		w.Write(body)
	})
	mux.HandleFunc("/paths/strict-send", func(w http.ResponseWriter, r *http.Request) {
		// Empty record set = no paths.
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"_embedded":{"records":[]}}`))
	})
	mux.HandleFunc("/order_book", func(w http.ResponseWriter, r *http.Request) {
		// Empty order book.
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"bids":[],"asks":[]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	r := probe(context.Background(), "NGNC", "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6",
		srv.URL, srv.Client())

	for i, pp := range r.Paths {
		if pp.Err != nil {
			t.Errorf("Paths[%d].Err = %v (empty records should not error)", i, pp.Err)
		}
		if len(pp.Paths) != 0 {
			t.Errorf("Paths[%d]: expected 0 paths for empty records", i)
		}
	}

	f, _ := os.CreateTemp(t.TempDir(), "*.md")
	defer f.Close()
	printTemplate(f, r)
	f.Seek(0, 0)
	content, _ := os.ReadFile(f.Name())
	if !strings.Contains(string(content), "NO-MARKET") {
		t.Error("output should label zero-path response as NO-MARKET")
	}
}

