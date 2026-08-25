package dex_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// These tests run entirely against recorded mainnet bytes.
//
// The fixtures are real Horizon responses captured by cmd/ladder -record, not
// payloads built from this package's own wire structs. That distinction is the
// point: a fixture marshalled from wirePathRecord can only prove the parser
// agrees with itself, and would not contain the trailing-zero precision, the
// native hop that carries no code or issuer, or the empty records array that
// is a 200 meaning "no market". See docs/snapshot-format.md.
//
// snapshot.Replayer errors on any request it has no recorded answer for, so a
// test that drifts into hitting the network fails rather than passing
// intermittently.

const snapshotRoot = "../testdata/snapshots"

// loadSnapshot opens the one snapshot for a corridor.
//
// Matched by prefix rather than by full name so re-capturing a corridor does
// not require editing every test that uses it.
func loadSnapshot(t *testing.T, prefix string) *snapshot.Manifest {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(snapshotRoot, prefix+"-*"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no snapshot matching %q under %s (err=%v); "+
			"capture one with: go run ./cmd/ladder -to %s -ref currency-api -record testdata/snapshots",
			prefix, snapshotRoot, err, strings.ToUpper(strings.TrimPrefix(prefix, "usdc-")))
	}
	m, err := snapshot.Load(matches[0])
	if err != nil {
		t.Fatalf("loading snapshot %s: %v", matches[0], err)
	}
	return m
}

// replayClient builds a dex.Client answering only from a snapshot.
func replayClient(m *snapshot.Manifest) *dex.Client {
	return &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}
}

func dec(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("bad decimal %q: %v", s, err)
	}
	return d
}

