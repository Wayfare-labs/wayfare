package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// fakePathfinder implements route.Pathfinder and always succeeds, returning a
// valid path for any source amount so live DEX measurements never fail with 5xx.
type fakePathfinder struct{}

func (f *fakePathfinder) StrictSendPaths(ctx context.Context, source asset.Asset, amount decimal.Decimal, dest asset.Asset) ([]dex.Path, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Return a valid direct path paying a positive amount with at most 7 decimals.
	// For instance, effective rate 1500 (1 USDC -> 1500 NGNC).
	destAmt := amount.Mul(decimal.RequireFromString("1500")).Truncate(7)
	if !destAmt.IsPositive() {
		destAmt = decimal.RequireFromString("0.0000001")
	}
	return []dex.Path{
		{
			SourceAsset:  source,
			SourceAmount: amount,
			DestAsset:    dest,
			DestAmount:   destAmt,
			Hops:         nil, // direct
		},
	}, nil
}

func (f *fakePathfinder) MeasureSlippage(ctx context.Context, source asset.Asset, amount decimal.Decimal, dest asset.Asset, probe decimal.Decimal) (*dex.Slippage, error) {
	rate := decimal.RequireFromString("1500")
	return &dex.Slippage{
		ProbeAmount: probe,
		ProbeRate:   rate,
		FullAmount:  amount,
		FullRate:    rate,
		PctWorse:    decimal.Zero,
	}, nil
}

var _ route.Pathfinder = (*fakePathfinder)(nil)

