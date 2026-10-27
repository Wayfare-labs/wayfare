package route

import (
	"strings"
	"testing"
	"time"
)

func TestCorridorImageSVGShapes(t *testing.T) {
	d := CorridorJSON{
		SendAsset:       AssetJSON{Code: "USDC"},
		ReceiveAsset:    AssetJSON{Code: "NGNC", Peg: "NGN"},
		Integrity:       "DIRECT",
		Floor:           "27.15",
		FloorSize:       "0.1",
		WorstLoss:       "97.52",
		WorstSize:       "5000",
		ReferenceMid:    "1349.67",
		ReferenceSource: "exchangerate-api",
		MeasuredAt:      "2026-10-19T14:08:00Z",
		Live:            true,
	}

	img := NewCorridorImage(d, time.Now())
	svg := img.SVG()

	if !strings.Contains(svg, `<?xml version="1.0"`) {
		t.Error("SVG missing XML declaration")
	}
	if !strings.Contains(svg, `width="1200" height="630"`) {
		t.Error("SVG missing expected preview dimensions")
	}
	if !strings.Contains(svg, "WAYFARE — CORRIDOR SNAPSHOT") {
		t.Error("SVG missing document header")
	}
	if !strings.Contains(svg, "USDC → NGNC (NGN)") {
		t.Errorf("SVG missing corridor title: %s", svg)
	}
	if !strings.Contains(svg, "DIRECT") {
		t.Error("SVG missing integrity badge")
	}
	if !strings.Contains(svg, "27.15%") {
		t.Error("SVG missing floor loss")
	}
	if !strings.Contains(svg, "at 0.1 send") {
		t.Error("SVG missing floor size")
	}
	if !strings.Contains(svg, "97.52%") {
		t.Error("SVG missing worst loss")
	}
	if !strings.Contains(svg, "at 5000 send") {
		t.Error("SVG missing worst size")
	}
	if !strings.Contains(svg, "1349.67") {
		t.Error("SVG missing reference mid")
	}
	if !strings.Contains(svg, "exchangerate-api") {
		t.Error("SVG missing reference source")
	}
	if !strings.Contains(svg, "recorded Oct 19 14:08 UTC") {
		t.Errorf("SVG missing recorded time: %s", svg)
	}
}

func TestCorridorImageRecordsStaleTimeBeforeMeasuredAt(t *testing.T) {
	d := CorridorJSON{
		SendAsset:       AssetJSON{Code: "USDC"},
		ReceiveAsset:    AssetJSON{Code: "NGNC"},
		Integrity:       "DIRECT",
		Floor:           "10.00",
		FloorSize:       "0.1",
		WorstLoss:       "20.00",
		WorstSize:       "100",
		ReferenceMid:    "1000",
		ReferenceSource: "static",
		MeasuredAt:      "2026-10-19T14:08:00Z",
		Stale: &StaleJSON{
			RecordedAt: "2026-10-18T09:00:00Z",
		},
	}

	img := NewCorridorImage(d, time.Now())
	svg := img.SVG()

	if !strings.Contains(svg, "recorded Oct 18 09:00 UTC") {
		t.Errorf("SVG should prefer stale recorded_at; got: %s", svg)
	}
	if strings.Contains(svg, "recorded Oct 19") {
		t.Errorf("SVG should not show measured_at when stale exists: %s", svg)
	}
}

func TestCorridorImageRecordsEmptyTimestampWhenNone(t *testing.T) {
	d := CorridorJSON{
		SendAsset:       AssetJSON{Code: "USDC"},
		ReceiveAsset:    AssetJSON{Code: "NGNC"},
		Integrity:       "DIRECT",
		Floor:           "10.00",
		FloorSize:       "0.1",
		WorstLoss:       "20.00",
		WorstSize:       "100",
		ReferenceMid:    "1000",
		ReferenceSource: "static",
	}

	img := NewCorridorImage(d, time.Now())
	if img.RecordedAt != "" {
		t.Errorf("recorded time should be empty when the document has none: %q", img.RecordedAt)
	}

	svg := img.SVG()
	if !strings.Contains(svg, "recorded time unavailable") {
		t.Errorf("SVG should say recorded time unavailable: %s", svg)
	}
}

