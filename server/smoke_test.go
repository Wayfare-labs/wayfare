package server

// The post-deploy smoke check, as a Go test.
//
// CI's smoke job (scripts/smoke-check.sh against the real container) proves
// the deployment answers; this test proves the same assertions about the wire
// shape hold on the handler itself, offline, on every `go test` run. It hits
// the real routes through s.Handler() with a stubbed upstream, so a
// regression in the corridor document, the health answer, the asset list or
// the embedded UI fails the normal suite as well as the smoke job.
//
// Nothing here assumes freshness: the corridor assertions read whatever
// `live` the document states, and the history-first variant asserts the stale
// envelope a stored reading must carry. The checks qualify the headline; they
// never move a verdict or an integrity state, and nothing is synthesised to
// fill a gap.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSmokeHealthReportsOK(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "660")

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding /healthz: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("/healthz status = %q, want %q", body["status"], "ok")
	}
}

func TestSmokeServesTheMonitorUI(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "660")

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	page, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading /: %v", err)
	}
	html := string(page)
	if !strings.Contains(html, "Corridor integrity monitor") {
		t.Error("the served UI is not the corridor monitor page")
	}
	if !strings.Contains(html, "Wayfare is non-custodial") {
		t.Error("the served UI is missing the provenance footer")
	}
}

func TestSmokeCorridorAnswersWithTheWireShape(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "660")

	status, body := getJSON(t, srv.URL+"/api/corridor?to=NGNC&sizes=100")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %v", status, body)
	}

	// live must always be present: its absence cannot be readable as
	// freshness by a smoke check or any other client.
	live, present := body["live"]
	if !present {
		t.Fatal("the live field must always be present")
	}

	if finding, _ := body["finding"].(string); finding == "" {
		t.Error("finding is empty; a served document must state a finding")
	}
	if mid, _ := body["reference_mid"].(string); mid == "" {
		t.Error("reference_mid is empty")
	}
	if src, _ := body["reference_source"].(string); src == "" {
		t.Error("reference_source is empty")
	}

	rungs, ok := body["rungs"].([]any)
	if !ok || len(rungs) == 0 {
		t.Fatal("rungs must be a non-empty list: a measurement tests sizes")
	}
	for i, r := range rungs {
		rung, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("rung %d is not an object", i)
		}
		if _, ok := rung["send_amount"]; !ok {
			t.Errorf("rung %d lacks send_amount", i)
		}
		priced, ok := rung["priced"].(bool)
		if !ok {
			t.Errorf("rung %d lacks the priced flag", i)
		}
		// A priced rung must carry its quote, and the quote its loss figure;
		// an unpriced rung must not invent one.
		quote, hasQuote := rung["quote"]
		if priced {
			q, ok := quote.(map[string]any)
			if !ok || !hasQuote {
				t.Errorf("priced rung %d lacks a quote", i)
			} else if _, ok := q["loss_pct"]; !ok {
				t.Errorf("priced rung %d has a quote without loss_pct", i)
			}
		} else if hasQuote && quote != nil {
			t.Errorf("unpriced rung %d carries a quote", i)
		}
	}

	// recommended must be present even when null, so a client can never miss
	// the refusal to recommend.
	if _, present := body["recommended"]; !present {
		t.Error("the recommended field must always be present")
	}
	if rec, ok := body["recommended"].(map[string]any); ok {
		if _, ok := body["recommended_size"]; !ok {
			t.Error("a recommendation must be paired with its size")
		}
		if _, ok := rec["loss_pct"]; !ok {
			t.Error("a recommendation must carry its loss figure")
		}
	}

	// This test served a live measurement (no store): it must not pretend to
	// be history.
	if live != true {
		t.Errorf("live = %v, want true for a direct engine measurement", live)
	}
}

func TestSmokeAssetsListKnownAssets(t *testing.T) {
	srv := testServer(t, liveNGNCPaths, "660")

	status, body := getJSON(t, srv.URL+"/api/assets")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %v", status, body)
	}
	assets, ok := body["assets"].([]any)
	if !ok || len(assets) == 0 {
		t.Fatal("/api/assets must list the known assets")
	}
}
