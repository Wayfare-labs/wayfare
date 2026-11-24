package server

import (
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSocialPreviewUsesLatestRecordedCorridorData(t *testing.T) {
	at := time.Date(2026, 8, 22, 12, 34, 56, 123000000, time.UTC)
	store := seedTrendStore(t, []trendSeed{{
		at: at, integrity: "DIRECT", mid: "1350", source: "currency-api", loss: "24.75", verdict: "UNUSABLE",
	}})
	srv := trendServer(t, store)

	resp, err := http.Get(srv.URL + "/og/corridor.svg?to=NGNC")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "image/svg+xml") {
		t.Errorf("Content-Type = %q, want image/svg+xml", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading SVG: %v", err)
	}
	content := string(body)
	var image struct {
		XMLName xml.Name `xml:"svg"`
		Width   string   `xml:"width,attr"`
		Height  string   `xml:"height,attr"`
	}
	if err := xml.Unmarshal(body, &image); err != nil {
		t.Fatalf("SVG is not well-formed XML: %v", err)
	}
	if image.Width != "1200" || image.Height != "630" {
		t.Errorf("SVG dimensions = %sx%s, want 1200x630", image.Width, image.Height)
	}
	for _, want := range []string{
		"<svg ", "USDC-NGNC", "DIRECT", "24.75%", "at 0.1 USDC",
		"2026-08-22T12:34:56Z",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("SVG does not contain %q", want)
		}
	}
}

func TestSocialMetadataUsesRequestedCorridorAndStoredSnapshot(t *testing.T) {
	at := time.Date(2026, 8, 22, 12, 34, 56, 0, time.UTC)
	store := seedTrendStore(t, []trendSeed{{
		at: at, integrity: "DIRECT", mid: "1350", source: "currency-api", loss: "24.75", verdict: "UNUSABLE",
	}})
	srv := trendServer(t, store)

	resp, err := http.Get(srv.URL + "/?to=NGNC")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	page, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`property="og:title" content="USDC-NGNC | Wayfare corridor integrity"`,
		`property="og:image" content="http://`,
		`/og/corridor.svg?from=USDC&amp;to=NGNC`,
		"Recorded DIRECT integrity; floor loss 24.75% at 0.1 USDC.",
		"2026-08-22T12:34:56Z",
	} {
		if !strings.Contains(string(page), want) {
			t.Errorf("page metadata does not contain %q", want)
		}
	}
}

func TestSocialPreviewWithoutStoredHistoryIsNotFound(t *testing.T) {
	srv := trendServer(t, seedTrendStore(t, nil))
	resp, err := http.Get(srv.URL + "/og/corridor.svg?to=NGNC")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 without a recorded run", resp.StatusCode)
	}
}