func TestCorridorImageEscaping(t *testing.T) {
	d := CorridorJSON{
		SendAsset:       AssetJSON{Code: "USDC"},
		ReceiveAsset:    AssetJSON{Code: "NGNC"},
		Integrity:       "<SCRIPT>",
		Floor:           "1.00",
		FloorSize:       "1",
		WorstLoss:       "2.00",
		WorstSize:       "1",
		ReferenceMid:    "1<2",
		ReferenceSource: "a<b>c&\"d",
	}

	img := NewCorridorImage(d, time.Now())
	svg := img.SVG()

	for _, dangerous := range []string{"<SCRIPT>", "a<b>c&\"d", "1<2"} {
		if strings.Contains(svg, dangerous) {
			t.Errorf("SVG must escape %q; svg=%s", dangerous, svg)
		}
	}
	if !strings.Contains(svg, "&lt;SCRIPT&gt;") {
		t.Error("SVG should escape a bad integrity value")
	}
	if !strings.Contains(svg, "a&lt;b&gt;c&amp;&quot;d") {
		t.Errorf("SVG should escape the reference source: %s", svg)
	}
}

func TestCorridorImageDependencyLine(t *testing.T) {
	d := CorridorJSON{
		SendAsset:       AssetJSON{Code: "USDC"},
		ReceiveAsset:    AssetJSON{Code: "GHSC"},
		Integrity:       "DERIVATIVE",
		DependsOn:       []AssetJSON{{Code: "NGNC"}, {Code: "KESC"}},
		Floor:           "50.00",
		FloorSize:       "0.1",
		WorstLoss:       "80.00",
		WorstSize:       "5000",
		ReferenceMid:    "10",
		ReferenceSource: "exchangerate-api",
		MeasuredAt:      "2026-10-19T10:00:00Z",
	}

	img := NewCorridorImage(d, time.Now())
	svg := img.SVG()

	if !strings.Contains(svg, "depends on NGNC, KESC") {
		t.Errorf("SVG missing dependency line: %s", svg)
	}
}

func TestCorridorImageEmptyDependencyLine(t *testing.T) {
	d := CorridorJSON{
		SendAsset:       AssetJSON{Code: "USDC"},
		ReceiveAsset:    AssetJSON{Code: "NGNC"},
		Integrity:       "DIRECT",
		DependsOn:       []AssetJSON{},
		Floor:           "10.00",
		FloorSize:       "0.1",
		WorstLoss:       "20.00",
		WorstSize:       "100",
		ReferenceMid:    "1000",
		ReferenceSource: "static",
		MeasuredAt:      "2026-10-19T10:00:00Z",
	}

	img := NewCorridorImage(d, time.Now())
	svg := img.SVG()

	if strings.Contains(svg, "depends on") {
		t.Errorf("SVG should not render a dependency line when there are none: %s", svg)
	}
}

func TestCorridorImageUsesReceivePegInTitle(t *testing.T) {
	cases := []struct {
		name string
		recv AssetJSON
		want string
	}{
		{"with peg", AssetJSON{Code: "NGNC", Peg: "NGN"}, "USDC → NGNC (NGN)"},
		{"without peg", AssetJSON{Code: "NGNC"}, "USDC → NGNC"},
		{"unknown peg omitted", AssetJSON{Code: "NGNC", Peg: ""}, "USDC → NGNC"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := CorridorJSON{
				SendAsset:       AssetJSON{Code: "USDC"},
				ReceiveAsset:    c.recv,
				Integrity:       "DIRECT",
				Floor:           "10.00",
				FloorSize:       "0.1",
				WorstLoss:       "20.00",
				WorstSize:       "100",
				ReferenceMid:    "1000",
				ReferenceSource: "static",
				MeasuredAt:      "2026-10-19T10:00:00Z",
			}
			img := NewCorridorImage(d, time.Now())
			if img.Title != c.want {
				t.Errorf("title = %q, want %q", img.Title, c.want)
			}
		})
	}
}

func TestCorridorImageRoundsTripThroughAssetCodeOnly(t *testing.T) {
	// An image built from a document that carries only codes must not invent
	// an issuer or asset string; the renderer should survive that path.
	d := CorridorJSON{
		SendAsset:       AssetJSON{Code: "USDC"},
		ReceiveAsset:    AssetJSON{Code: "NGNC"},
		Integrity:       "DIRECT",
		Floor:           "10.00",
		FloorSize:       "0.1",
		WorstLoss:       "20.00",
		WorstSize:       "100",
		ReferenceMid:    "1000",
		ReferenceSource: "static",
		MeasuredAt:      "2026-10-19T10:00:00Z",
		Live:            false,
		Stale: &StaleJSON{
			RecordedAt: "2026-10-19T10:00:00Z",
		},
	}

	img := NewCorridorImage(d, time.Now())
	svg := img.SVG()

	if !strings.Contains(svg, "USDC → NGNC") {
		t.Errorf("SVG missing corridor title: %s", svg)
	}
	if strings.Contains(svg, "stellar:") {
		t.Errorf("SVG must not invent a wire asset form from code-only input: %s", svg)
	}
}