// TestStrictSendPathsParsesRecordedResponse covers the parse against real
// bytes, including the native hop.
func TestStrictSendPathsParsesRecordedResponse(t *testing.T) {
	m := loadSnapshot(t, "usdc-ngnc")
	c := replayClient(m)

	paths, err := c.StrictSendPaths(context.Background(),
		asset.USDC(), dec(t, "100"), asset.NGNC())
	if err != nil {
		t.Fatalf("StrictSendPaths: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("expected at least one path in the recorded response")
	}

	var sawNative bool
	for _, p := range paths {
		if p.SourceAsset.Code != "USDC" || p.DestAsset.Code != "NGNC" {
			t.Errorf("path endpoints = %s -> %s, want USDC -> NGNC",
				p.SourceAsset.Code, p.DestAsset.Code)
		}
		if !p.DestAmount.IsPositive() {
			t.Errorf("destination amount %s is not positive", p.DestAmount)
		}
		for _, h := range p.Hops {
			if h.IsNative() {
				sawNative = true
				// A native hop arrives as {"asset_type":"native"} with no
				// code or issuer. Anything that invented an issuer for it
				// would compare unequal to asset.Native() downstream.
				if h.Issuer != "" {
					t.Errorf("native hop carries issuer %q, want empty", h.Issuer)
				}
			}
		}
	}
	if !sawNative {
		t.Error("expected at least one XLM-bridged path in the NGNC snapshot")
	}
}

// TestBestPathSelectsMaximumWhenNotFirst is the reason BestPath exists in the
// form it does.
//
// Horizon returns its best path first in practice — every response in the
// committed snapshots does — but that ordering is not a documented guarantee,
// so the maximum is selected explicitly. A test using the response as recorded
// would pass against an implementation that simply returned paths[0], which is
// exactly the bug worth catching.
//
// So this reverses the records of a real recorded body and asserts the same
// path still wins. The amounts and shapes stay real; only the order changes.
func TestBestPathSelectsMaximumWhenNotFirst(t *testing.T) {
	m := loadSnapshot(t, "usdc-ngnc")

	key, body := horizonBodyForAmount(t, m, "100")

	var parsed struct {
		Embedded struct {
			Records []json.RawMessage `json:"records"`
		} `json:"_embedded"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("parsing recorded body: %v", err)
	}
	if len(parsed.Embedded.Records) < 2 {
		t.Skipf("recorded body for 100 has %d records; need at least 2 to reorder",
			len(parsed.Embedded.Records))
	}

	// Establish the expected winner from the body as recorded.
	best := bestAmountIn(t, parsed.Embedded.Records)

	// Reverse the records and serve that instead.
	reversed := make([]json.RawMessage, 0, len(parsed.Embedded.Records))
	for i := len(parsed.Embedded.Records) - 1; i >= 0; i-- {
		reversed = append(reversed, parsed.Embedded.Records[i])
	}
	rebuilt, err := json.Marshal(map[string]any{
		"_embedded": map[string]any{"records": reversed},
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/hal+json")
		_, _ = w.Write(rebuilt)
	}))
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL}
	got, err := c.BestPath(context.Background(), asset.USDC(), dec(t, "100"), asset.NGNC())
	if err != nil {
		t.Fatalf("BestPath: %v", err)
	}

	if !got.DestAmount.Equal(best) {
		t.Errorf("BestPath returned %s, want %s.\n"+
			"The records were reversed, so an implementation returning the first "+
			"record rather than the maximum fails here — which is the whole point "+
			"of this test. Recorded key: %s",
			got.DestAmount, best, key)
	}
}

// TestBestPathErrorsWhenNoMarket covers KESC: a 200 response whose records
// array is empty. This must be a clear error rather than a zero-value path,
// because upstream it becomes the NO-MARKET state rather than a bad price.
func TestBestPathErrorsWhenNoMarket(t *testing.T) {
	m := loadSnapshot(t, "usdc-kesc")
	c := replayClient(m)

	paths, err := c.StrictSendPaths(context.Background(),
		asset.USDC(), dec(t, "100"), asset.KESC())
	if err != nil {
		t.Fatalf("StrictSendPaths on an empty-records body should parse cleanly: %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected zero paths for KESC, got %d", len(paths))
	}

	_, err = c.BestPath(context.Background(), asset.USDC(), dec(t, "100"), asset.KESC())
	if err == nil {
		t.Fatal("BestPath returned no error for a corridor with no paths")
	}
	if !strings.Contains(err.Error(), "no path") {
		t.Errorf("error %q should say plainly that no path was found", err)
	}
}

// TestPathRateAndDescribe covers the two derived views of a path, using the
// GHSC snapshot for its multi-hop descriptions.
func TestPathRateAndDescribe(t *testing.T) {
	m := loadSnapshot(t, "usdc-ghsc")
	c := replayClient(m)

	best, err := c.BestPath(context.Background(), asset.USDC(), dec(t, "100"), asset.GHSC())
	if err != nil {
		t.Fatalf("BestPath: %v", err)
	}

	// Rate is destination per source unit.
	want := best.DestAmount.Div(best.SourceAmount)
	if !best.Rate().Equal(want) {
		t.Errorf("Rate() = %s, want %s", best.Rate(), want)
	}

	desc := best.Describe()
	if !strings.HasPrefix(desc, "USDC") || !strings.HasSuffix(desc, "GHSC") {
		t.Errorf("Describe() = %q, want it to run from USDC to GHSC", desc)
	}
	// Every GHSC path in the snapshot traverses NGNC — that is the finding
	// the derivative state exists to express, and it must be visible in the
	// human-readable description too.
	if !strings.Contains(desc, "NGNC") {
		t.Errorf("Describe() = %q, want the NGNC hop named", desc)
	}
	if !strings.Contains(desc, "->") {
		t.Errorf("Describe() = %q, want hops joined with arrows", desc)
	}
}

// TestRateIsZeroSafe guards the division in Rate against a zero source amount.
func TestRateIsZeroSafe(t *testing.T) {
	var p dex.Path
	if got := p.Rate(); !got.IsZero() {
		t.Errorf("Rate() on a zero path = %s, want 0 rather than a panic", got)
	}
}

// TestMeasureSlippageAgainstRecordedAmounts checks PctWorse against values
// computed by hand from the recorded bodies, so the assertion does not simply
// restate the implementation.
func TestMeasureSlippageAgainstRecordedAmounts(t *testing.T) {
	m := loadSnapshot(t, "usdc-ngnc")
	c := replayClient(m)

	probe, full := dec(t, "10"), dec(t, "1000")

	probePath, err := c.BestPath(context.Background(), asset.USDC(), probe, asset.NGNC())
	if err != nil {
		t.Fatalf("probe BestPath: %v", err)
	}
	fullPath, err := c.BestPath(context.Background(), asset.USDC(), full, asset.NGNC())
	if err != nil {
		t.Fatalf("full BestPath: %v", err)
	}

	// Hand-computed from the two recorded destination amounts.
	probeRate := probePath.DestAmount.Div(probe)
	fullRate := fullPath.DestAmount.Div(full)
	wantPct := probeRate.Sub(fullRate).Div(probeRate).Mul(decimal.NewFromInt(100))

	s, err := c.MeasureSlippage(context.Background(), asset.USDC(), full, asset.NGNC(), probe)
	if err != nil {
		t.Fatalf("MeasureSlippage: %v", err)
	}

	if !s.ProbeRate.Equal(probeRate) {
		t.Errorf("ProbeRate = %s, want %s", s.ProbeRate, probeRate)
	}
	if !s.FullRate.Equal(fullRate) {
		t.Errorf("FullRate = %s, want %s", s.FullRate, fullRate)
	}
	if !s.PctWorse.Equal(wantPct) {
		t.Errorf("PctWorse = %s, want %s", s.PctWorse, wantPct)
	}
	// On this corridor the large trade must price materially worse; a
	// non-positive result would mean the thin-liquidity warning never fires.
	if !s.PctWorse.IsPositive() {
		t.Errorf("PctWorse = %s, want positive on a corridor measured as thin", s.PctWorse)
	}
}

// TestMalformedAmountIsAnError pins that a bad number surfaces rather than
// defaulting to zero. A zero destination amount would render as a 100% loss —
// a plausible-looking figure produced by a parse failure.
func TestMalformedAmountIsAnError(t *testing.T) {
	body := `{"_embedded":{"records":[{
      "source_asset_type":"credit_alphanum4","source_asset_code":"USDC",
      "source_amount":"100.0000000",
      "destination_asset_type":"credit_alphanum4","destination_asset_code":"NGNC",
      "destination_amount":"not-a-number","path":[]}]}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL}
	_, err := c.StrictSendPaths(context.Background(), asset.USDC(), dec(t, "100"), asset.NGNC())
	if err == nil {
		t.Fatal("expected an error for an unparseable destination_amount, got none")
	}
	if !strings.Contains(err.Error(), "destination_amount") {
		t.Errorf("error %q should name the field that failed to parse", err)
	}
}

// TestUnrecordedRequestDoesNotReachTheNetwork is the structural guarantee
// behind every other test in this file. If the replayer fell through to the
// network, these tests would be live without saying so.
func TestUnrecordedRequestDoesNotReachTheNetwork(t *testing.T) {
	m := loadSnapshot(t, "usdc-ngnc")
	c := replayClient(m)

	// A size the ladder never priced, so it cannot be in the snapshot.
	_, err := c.StrictSendPaths(context.Background(), asset.USDC(), dec(t, "31337"), asset.NGNC())
	if err == nil {
		t.Fatal("an unrecorded request succeeded; the replayer reached the network")
	}
	if !strings.Contains(err.Error(), "no recorded response") {
		t.Errorf("error %q should say the request was not recorded", err)
	}
}

// TestHorizonThrottleSurfacesAsRateLimited pins the distinction between a
// throttled Horizon and a broken one.
//
// A 429 is routine under a monitoring schedule — the remedy is to wait for
// the interval and retry. A 500 means the upstream is unhealthy. Both used to
// surface as the same generic error string, so a corridor that was merely
// rate-limited read as an outage. The typed error must survive unwrapping so
// a caller can branch on it programmatically rather than by substring.
func TestHorizonThrottleSurfacesAsRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL}
	_, err := c.StrictSendPaths(context.Background(), asset.USDC(), dec(t, "100"), asset.NGNC())
	if err == nil {
		t.Fatal("expected an error for HTTP 429, got none")
	}

	var limited *dex.ErrRateLimited
	if !errors.As(err, &limited) {
		t.Fatalf("error %q does not unwrap to ErrRateLimited; a throttled corridor "+
			"is indistinguishable from a broken one", err)
	}
	if limited.Endpoint != "/paths/strict-send" {
		t.Errorf("Endpoint = %q, want the pathfinding endpoint", limited.Endpoint)
	}
	if limited.RetryAfter != 90*time.Second {
		t.Errorf("RetryAfter = %s, want the 90s the header asked for", limited.RetryAfter)
	}
	if !strings.Contains(err.Error(), "rate-limited") {
		t.Errorf("error %q should say plainly that Horizon rate-limited the request", err)
	}
}

