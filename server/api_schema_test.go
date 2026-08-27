package server

// API contract tests. The existing api_test.go covers routing and status
// codes; this file pins the wire shape those codes hide: the JSON the endpoint
// actually returns. A client parses these bodies, so a drift in field set,
// a decimal rendered as a bare number, or an error that stops resembling the
// others is the kind of silent breakage these tests exist to catch (the
// response-schema test gap, issue #68).
//
// They cover what api_test.go deliberately does not:
//   - every JSON endpoint advertises application/json
//   - the corridor response carries every field of the wire schema
//   - /api/assets returns the sorted supported-asset list with corridor flags
//   - error responses share one shape ({"error": ...}), never a measurement
//   - money crosses the wire as quoted strings, at every level of a response

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// jsonBody does a GET, asserts the response advertises application/json, and
// returns the raw body. Content-Type is the contract: a handler that answers
// text/plain breaks every JSON client at once, and this is the only way to
// catch it.
func jsonBody(t *testing.T, url string) []byte {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("%s: Content-Type = %q, want application/json", url, ct)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return raw
}

// objectFields decodes raw JSON into a map keyed by top-level field, values
// kept as raw messages so a table test can assert quoting per field.
func objectFields(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	return m
}

// requireQuoted asserts a field exists and that its raw JSON starts with a
// double quote, i.e. it is a string rather than a bare number.
func requireQuoted(t *testing.T, m map[string]json.RawMessage, field string) {
	t.Helper()
	raw, ok := m[field]
	if !ok {
		t.Errorf("%s is missing from the response", field)
		return
	}
	if !strings.HasPrefix(string(raw), `"`) {
		t.Errorf("%s = %s, want a quoted string so a client cannot parse it as a number", field, raw)
	}
}

