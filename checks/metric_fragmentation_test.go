package checks

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// The figures below are pinned against the live USDC -> NGNC recording of
// 2026-08-21 (testdata/snapshots/usdc-ngnc-20260821T223040Z at the repository
// root), not against this package's own output. Each expected HHI and top
// share was computed independently from the recorded destination amounts as
// Σv²/(Σv)² and max(v)/Σv in exact decimal arithmetic, then rounded to 12
// places — so an implementation that summed rounded shares, dropped a path,
// counted a duplicate, or weighted by anything but the destination amount
// would fail here rather than agree with itself.

// loadNGNCLadderRecording loads the repository's recorded USDC -> NGNC
// ladder, which carries Horizon's full strict-send answer — every path, not
// only the best — at each of the twelve ladder sizes.
func loadNGNCLadderRecording(t *testing.T) *snapshot.Manifest {
	t.Helper()
	m, err := snapshot.Load(filepath.Join("..", "testdata", "snapshots", "usdc-ngnc-20260821T223040Z"))
	if err != nil {
		t.Fatalf("loading recorded USDC -> NGNC ladder: %v", err)
	}
	return m
}

func recordedDEX(m *snapshot.Manifest) *dex.Client {
	return &dex.Client{HorizonURL: "https://horizon.stellar.org", HTTPClient: m.HTTPClient()}
}

func fragDecimals(ss ...string) []decimal.Decimal {
	out := make([]decimal.Decimal, len(ss))
	for i, s := range ss {
		out[i] = decimal.RequireFromString(s)
	}
	return out
}

func TestConcentrationOfPaths(t *testing.T) {
	tests := []struct {
		name     string
		values   []decimal.Decimal
		count    int
		total    string
		topShare string
		hhi      string
	}{
		{
			// One path is a measurement, not an unknown: Horizon answered
			// and every unit of executable value it found is on that path.
			name:     "one path holds all of it",
			values:   fragDecimals("651.0013796"),
			count:    1,
			total:    "651.0013796",
			topShare: "1",
			hhi:      "1",
		},
		{
			name:     "two equal paths",
			values:   fragDecimals("50", "50"),
			count:    2,
			total:    "100",
			topShare: "0.5",
			hhi:      "0.5",
		},
		{
			// 1/3 to the fixed scale: one final rounding, not three
			// rounded shares summed (which would end ...26666…).
			name:     "three equal paths",
			values:   fragDecimals("7", "7", "7"),
			count:    3,
			total:    "21",
			topShare: "0.3333333333333333",
			hhi:      "0.3333333333333333",
		},
		{
			name:     "one dominant path",
			values:   fragDecimals("99", "1"),
			count:    2,
			total:    "100",
			topShare: "0.99",
			hhi:      "0.9802",
		},
		{
			// Order does not matter: the best path need not come first.
			name:     "best path listed last",
			values:   fragDecimals("1", "99"),
			count:    2,
			total:    "100",
			topShare: "0.99",
			hhi:      "0.9802",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ConcentrationOfPaths(tc.values)
			if err != nil {
				t.Fatalf("ConcentrationOfPaths: %v", err)
			}
			if got.PathCount != tc.count {
				t.Errorf("PathCount = %d, want %d", got.PathCount, tc.count)
			}
			if !got.Total.Equal(decimal.RequireFromString(tc.total)) {
				t.Errorf("Total = %s, want %s", got.Total, tc.total)
			}
			if !got.TopShare.Equal(decimal.RequireFromString(tc.topShare)) {
				t.Errorf("TopShare = %s, want %s", got.TopShare, tc.topShare)
			}
			if !got.HHI.Equal(decimal.RequireFromString(tc.hhi)) {
				t.Errorf("HHI = %s, want %s", got.HHI, tc.hhi)
			}
		})
	}
}

