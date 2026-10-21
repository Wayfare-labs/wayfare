package server

import (
	"strings"
	"testing"
)

// TestUIMarketStructureView guards issue #631:
// UI renders /api/market-structure with corridor count, concentrated corridor count,
// issuer concentrations table, and distinct empty and undetermined states.
func TestUIMarketStructureView(t *testing.T) {
	page := uiHTML(t)

	// Market structure button exists in controls
	if !strings.Contains(page, `id="market-structure"`) {
		t.Error("missing Market structure button in controls")
	}

	// Button click wires to loadMarketStructure
	for _, want := range []string{
		`$('market-structure').addEventListener('click', loadMarketStructure)`,
		`fetch('/api/market-structure')`,
		`renderMarketStructure(data)`,
		`renderMarketStructure(d)`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing market structure interaction %q", want)
		}
	}

	// Renders corridor counts, concentrated corridors, and unknown fallback
	for _, want := range []string{
		`d.corridor_count`,
		`d.concentrated_corridor_count`,
		`d.issuer_concentrations`,
		`<span class="m-state">UNDETERMINED</span>`,
		`Concentrated Corridors`,
		`Issuer Account`,
		`emptyState({`,
		`No concentrated issuers`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing market structure view element %q", want)
		}
	}
}

// TestUIChainHeadsVerification guards issue #632:
// Show the corridor's current chain head (/api/chain-heads) next to recorded readings,
// linking to docs/verify-store.md, copyable, and rendering null/missing heads as unknown.
func TestUIChainHeadsVerification(t *testing.T) {
	page := uiHTML(t)

	for _, want := range []string{
		`/api/chain-heads`,
		`fetchChainHeads`,
		`renderChainHeadBlock`,
		`docs/verify-store.md`,
		`How to verify`,
		`copyText(`,
		`Copy hash`,
		`<span class="m-state">UNDETERMINED</span>`,
		`Chain head unknown for this corridor`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing chain heads verification element %q", want)
		}
	}

	// Verify chain head is rendered when !d.live (recorded / cached)
	if !strings.Contains(page, `(!d.live) ? renderChainHeadBlock(d) : ''`) {
		t.Error("chain head block is not conditionally rendered for recorded readings")
	}
}

// TestUIDownloadLadderCSVAndJSON guards issue #633:
// Adds buttons to download displayed corridor ladder as CSV (with exact decimal wire strings)
// and raw JSON response.
func TestUIDownloadLadderCSVAndJSON(t *testing.T) {
	page := uiHTML(t)

	// Buttons exist in measurements panel
	for _, want := range []string{
		`id="download-csv"`,
		`id="download-json"`,
		`downloadLadderCSV()`,
		`downloadLadderJSON()`,
		`Download CSV`,
		`Download JSON`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing ladder download button %q", want)
		}
	}

	// CSV download maintains exact decimal wire strings without float conversion
	for _, want := range []string{
		`currentCorridorReading = d;`,
		`r.send_amount`,
		`q.receive_amount`,
		`q.effective_rate`,
		`q.loss_pct`,
		`q.verdict`,
		`text/csv;charset=utf-8;`,
		`application/json;charset=utf-8;`,
		`.download =`,
		`-ladder.csv`,
		`-ladder.json`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("UI missing download implementation detail %q", want)
		}
	}
}