// cutString decodes a json.RawMessage expected to be a string and returns it,
// returning "" if the field is absent or not a string.
func cutString(m map[string]json.RawMessage, key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// requireArray asserts a field exists and is a JSON array.
func requireArray(t *testing.T, m map[string]json.RawMessage, field string) {
	t.Helper()
	raw, ok := m[field]
	if !ok {
		t.Errorf("%s is missing from the response", field)
		return
	}
	if !strings.HasPrefix(string(raw), "[") {
		t.Errorf("%s = %s, want an array", field, raw)
	}
}

// requireObject asserts a field exists and is a JSON object.
func requireObject(t *testing.T, m map[string]json.RawMessage, field string) map[string]json.RawMessage {
	t.Helper()
	raw, ok := m[field]
	if !ok {
		t.Errorf("%s is missing from the response", field)
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Errorf("%s = %s, want an object", field, raw)
		return nil
	}
	return obj
}

// content type pins ----------------------------------------------------------

// TestCorridorJSONContentType pins the headline endpoint's content type.
func TestCorridorJSONContentType(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")
	jsonBody(t, srv.URL+"/api/corridor?to=NGNC&sizes=100")
}

// TestHealthzJSONContentType pins the health endpoint's content type.
func TestHealthzJSONContentType(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")
	jsonBody(t, srv.URL+"/healthz")
}

// TestAssetsJSONContentType pins the assets endpoint's content type.
func TestAssetsJSONContentType(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")
	jsonBody(t, srv.URL+"/api/assets")
}

// TestErrorResponseContentType pins that even an error response is JSON, so a
// client can rely on one decoder for the whole endpoint.
func TestErrorResponseContentType(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")
	resp, err := http.Get(srv.URL + "/api/corridor?to=SCAMC")
	if err != nil {
		t.Fatalf("GET corridor (unknown asset): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("error Content-Type = %q, want application/json", ct)
	}
}

// error shape -----------------------------------------------------------------

// TestErrorResponseShape is the contract that unknown corridors look like every
// other API error: a single "error" string, and nothing that could be mistaken
// for a measurement.
func TestErrorResponseShape(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")
	resp, err := http.Get(srv.URL + "/api/corridor?to=SCAMC")
	if err != nil {
		t.Fatalf("GET corridor (unknown asset): %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}

	// Exactly one key: "error". A measurement-looking body on an error path
	// would let an SRE dashboard accidentally parse it as data.
	if len(body) != 1 {
		t.Errorf("error body has %d keys, want exactly 1: %v", len(body), body)
	}
	raw, ok := body["error"]
	if !ok {
		t.Fatal("error body must carry the error key")
	}
	if !strings.HasPrefix(string(raw), `"`) {
		t.Errorf("error = %s, want a string message", raw)
	}

	// Never a measurement-shaped field alongside the error.
	for _, forbidden := range []string{
		"integrity", "rungs", "floor_loss_pct", "reference_mid", "finding", "live",
	} {
		if _, present := body[forbidden]; present {
			t.Errorf("error response carries %q; it must not resemble a measurement", forbidden)
		}
	}
}

// corridor shape --------------------------------------------------------------

// TestCorridorResponseShapeFull pins the whole top-level field set of a live
// corridor response against the wire schema (route.CorridorJSON). Every field
// a client could read must be present in the body, not silently dropped by an
// omitempty or a serialisation bug. Presence is the contract; exact values are
// pinned by the narrower existing tests (TestGoodCorridorIsRecommended etc.).
func TestCorridorResponseShapeFull(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "660")
	body := objectFields(t, jsonBody(t, srv.URL+"/api/corridor?to=NGNC&sizes=100"))

	// Every key on route.CorridorJSON that is emitted on a healthy corridor.
	for _, field := range []string{
		"send_asset", "receive_asset", "integrity", "depends_on",
		"reference_mid", "reference_source", "reference_pair",
		"reference_agreement", "scored",
		"floor_loss_pct", "floor_size", "worst_loss_pct", "worst_size",
		"recommended", "finding", "rungs", "measured_at",
		"live",
	} {
		if _, ok := body[field]; !ok {
			t.Errorf("corridor response is missing required field %q", field)
		}
	}

	// The live control: a fresh measurement must not be labelled stale.
	if s := string(body["live"]); s != "true" {
		t.Errorf("live = %s, want true for a fresh measurement", s)
	}

	// Assets are objects carrying at least a code.
	if sa := requireObject(t, body, "send_asset"); sa != nil && cutString(sa, "code") == "" {
		t.Error("send_asset is missing its code")
	}
	if ra := requireObject(t, body, "receive_asset"); ra != nil && cutString(ra, "code") == "" {
		t.Error("receive_asset is missing its code")
	}

	// Collections are arrays, never absent on a live corridor.
	requireArray(t, body, "depends_on")
	requireArray(t, body, "rungs")
}

// TestCorridorRungShape pins the per-rung shape of a priced corridor: a rung
// carries a send_amount, a price flag, an integrity state, notes, and a quoted
// money value when priced.
func TestCorridorRungShape(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")
	body := objectFields(t, jsonBody(t, srv.URL+"/api/corridor?to=NGNC&sizes=1,100"))

	rungsRaw, ok := body["rungs"]
	if !ok {
		t.Fatal("rungs missing from response")
	}
	var rungs []map[string]json.RawMessage
	if err := json.Unmarshal(rungsRaw, &rungs); err != nil {
		t.Fatalf("decoding rungs: %v", err)
	}
	if len(rungs) != 2 {
		t.Fatalf("expected 2 rungs, got %d", len(rungs))
	}

	for i, rung := range rungs {
		if s := cutString(rung, "send_amount"); s == "" {
			t.Errorf("rung %d is missing a quoted send_amount", i)
		}
		if _, ok := rung["priced"]; !ok {
			t.Errorf("rung %d is missing the priced flag", i)
		}
		if s := cutString(rung, "integrity"); s == "" {
			t.Errorf("rung %d is missing its integrity state", i)
		}
		requireArray(t, rung, "notes")

		if priced := string(rung["priced"]); priced == "true" {
			q := requireObject(t, rung, "quote")
			if q == nil {
				continue
			}
			if s := cutString(q, "source"); s == "" {
				t.Errorf("rung %d quote is missing its source", i)
			}
			for _, f := range []string{"receive_amount", "effective_rate", "loss_pct"} {
				requireQuoted(t, q, f)
			}
		}
	}
}

// assets shape ----------------------------------------------------------------

// TestAssetsEndpointShape pins the /api/assets contract: a top-level "assets"
// array in deterministic code order, where every entry carries code, issuer and
// the corridor flag, and the settlement asset is the only non-corridor entry.
func TestAssetsEndpointShape(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")
	body := objectFields(t, jsonBody(t, srv.URL+"/api/assets"))

	assetsRaw, ok := body["assets"]
	if !ok {
		t.Fatal("assets response is missing the assets array")
	}
	var assets []map[string]json.RawMessage
	if err := json.Unmarshal(assetsRaw, &assets); err != nil {
		t.Fatalf("decoding assets: %v", err)
	}
	if len(assets) == 0 {
		t.Fatal("assets list is empty; the API must advertise its supported assets")
	}

	prev := ""
	for i, e := range assets {
		code, issuer := cutString(e, "code"), cutString(e, "issuer")
		if code == "" {
			t.Errorf("asset %d is missing its code", i)
		}
		if issuer == "" {
			t.Errorf("asset %s is missing its issuer", code)
		}
		if prev != "" && code <= prev {
			t.Errorf("assets out of order: %q after %q (KnownCodes must sort)", code, prev)
		}
		prev = code

		// can_be_destination is the corridor flag: false only for the
		// settlement asset everyone starts from.
		cb, ok := e["can_be_destination"]
		if !ok {
			t.Errorf("asset %s is missing the can_be_destination flag", code)
			continue
		}
		got := string(cb)
		if code == "USDC" {
			if got != "false" {
				t.Errorf("USDC.can_be_destination = %s, want false (it is the settlement asset)", got)
			}
		} else if got != "true" {
			t.Errorf("%s.can_be_destination = %s, want true for a corridor token", code, got)
		}
	}
}

// decimals as strings ---------------------------------------------------------

// TestMoneyIsQuotedEverywhere broadens the decimal-as-string guard beyond the
// three headline fields in api_test.go: every numeric figure a client would
// reach for must be a quoted string, at the top of the document and inside
// rungs and quotes. A single JSON number invites a client to parse money into
// a float64 — the rounding bug the project refuses.
func TestMoneyIsQuotedEverywhere(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "1500")
	body := objectFields(t, jsonBody(t, srv.URL+"/api/corridor?to=NGNC&sizes=100"))

	for _, field := range []string{
		"reference_mid", "reference_divergence_pct", "floor_loss_pct",
		"floor_size", "worst_loss_pct", "worst_size", "recommended_size",
	} {
		if _, ok := body[field]; !ok {
			// Optional fields can be absent (omitempty); only assert when
			// present. Every field that is present must be quoted.
			continue
		}
		requireQuoted(t, body, field)
	}

	rungsRaw, ok := body["rungs"]
	if !ok {
		t.Fatal("rungs missing from response")
	}
	var rungs []map[string]json.RawMessage
	if err := json.Unmarshal(rungsRaw, &rungs); err != nil {
		t.Fatalf("decoding rungs: %v", err)
	}
	for _, rung := range rungs {
		requireQuoted(t, rung, "send_amount")
		if q := requireObject(t, rung, "quote"); q != nil {
			requireQuoted(t, q, "receive_amount")
			requireQuoted(t, q, "effective_rate")
			requireQuoted(t, q, "loss_pct")
		}
	}
}