// FuzzQueryParams fuzzes query-parameter parsing across all server endpoints.
//
// In Go's net/url:
//   - r.URL.Query().Get(k) returns the FIRST value associated with key k.
//     Therefore, when duplicate keys are provided (e.g. `limit=1&limit=2`),
//     the first value wins for `from`, `to`, `sizes`, `limit`, and `live`.
//   - wantsPretty(r) inspects r.URL.Query()["pretty"][0], so the first value
//     wins for `pretty` as well.
func FuzzQueryParams(f *testing.F) {
	// 1. Seed corpus
	// Seeds supply values for: (rawQuery, from, to, sizes, limit, live, pretty)
	// If rawQuery is non-empty, it is used directly as req.URL.RawQuery (exercising duplicate keys & unknown params).
	// Otherwise, a query string is built from the individual param values.
	seeds := []struct {
		rawQuery string
		from     string
		to       string
		sizes    string
		limit    string
		live     string
		pretty   string
	}{
		// Standard valid defaults
		{"", "USDC", "NGNC", "100", "10", "1", "0"},
		{"", "USDC", "NGNC", "0.1,1,10,100", "50", "true", "true"},
		{"", "USDC", "NGNC", "", "", "", ""},
		// Empty and whitespace variants
		{"", "  USDC  ", "  NGNC  ", "  100 , 200  ", "  5  ", "  true  ", " 1 "},
		{"", " ", " ", " ", " ", " ", " "},
		// Numbers: zero, negative, floats, overflows, scientific / hex
		{"", "USDC", "NGNC", "0", "0", "0", ""},
		{"", "USDC", "NGNC", "-1", "-1", "false", ""},
		{"", "USDC", "NGNC", "-100,50", "-50", "0", ""},
		{"", "USDC", "NGNC", "100.5", "10.5", "1", ""},
		{"", "USDC", "NGNC", "NaN", "NaN", "NaN", ""},
		{"", "USDC", "NGNC", "Inf", "Inf", "Inf", ""},
		{"", "USDC", "NGNC", "+Inf", "-Inf", "1", ""},
		{"", "USDC", "NGNC", "1e5", "1e5", "1", ""},
		{"", "USDC", "NGNC", "0x10", "0x10", "1", ""},
		{"", "USDC", "NGNC", "9223372036854775807", "9223372036854775807", "1", ""},
		{"", "USDC", "NGNC", "9223372036854775808", "9223372036854775808", "1", ""},
		{"", "USDC", "NGNC", "18446744073709551615", "18446744073709551615", "1", ""},
		{"", "USDC", "NGNC", "-9223372036854775808", "-9223372036854775809", "1", ""},
		// Malformed sizes lists and limits
		{"", "USDC", "NGNC", "1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25", "1", "1", ""}, // > maxSizes (24)
		{"", "USDC", "NGNC", ",,,,", "1", "1", ""},
		{"", "USDC", "NGNC", "abc", "abc", "maybe", ""},
		{"", "USDC", "NGNC", "1,abc,2", "12a", "yes", ""},
		{"", "USDC", "NGNC", "1,,2", " 10 ", "no", ""},
		// Invalid / unusual assets
		{"", "UNKNOWN", "NGNC", "10", "10", "1", ""},
		{"", "USDC", "UNKNOWN", "10", "10", "1", ""},
		{"", "USDC", "USDC", "10", "10", "1", ""}, // USDC has no fiat peg as recvAsset
		// String edge cases: UTF-8, null bytes, long strings
		{"", "\x00", "\xff\xfe", "100\x00", "1\x00", "1", ""},
		{"", strings.Repeat("A", 1000), strings.Repeat("B", 1000), strings.Repeat("1,", 50), "10000000000", "true", ""},
		// Raw queries: duplicate keys (first wins), unknown parameters
		{"from=USDC&from=UNKNOWN&to=NGNC&sizes=10&sizes=abc", "", "", "", "", "", ""},
		{"limit=1&limit=2&from=USDC&to=NGNC", "", "", "", "", "", ""},
		{"unknown_param=123&from=USDC&to=NGNC", "", "", "", "", "", ""},
		{"foo=bar", "", "", "", "", "", ""},
		{"pretty=1&pretty=0", "USDC", "NGNC", "10", "10", "1", "1"},
		{"pretty=true&pretty=false", "USDC", "NGNC", "10", "10", "1", "true"},
		{"pretty", "USDC", "NGNC", "10", "10", "1", ""},
		{"%00=1&%ff=%fe", "", "", "", "", "", ""},
		{"from=USDC&to=NGNC&sizes=10%200", "", "", "", "", "", ""},
		{"sizes=10&sizes=-5&limit=10&limit=-1", "", "", "", "", "", ""},
	}

	for _, s := range seeds {
		f.Add(s.rawQuery, s.from, s.to, s.sizes, s.limit, s.live, s.pretty)
	}

	// 2. Setup server dependencies once before f.Fuzz (using f.TempDir())
	st, err := runstore.Open(f.TempDir())
	if err != nil {
		f.Fatalf("opening runstore: %v", err)
	}

	baseTime := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	rec := &runstore.Record{
		RecordedAt: baseTime,
		Corridor:   "USDC-NGNC",
		Integrity:  "DIRECT",
		Reference: runstore.Reference{
			Mid:           "1500.00",
			Source:        "exchangerate-api",
			AsOf:          baseTime.Format(time.RFC3339),
			ScoredAgainst: "exchangerate-api",
		},
		FloorLossPct: "0.00", FloorSize: "100",
		WorstLossPct: "0.00", WorstSize: "100",
		Finding: "fuzz seeded run",
		Rungs: []runstore.Rung{{
			SendAmount: "100", Priced: true, Integrity: "DIRECT",
			ReceiveAmount: "150000", EffectiveRate: "1500",
			LossPct: "0.00", Verdict: "EXCELLENT", Path: "USDC -> NGNC",
		}},
	}
	if err := st.Append(context.Background(), rec); err != nil {
		f.Fatalf("appending seed record: %v", err)
	}

	srv := &Server{
		Engine: &route.Engine{
			DEX: &fakePathfinder{},
			RefRate: refrate.NewStatic(map[string]decimal.Decimal{
				"USD/NGN": decimal.RequireFromString("1500"),
				"USD/GHS": decimal.RequireFromString("11.7625"),
				"USD/KES": decimal.RequireFromString("129.4263"),
			}),
		},
		Store:        st,
		HistoryFirst: false, // ensures live measurements through DEX are exercised
	}

	// Build mux directly without rate limiter wrapping it to avoid any 429s
	mux := http.NewServeMux()
	mux.HandleFunc("/api/corridor", srv.handleCorridor)
	mux.HandleFunc("/api/corridor/trend", srv.handleTrend)
	mux.HandleFunc("/api/chain-heads", srv.handleChainHeads)
	mux.HandleFunc("/api/assets", srv.handleAssets)
	mux.HandleFunc("/api/market-structure", srv.handleMarketStructure)
	mux.HandleFunc("/healthz", srv.handleHealth)

	// Endpoints to test
	endpoints := []struct {
		path         string
		allowsSizes  bool
		allowsLimit  bool
		allowsFromTo bool
		allowsLive   bool
	}{
		{"/api/corridor", true, false, true, true},
		{"/api/corridor/trend", false, true, true, false},
		{"/api/chain-heads", false, false, false, false},
		{"/api/assets", false, false, false, false},
		{"/api/market-structure", false, false, false, false},
		{"/healthz", false, false, false, false},
	}

	// 3. Fuzz execution
	f.Fuzz(func(t *testing.T, rawQuery, from, to, sizes, limit, live, pretty string) {
		for _, ep := range endpoints {
			var queryString string
			if rawQuery != "" {
				queryString = rawQuery
			} else {
				vals := url.Values{}
				if from != "" {
					vals.Set("from", from)
				}
				if to != "" {
					vals.Set("to", to)
				}
				if sizes != "" {
					vals.Set("sizes", sizes)
				}
				if limit != "" {
					vals.Set("limit", limit)
				}
				if live != "" {
					vals.Set("live", live)
				}
				if pretty != "" {
					vals.Set("pretty", pretty)
				}
				queryString = vals.Encode()
			}

			targetURL := ep.path
			if queryString != "" {
				targetURL += "?" + queryString
			}

			req, err := http.NewRequest(http.MethodGet, targetURL, nil)
			if err != nil {
				// If net/url or http.NewRequest rejects the URL, skip iteration
				t.Skip()
			}

			rec := httptest.NewRecorder()

			// Run request wrapped in recover to catch any panic
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("PANIC on %s: %v\nInput: rawQuery=%q, from=%q, to=%q, sizes=%q, limit=%q, live=%q, pretty=%q",
							targetURL, r, rawQuery, from, to, sizes, limit, live, pretty)
					}
				}()
				mux.ServeHTTP(rec, req)
			}()

			res := rec.Result()
			defer res.Body.Close()

			// Invariant 1: Must never return 5xx
			if res.StatusCode >= 500 {
				t.Fatalf("Server returned %d (5xx) on %s\nBody: %s",
					res.StatusCode, targetURL, rec.Body.String())
			}

			// Invariant 2: Must be 2xx or exactly 400
			if (res.StatusCode < 200 || res.StatusCode >= 300) && res.StatusCode != http.StatusBadRequest {
				t.Fatalf("Unexpected status code %d on %s\nBody: %s",
					res.StatusCode, targetURL, rec.Body.String())
			}

			// Check for endpoints that accept none of the 5 query params
			parsedQuery := req.URL.Query()
			hasDisallowedParam := false
			if !ep.allowsFromTo && (parsedQuery.Has("from") || parsedQuery.Has("to")) {
				hasDisallowedParam = true
			}
			if !ep.allowsSizes && parsedQuery.Has("sizes") {
				hasDisallowedParam = true
			}
			if !ep.allowsLimit && parsedQuery.Has("limit") {
				hasDisallowedParam = true
			}
			if !ep.allowsLive && parsedQuery.Has("live") {
				hasDisallowedParam = true
			}

			// Invariant 3: If 400, body must be typed error
			if res.StatusCode == http.StatusBadRequest {
				var errBody struct {
					Error string `json:"error"`
					Code  string `json:"code"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
					t.Fatalf("400 response on %s is not valid JSON: %v\nBody: %s",
						targetURL, err, rec.Body.String())
				}
				if errBody.Error == "" {
					t.Fatalf("400 response on %s has empty 'error' field\nBody: %s",
						targetURL, rec.Body.String())
				}
				if errBody.Code == "" {
					t.Fatalf("400 response on %s has empty 'code' field\nBody: %s",
						targetURL, rec.Body.String())
				}

				// If disallowed param was present, error code must be "invalid_query"
				if hasDisallowedParam && errBody.Code != codeInvalidQuery {
					t.Fatalf("Expected code %q for disallowed param on %s, got %q\nBody: %s",
						codeInvalidQuery, targetURL, errBody.Code, rec.Body.String())
				}
			}

			// Invariant 4: If 2xx, basic sanity check and cheap oracle bounds
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				if hasDisallowedParam {
					t.Fatalf("Endpoint %s succeeded with 2xx but had disallowed query parameters\nQuery: %s",
						ep.path, queryString)
				}
				var jsonBody map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &jsonBody); err != nil {
					t.Fatalf("2xx response on %s is not valid JSON: %v\nBody: %s",
						targetURL, err, rec.Body.String())
				}

				// Oracle bounds cross-checks on 2xx:
				if ep.path == "/api/corridor/trend" {
					if runs, ok := jsonBody["runs"].([]any); ok {
						if len(runs) > maxTrendLimit {
							t.Fatalf("Trend response returned %d runs, exceeds maxTrendLimit %d",
								len(runs), maxTrendLimit)
						}
					}
				}
				if ep.path == "/api/corridor" {
					if rungs, ok := jsonBody["rungs"].([]any); ok {
						if len(rungs) > maxSizes {
							t.Fatalf("Corridor response returned %d rungs, exceeds maxSizes %d",
								len(rungs), maxSizes)
						}
					}
				}
			}
		}
	})
}
