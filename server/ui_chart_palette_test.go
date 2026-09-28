package server

import (
	"strings"
	"testing"
)

// TestUIChartPaletteIsNotTheSemanticPalette guards issue #284: the line charts
// paint their data — line, points, threshold and the integrity strip — from a
// chart-specific palette, never from the verdict/semantic colours (--ok,
// --warn, --bad). A chart that painted everything in --bad said "danger" before
// it said anything; this pins the palette that replaces it, and pins that
// structural integrity states (DIRECT / DERIVATIVE / NO-MARKET) do not borrow a
// severity colour.
//
// Like the other UI contracts this is a source-text assertion on the single
// embedded page; docs/qa/README.md is the visual harness.
func TestUIChartPaletteIsNotTheSemanticPalette(t *testing.T) {
	page := uiSource(t)

	tokens := []string{
		"--chart-grid:", "--chart-line:", "--chart-point:", "--chart-threshold:",
		"--chart-direct:", "--chart-derivative:", "--chart-nomarket:",
	}
	for _, token := range tokens {
		if !strings.Contains(page, token) {
			t.Errorf("chart palette missing %q", token)
		}
	}

	// The integrity roles must be declared in the dark scheme too, so the
	// charts read in both colour schemes.
	darkMarker := "prefers-color-scheme: dark"
	darkStart := strings.Index(page, darkMarker)
	if darkStart < 0 {
		t.Fatal("no dark scheme block found")
	}
	dark := page[darkStart:]
	for _, token := range []string{"--chart-direct:", "--chart-derivative:", "--chart-nomarket:"} {
		if !strings.Contains(dark, token) {
			t.Errorf("dark scheme missing %q; the chart palette must render in both schemes", token)
		}
	}

	// Each renderer draws only from the chart/structural palette.
	for _, fence := range [][2]string{
		{"function curve(", "function table("},
		{"function trendChart(", "function runsTable("},
	} {
		start := strings.Index(page, fence[0])
		if start < 0 {
			t.Fatalf("could not find chart renderer %q", fence[0])
		}
		rel := strings.Index(page[start:], fence[1])
		if rel < 0 {
			t.Fatalf("could not find the end of chart renderer %q", fence[0])
		}
		body := page[start : start+rel]
		for _, forbidden := range []string{
			"var(--bad)", "var(--warn)", "var(--ok)",
			"var(--critical)", "var(--warning)", "var(--success)",
		} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s uses %q; chart data must not carry a verdict colour", fence[0], forbidden)
			}
		}
		if !strings.Contains(body, "var(--chart-") {
			t.Errorf("%s uses no chart token; the chart palette is not applied", fence[0])
		}
	}

	// The threshold marker is a chart role, not the verdict red.
	if strings.Contains(page, `stroke="var(--bad)"`) {
		t.Error("a chart threshold still paints with var(--bad); use var(--chart-threshold)")
	}
}