// TestConcentrationOfPathsRefusesToInvent pins the unknown cases: no paths
// is not a concentration of zero, and a non-positive amount is not a path
// that pays nothing.
func TestConcentrationOfPathsRefusesToInvent(t *testing.T) {
	tests := []struct {
		name   string
		values []decimal.Decimal
		isNone bool
	}{
		{name: "nil", values: nil, isNone: true},
		{name: "empty", values: []decimal.Decimal{}, isNone: true},
		{name: "zero amount", values: fragDecimals("10", "0")},
		{name: "negative amount", values: fragDecimals("-1", "10")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ConcentrationOfPaths(tc.values)
			if err == nil {
				t.Fatalf("ConcentrationOfPaths(%v) = %+v, want an error", tc.values, got)
			}
			if tc.isNone && !errors.Is(err, ErrNoPathValues) {
				t.Errorf("err = %v, want ErrNoPathValues", err)
			}
			if !got.HHI.IsZero() || got.PathCount != 0 {
				t.Errorf("an error must come with no figure, got %+v", got)
			}
		})
	}
}

func TestFragmentationMetricFromRecordedLadder(t *testing.T) {
	m := loadNGNCLadderRecording(t)

	tests := []struct {
		size     string
		paths    int
		total    string
		topShare string // 12 places
		hhi      string // 12 places
	}{
		// Four paths within 3% of each other: value is dispersed.
		{size: "1", paths: 4, total: "3813.7867084", topShare: "0.251822682528", hhi: "0.250025437074"},
		// Two near-equal paths at the dust size.
		{size: "0.1", paths: 2, total: "193.7385783", topShare: "0.500339808161", hhi: "0.500000230939"},
		// Two bridged paths close together, the direct one far behind.
		{size: "100", paths: 3, total: "144737.5227964", topShare: "0.428581231828", hhi: "0.387748760608"},
		// The XLM-bridged path delivers most of the value.
		{size: "5000", paths: 2, total: "194620.9611679", topShare: "0.865750127238", hhi: "0.767546311149"},
	}
	for _, tc := range tests {
		t.Run("size="+tc.size, func(t *testing.T) {
			metric := FragmentationMetric{DEX: recordedDEX(m), Size: decimal.RequireFromString(tc.size)}
			r := RunMetric(ctx(), metric, Subject{Send: asset.USDC(), Receive: asset.NGNC()})

			if !r.Determined {
				t.Fatalf("undetermined on a recorded multi-path response: %s", r.Reason)
			}
			if r.ID != FragmentationMetricID {
				t.Errorf("ID = %q, want %q", r.ID, FragmentationMetricID)
			}
			if r.Unit != UnitRatio {
				t.Errorf("Unit = %s, want ratio", r.Unit)
			}
			if r.Venue != VenuePathfinding {
				t.Errorf("Venue = %q, want %q", r.Venue, VenuePathfinding)
			}
			if got := r.Value.StringFixed(12); got != tc.hhi {
				t.Errorf("HHI = %s, want %s", got, tc.hhi)
			}
			if len(r.Evidence) != 1 {
				t.Fatalf("evidence = %+v, want one item", r.Evidence)
			}
			ev := r.Evidence[0]
			if !strings.Contains(ev.Source, "/paths/strict-send USDC/NGNC source_amount="+tc.size) {
				t.Errorf("evidence source = %q, want it to name the endpoint, pair and size", ev.Source)
			}
			for _, want := range []string{
				"paths=" + strconv.Itoa(tc.paths) + ",",
				"duplicates_dropped=0",
				"total_dest=" + tc.total,
				"top_share=" + decimal.RequireFromString(tc.topShare).StringFixed(6),
				"hhi=" + decimal.RequireFromString(tc.hhi).StringFixed(6),
			} {
				if !strings.Contains(ev.Observed, want) {
					t.Errorf("evidence observed = %q, want it to contain %q", ev.Observed, want)
				}
			}
		})
	}
}