// TestHorizonThrottleWithHTTPDateRetryAfter covers the other legal form of
// the header.
func TestHorizonThrottleWithHTTPDateRetryAfter(t *testing.T) {
	when := time.Now().Add(2 * time.Minute).UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", when.Format(http.TimeFormat))
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL}
	_, err := c.StrictSendPaths(context.Background(), asset.USDC(), dec(t, "100"), asset.NGNC())

	var limited *dex.ErrRateLimited
	if !errors.As(err, &limited) {
		t.Fatalf("error %q does not unwrap to ErrRateLimited", err)
	}
	if limited.RetryAfter <= 0 || limited.RetryAfter > 3*time.Minute {
		t.Errorf("RetryAfter = %s, want roughly the two minutes the date encodes",
			limited.RetryAfter)
	}
}

// TestMissingRetryAfterIsReportedNotGuessed: Horizon is free not to send the
// header, and inventing a backoff would be exactly the plausible-looking
// number this project refuses everywhere else. Zero means unknown.
func TestMissingRetryAfterIsReportedNotGuessed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL}
	_, err := c.StrictSendPaths(context.Background(), asset.USDC(), dec(t, "100"), asset.NGNC())

	var limited *dex.ErrRateLimited
	if !errors.As(err, &limited) {
		t.Fatalf("error %q does not unwrap to ErrRateLimited", err)
	}
	if limited.RetryAfter != 0 {
		t.Errorf("RetryAfter = %s, want zero when no header was sent", limited.RetryAfter)
	}
	if !strings.Contains(err.Error(), "no usable Retry-After") {
		t.Errorf("error %q should say the interval is unknown rather than imply one exists", err)
	}
}

