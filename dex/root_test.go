package dex_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/dex"
)

// rootServer serves rootBody for Horizon's root endpoint and nothing else:
// an unrecorded path is a hard failure, so a test cannot accidentally lean
// on the network.
func rootServer(t *testing.T, rootBody string, status int) *dex.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" || status != http.StatusOK {
			http.Error(w, "upstream down", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/hal+json")
		_, _ = w.Write([]byte(rootBody))
	}))
	t.Cleanup(srv.Close)
	return &dex.Client{HorizonURL: srv.URL}
}

// TestChainHeadReadsRecordedRoot pins the parse against a body shaped like
// the real Horizon root resource (fields abridged, names verbatim) recorded
// from mainnet.
func TestChainHeadReadsRecordedRoot(t *testing.T) {
	body := `{
	  "horizon_version": "2.32.0-unstable",
	  "core_version": "v22.0.0",
	  "history_latest_ledger": 55101723,
	  "history_elder_ledger": 52334383,
	  "core_latest_ledger": 55101723,
	  "network_passphrase": "Test SDF Network ; September 2015",
	  "protocol_version": 23,
	  "core_ledger_seq": 55101724,
	  "latest_ledger": 55101723
	}`
	c := rootServer(t, body, http.StatusOK)

	head, err := c.ChainHead(context.Background())
	if err != nil {
		t.Fatalf("ChainHead: %v", err)
	}
	if head != 55101724 {
		t.Errorf("ChainHead = %d, want the root's core_ledger_seq 55101724", head)
	}
}

// TestChainHeadRefusesMissingSeq: a root body without a usable
// core_ledger_seq is an error, not a silent zero. Callers render the error
// as unknown; a returned 0 would read as "the chain is at ledger zero".
func TestChainHeadRefusesMissingSeq(t *testing.T) {
	c := rootServer(t, `{"history_latest_ledger": 55101723}`, http.StatusOK)
	if head, err := c.ChainHead(context.Background()); err == nil {
		t.Errorf("ChainHead = %d, want an error when core_ledger_seq is absent", head)
	}
}

// TestChainHeadPropagatesHTTPErrors keeps a refused lookup an error.
func TestChainHeadPropagatesHTTPErrors(t *testing.T) {
	c := rootServer(t, "", http.StatusServiceUnavailable)
	if _, err := c.ChainHead(context.Background()); err == nil {
		t.Error("ChainHead: want error on HTTP 503, got nil")
	} else if !strings.Contains(err.Error(), "503") {
		t.Errorf("ChainHead error = %v, want it to name the status", err)
	}
}