// TestFragmentationRisesWithSizeOnRecordedLadder is the structural finding
// the recording carries: at 1 USDC four paths pay within 3% of each other,
// and by 5000 the XLM-bridged path delivers most of the value. Path count
// alone cannot show this — 1 USDC has four paths and 100 has three, a
// difference of one — while the value-weighted figure moves from 0.25 to
// 0.77.
func TestFragmentationRisesWithSizeOnRecordedLadder(t *testing.T) {
	m := loadNGNCLadderRecording(t)
	var prev decimal.Decimal
	for i, size := range []string{"1", "100", "5000"} {
		r := RunMetric(ctx(), FragmentationMetric{DEX: recordedDEX(m), Size: decimal.RequireFromString(size)},
			Subject{Send: asset.USDC(), Receive: asset.NGNC()})
		if !r.Determined {
			t.Fatalf("size %s undetermined: %s", size, r.Reason)
		}
		if i > 0 && !r.Value.GreaterThan(prev) {
			t.Errorf("HHI at %s = %s, want it above the previous size's %s", size, r.Value, prev)
		}
		prev = r.Value
	}
}

// TestFragmentationMetricMeasuresDerivativeCorridors pins that DERIVATIVE is
// not short-circuited the way the book metrics short-circuit it: pathfinding
// reaches a derivative corridor end to end, and how many ways it does is the
// fragility question.
func TestFragmentationMetricMeasuresDerivativeCorridors(t *testing.T) {
	m := loadNGNCLadderRecording(t)
	r := RunMetric(ctx(), FragmentationMetric{DEX: recordedDEX(m), Size: decimal.NewFromInt(100)},
		Subject{Send: asset.USDC(), Receive: asset.NGNC(), Integrity: integrityDerivative})
	if !r.Determined {
		t.Fatalf("DERIVATIVE corridor undetermined: %s", r.Reason)
	}
}

// TestFragmentationMetricUnknownIsNeverZero covers every way the figure can
// be unobtainable. Each must come back undetermined with a reason — never a
// determined zero (which would read as perfectly dispersed) or one (a single
// path).
func TestFragmentationMetricUnknownIsNeverZero(t *testing.T) {
	ngnc := loadNGNCLadderRecording(t)
	empty := loadOrderBookSnapshot(t, "usdc-ngnc-strictsend-empty")
	pair := Subject{Send: asset.USDC(), Receive: asset.NGNC()}

	tests := []struct {
		name    string
		metric  FragmentationMetric
		subject Subject
		reason  string
	}{
		{
			name:    "Horizon returned no path",
			metric:  FragmentationMetric{DEX: recordedDEX(empty), Size: decimal.NewFromInt(1)},
			subject: pair,
			reason:  "unknown, not zero",
		},
		{
			name:    "NO-MARKET by construction",
			metric:  FragmentationMetric{DEX: recordedDEX(ngnc)},
			subject: Subject{Send: asset.USDC(), Receive: asset.NGNC(), Integrity: integrityNoMarket},
			reason:  "NO-MARKET",
		},
		{
			// A size the recording never saw: the replayer refuses rather
			// than reaching the network, and the metric must report the
			// failure, not a figure.
			name:    "pathfinding failed",
			metric:  FragmentationMetric{DEX: recordedDEX(ngnc), Size: decimal.NewFromInt(7)},
			subject: pair,
			reason:  "pathfinding failed",
		},
		{
			name:    "no DEX client",
			metric:  FragmentationMetric{},
			subject: pair,
			reason:  "no DEX client",
		},
		{
			name:    "no pair",
			metric:  FragmentationMetric{DEX: recordedDEX(ngnc)},
			subject: Subject{},
			reason:  "no send or receive asset",
		},
		{
			name:    "negative size",
			metric:  FragmentationMetric{DEX: recordedDEX(ngnc), Size: decimal.NewFromInt(-5)},
			subject: pair,
			reason:  "not positive",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := RunMetric(ctx(), tc.metric, tc.subject)
			if r.Determined {
				t.Fatalf("determined (value %s) where the figure is unobtainable", r.Value)
			}
			if !strings.Contains(r.Reason, tc.reason) {
				t.Errorf("reason = %q, want it to contain %q", r.Reason, tc.reason)
			}
			if r.ID != FragmentationMetricID {
				t.Errorf("ID = %q, want %q", r.ID, FragmentationMetricID)
			}

			// On the wire an unknown carries no value at all, so no
			// consumer can read a zero into it.
			f := &Findings{}
			f.AddMetric(r)
			wire := f.ToJSON().Metrics
			if len(wire) != 1 || wire[0].Determined || wire[0].Value != "" {
				t.Errorf("wire metric = %+v, want undetermined with no value", wire)
			}
		})
	}
}