// TestServerErrorStaysAGenericFailure keeps the boundary honest from the
// other side: a 500 must not be misread as a throttle.
func TestServerErrorStaysAGenericFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := &dex.Client{HorizonURL: srv.URL}
	_, err := c.StrictSendPaths(context.Background(), asset.USDC(), dec(t, "100"), asset.NGNC())
	if err == nil {
		t.Fatal("expected an error for HTTP 500, got none")
	}
	var limited *dex.ErrRateLimited
	if errors.As(err, &limited) {
		t.Errorf("error %q unwraps to ErrRateLimited; a broken upstream must not "+
			"read as one that is merely throttled", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q should carry the status code", err)
	}
}

// helpers --------------------------------------------------------------------

// horizonBodyForAmount finds the recorded pathfinding body for one send size.
func horizonBodyForAmount(t *testing.T, m *snapshot.Manifest, amount string) (string, []byte) {
	t.Helper()
	for _, key := range m.Keys() {
		if !strings.Contains(key, "/paths/strict-send") {
			continue
		}
		if !strings.Contains(key, "source_amount="+amount+"&") &&
			!strings.HasSuffix(key, "source_amount="+amount) {
			continue
		}
		body, ok := m.Body(key)
		if !ok {
			t.Fatalf("manifest lists key %q with no body", key)
		}
		return key, body
	}
	t.Fatalf("no recorded pathfinding response for source_amount=%s in %s", amount, m.Name())
	return "", nil
}

// bestAmountIn returns the largest destination_amount among raw records.
func bestAmountIn(t *testing.T, records []json.RawMessage) decimal.Decimal {
	t.Helper()
	best := decimal.Zero
	for _, raw := range records {
		var r struct {
			DestinationAmount string `json:"destination_amount"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatalf("parsing record: %v", err)
		}
		d, err := decimal.NewFromString(r.DestinationAmount)
		if err != nil {
			t.Fatalf("bad recorded amount %q: %v", r.DestinationAmount, err)
		}
		if d.GreaterThan(best) {
			best = d
		}
	}
	return best
}

// decFromString is a decimal literal for tests, failing loudly on a typo.
func decFromString(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	return dec(t, s)
}
