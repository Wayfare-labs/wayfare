package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestUINoscriptSummary guards issue #636: text browsers, strict privacy
// setups and crawlers get the default corridor's latest stored record
// without executing a line of script — the <noscript> block is rendered by
// html/template from the same history-first record the API serves, so every
// value is escaped and nothing is derived client-side. With no history, the
// block says so explicitly rather than inventing a figure.
func TestUINoscriptSummary(t *testing.T) {
	page := uiSource(t)

	if !strings.Contains(page, "<noscript>") || !strings.Contains(page, "</noscript>") {
		t.Fatal("page has no <noscript> block; non-JS readers see nothing")
	}
	// The summary is server-rendered, so the raw file carries template
	// actions — not literal figures.
	for _, want := range []string{
		"{{if .Summary}}",
		"{{.Summary.Corridor}}",
		"{{.Summary.Integrity}}",
		"{{.Summary.FloorLossPct}}",
		"{{.Summary.WorstLossPct}}",
		"{{.Summary.RecordedAt}}",
		"{{.Summary.Age}}",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("noscript block missing template action %s", want)
		}
	}
	if !strings.Contains(page, "No stored reading is available yet") {
		t.Error("noscript block has no explicit no-history message; an empty store would render nothing")
	}

	// The page must otherwise still embed cleanly and the handler must exist.
	if !strings.Contains(page, "function render(") {
		t.Error("embedded page lost its render() entry point; uiHandler may not be serving the UI")
	}
	if !strings.Contains(page, "og:title") {
		t.Error("embedded page lost its head metadata; head regression slipped through this file")
	}
}

// TestUIHandlerServesNoscriptSummary pins the served path: with a store
// holding a run for the default corridor, GET / renders the summary from
// that record; with no store it renders the explicit no-history message.
// Both must be valid HTML either way — the UI must never 500 because a
// summary field could not be rendered.
func TestUIHandlerServesNoscriptSummary(t *testing.T) {
	st := seedTrendStore(t, []trendSeed{
		{at: time.Now().Add(-time.Hour), integrity: "DIRECT", mid: "1500", source: "provider", loss: "0.31", verdict: "USABLE"},
	})
	api := httptest.NewServer((&Server{Store: st}).Handler())
	defer api.Close()

	res, err := http.Get(api.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading / body: %v", err)
	}
	noscript := noscriptSummary(string(body))
	if noscript == "" {
		t.Fatal("served page has no <noscript> block")
	}
	if !strings.Contains(noscript, "USDC-NGNC") {
		t.Error("noscript summary missing the corridor; the stored record was not rendered")
	}
	if !strings.Contains(noscript, "DIRECT") {
		t.Error("noscript summary missing the integrity state; the stored record was not rendered")
	}
	if strings.Contains(noscript, "{{") {
		t.Error("noscript summary still contains template actions; they were not executed")
	}
	// Escaping: any record-supplied string reaches the template escaped.
	if strings.Contains(noscript, "<script") {
		t.Error("noscript summary contains a script tag; values were not escaped")
	}

	// No store: the explicit no-history message, still 200.
	empty := httptest.NewServer((&Server{}).Handler())
	defer empty.Close()
	res2, err := http.Get(empty.URL + "/")
	if err != nil {
		t.Fatalf("GET / with no store: %v", err)
	}
	defer res2.Body.Close()
	body2, err := io.ReadAll(res2.Body)
	if err != nil {
		t.Fatalf("reading no-store / body: %v", err)
	}
	if res2.StatusCode != http.StatusOK {
		t.Errorf("GET / with no store status = %d, want 200", res2.StatusCode)
	}
	if !strings.Contains(noscriptSummary(string(body2)), "No stored reading is available yet") {
		t.Error("no-store noscript summary missing the explicit no-history message")
	}
}