func TestDistinctPathValues(t *testing.T) {
	usdc, ngnc := asset.USDC(), asset.NGNC()
	aqua := asset.Stellar("AQUA", "GBNZILSTVQZ4R7IKQDGHYGY2QXL5QOFJYQMXPKWRRM5PAV7Y4M67AQUA")
	// Same code, different issuer: a different asset, so a different path.
	fakeAqua := asset.Stellar("AQUA", "GAXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX")
	path := func(amount string, hops ...asset.Asset) dex.Path {
		return dex.Path{
			SourceAsset:  usdc,
			SourceAmount: decimal.NewFromInt(1),
			DestAsset:    ngnc,
			DestAmount:   decimal.RequireFromString(amount),
			Hops:         hops,
		}
	}

	tests := []struct {
		name       string
		paths      []dex.Path
		values     []string
		duplicates int
		wantErr    string
	}{
		{
			name:   "direct and bridged are distinct",
			paths:  []dex.Path{path("960", asset.Native()), path("936")},
			values: []string{"960", "936"},
		},
		{
			name:   "same code from another issuer is another path",
			paths:  []dex.Path{path("959", aqua), path("958", fakeAqua)},
			values: []string{"959", "958"},
		},
		{
			name:       "an exact repeat is one path",
			paths:      []dex.Path{path("959", aqua), path("959", aqua), path("936")},
			values:     []string{"959", "936"},
			duplicates: 1,
		},
		{
			name:    "a repeat with a different amount is a contradiction",
			paths:   []dex.Path{path("959", aqua), path("900", aqua)},
			wantErr: "listed twice with different destination amounts",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			values, duplicates, err := distinctPathValues(tc.paths)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("distinctPathValues: %v", err)
			}
			if duplicates != tc.duplicates {
				t.Errorf("duplicates = %d, want %d", duplicates, tc.duplicates)
			}
			if len(values) != len(tc.values) {
				t.Fatalf("values = %v, want %v", values, tc.values)
			}
			for i := range values {
				if !values[i].Equal(decimal.RequireFromString(tc.values[i])) {
					t.Errorf("values[%d] = %s, want %s", i, values[i], tc.values[i])
				}
			}
		})
	}
}

// TestFragmentationMetricWireValueIsTheDecimal pins that the stored and
// served figure is the decimal string of the HHI, so a history reader parses
// back exactly what was measured.
func TestFragmentationMetricWireValueIsTheDecimal(t *testing.T) {
	m := loadNGNCLadderRecording(t)
	r := RunMetric(ctx(), FragmentationMetric{DEX: recordedDEX(m), Size: decimal.NewFromInt(5000)},
		Subject{Send: asset.USDC(), Receive: asset.NGNC()})
	if !r.Determined {
		t.Fatalf("undetermined: %s", r.Reason)
	}
	f := &Findings{}
	f.AddMetric(r)
	b, err := json.Marshal(f.ToJSON())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"value":"`+r.Value.String()+`"`) {
		t.Errorf("wire JSON %s does not carry value %s as a decimal string", b, r.Value)
	}
	if !strings.Contains(string(b), `"id":"fragmentation.paths"`) {
		t.Errorf("wire JSON %s does not carry the metric id", b)
	}
}
