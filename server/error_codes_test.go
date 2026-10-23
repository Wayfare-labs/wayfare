package server

// Error-code contract tests.
//
// Backlog entry #177 (issue #237): "Document the error responses". The
// document is only a contract if the codes it names are the codes the service
// actually emits — and stable across endpoints. Before this, the corridor
// handler returned lower-case codes defined once (`unknown_receive_asset`),
// while the trend, chain-heads and market-structure handlers inlined upper-case
// twins of the same causes (`UNKNOWN_ASSET`, `STORE_READ_ERROR`). A client
// switching on `code` could not treat two endpoints the same way, and the
// document would have had to describe two spellings as though they were one.
//
// api_schema_test.go already pins the *shape* of an error (exactly `error` and
// `code`, never measurement-shaped). This file pins the *values* and their
// cross-endpoint identity, so a rename is caught here rather than by a
// consumer. It is the test the prose in docs/errors.md points to.

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// errorCode performs one request and returns its machine-readable code and
// status. It fails the test if the body is not the documented error shape.
func errorCode(t *testing.T, method, url string) (string, int) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("building %s %s: %v", method, url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s %s: %v", method, url, err)
	}
	var body map[string]string
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("%s %s: body is not a JSON error object: %v (%s)", method, url, err, raw)
	}
	code, ok := body["code"]
	if !ok || code == "" {
		t.Fatalf("%s %s: error body carries no code: %s", method, url, raw)
	}
	return code, resp.StatusCode
}

// TestErrorCodeContract walks each endpoint's error paths and pins the exact
// code. A code here is an API contract: clients switch on it, so a rename is a
// breaking change and must be deliberate.
func TestErrorCodeContract(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")

	cases := []struct {
		name       string
		method     string
		path       string
		wantCode   string
		wantStatus int
	}{
		// /api/corridor
		{"corridor method", http.MethodPost, "/api/corridor", codeMethodNotAllowed, http.StatusMethodNotAllowed},
		{"corridor unknown param", http.MethodGet, "/api/corridor?tp=NGNC", codeInvalidQuery, http.StatusBadRequest},
		{"corridor unknown send asset", http.MethodGet, "/api/corridor?from=SCAMC", codeUnknownSendAsset, http.StatusBadRequest},
		{"corridor unknown receive asset", http.MethodGet, "/api/corridor?to=SCAMC", codeUnknownReceiveAsset, http.StatusBadRequest},
		{"corridor no fiat peg", http.MethodGet, "/api/corridor?to=USDC", codeNoFiatPeg, http.StatusBadRequest},
		{"corridor invalid sizes", http.MethodGet, "/api/corridor?sizes=abc", codeInvalidSizes, http.StatusBadRequest},

		// /api/corridor/trend — the same causes must carry the same codes.
		{"trend method", http.MethodPost, "/api/corridor/trend", codeMethodNotAllowed, http.StatusMethodNotAllowed},
		{"trend unknown param", http.MethodGet, "/api/corridor/trend?tp=NGNC", codeInvalidQuery, http.StatusBadRequest},
		{"trend unknown send asset", http.MethodGet, "/api/corridor/trend?from=SCAMC", codeUnknownSendAsset, http.StatusBadRequest},
		{"trend unknown receive asset", http.MethodGet, "/api/corridor/trend?to=SCAMC", codeUnknownReceiveAsset, http.StatusBadRequest},
		{"trend no fiat peg", http.MethodGet, "/api/corridor/trend?to=USDC", codeNoFiatPeg, http.StatusBadRequest},
		{"trend invalid limit", http.MethodGet, "/api/corridor/trend?limit=abc", codeInvalidLimit, http.StatusBadRequest},
		{"trend invalid cursor", http.MethodGet, "/api/corridor/trend?before=abc", codeInvalidCursor, http.StatusBadRequest},
		{"trend zero cursor", http.MethodGet, "/api/corridor/trend?before=0", codeInvalidCursor, http.StatusBadRequest},
		{"trend negative cursor", http.MethodGet, "/api/corridor/trend?before=-1", codeInvalidCursor, http.StatusBadRequest},

		// Read-only endpoints still share the method/param contract.
		{"market-structure method", http.MethodPost, "/api/market-structure", codeMethodNotAllowed, http.StatusMethodNotAllowed},
		{"market-structure unknown param", http.MethodGet, "/api/market-structure?tp=x", codeInvalidQuery, http.StatusBadRequest},
		{"health unknown param", http.MethodGet, "/healthz?tp=x", codeInvalidQuery, http.StatusBadRequest},
		{"chain-heads method", http.MethodPost, "/api/chain-heads", codeMethodNotAllowed, http.StatusMethodNotAllowed},
		{"chain-heads unknown param", http.MethodGet, "/api/chain-heads?tp=x", codeInvalidQuery, http.StatusBadRequest},
		{"thresholds method", http.MethodPost, "/api/thresholds", codeMethodNotAllowed, http.StatusMethodNotAllowed},
		{"thresholds unknown param", http.MethodGet, "/api/thresholds?tp=x", codeInvalidQuery, http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, status := errorCode(t, tc.method, srv.URL+tc.path)
			if code != tc.wantCode {
				t.Errorf("code = %q, want %q", code, tc.wantCode)
			}
			if status != tc.wantStatus {
				t.Errorf("status = %d, want %d", status, tc.wantStatus)
			}
		})
	}
}

// TestErrorCodesAreLowerSnakeCase pins the spelling convention the document
// states: codes are lower-case snake_case. The upper-case twins this contract
// replaced (`UNKNOWN_ASSET`, `STORE_READ_ERROR`) were the drift the document
// could not honestly call a single contract.
func TestErrorCodesAreLowerSnakeCase(t *testing.T) {
	codes := []string{
		codeMethodNotAllowed,
		codeUnknownSendAsset,
		codeUnknownReceiveAsset,
		codeNoFiatPeg,
		codeInvalidSizes,
		codeMeasurementFailed,
		codeUpstreamTimeout,
		codeInvalidQuery,
		codeInternalError,
		codeInvalidLimit,
		codeInvalidCursor,
		codeStoreRead,
		codeDivergenceHistory,
		codeRateLimited,
		codeNoHistory,
	}
	for _, c := range codes {
		for _, r := range c {
			if r >= 'A' && r <= 'Z' {
				t.Errorf("error code %q is not lower-case snake_case", c)
				break
			}
		}
		if c == "" {
			t.Error("empty error code")
		}
	}
}
