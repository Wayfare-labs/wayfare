package main

import (
	"context"
	"embed"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
)

//go:embed testdata/data
var testAckFS embed.FS

// testEmbeddedStore is a minimal store for testing embedded rescore behavior.
type testEmbeddedStore struct {
	records map[string][]*runstore.Record
}

func (s *testEmbeddedStore) Corridors(ctx context.Context) ([]string, error) {
	corridors := make([]string, 0, len(s.records))
	for c := range s.records {
		corridors = append(corridors, c)
	}
	return corridors, nil
}

func (s *testEmbeddedStore) All(ctx context.Context, corridor string) ([]*runstore.Record, error) {
	return s.records[corridor], nil
}

func (s *testEmbeddedStore) Append(ctx context.Context, r *runstore.Record) error {
	return nil // read-only
}

func (s *testEmbeddedStore) Latest(ctx context.Context, corridor string) (*runstore.Record, error) {
	recs := s.records[corridor]
	if len(recs) == 0 {
		return nil, nil
	}
	return recs[len(recs)-1], nil
}

func (s *testEmbeddedStore) Recent(ctx context.Context, corridor string, n int) ([]*runstore.Record, error) {
	return nil, nil
}

func (s *testEmbeddedStore) RecentBefore(ctx context.Context, corridor string, n int, before int64) ([]*runstore.Record, error) {
	return nil, nil
}

func (s *testEmbeddedStore) Verify(ctx context.Context, corridor string) error {
	return nil
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// testRecord builds a minimal USDC-NGNC record scored against a 1350 mid:
//   - 0.1: effective 1350 -> loss 0 -> GOOD
//   - 1000: effective 1349 -> loss 0.07% -> GOOD
//   - 5000: effective 1200 -> loss 11.11% -> POOR
//
// The first acceptable rung on the ascending ladder is 0.1, which is what
// the engine recommends (route/ladder.go).
func testRecord(verdicts map[string]string, integrity, recommended string) *runstore.Record {
	rungs := []runstore.Rung{
		{SendAmount: "0.1", Priced: true, Integrity: "DIRECT", ReceiveAmount: "135", EffectiveRate: "1350", LossPct: "0", Verdict: "GOOD", Path: "USDC -> XLM -> NGNC"},
		{SendAmount: "1000", Priced: true, Integrity: "DIRECT", ReceiveAmount: "1349000", EffectiveRate: "1349", LossPct: "0.07", Verdict: "GOOD", Path: "USDC -> XLM -> NGNC"},
		{SendAmount: "5000", Priced: true, Integrity: "DIRECT", ReceiveAmount: "6000000", EffectiveRate: "1200", LossPct: "11.11", Verdict: "POOR", Path: "USDC -> XLM -> NGNC"},
	}
	for i := range rungs {
		if v, ok := verdicts[rungs[i].SendAmount]; ok {
			rungs[i].Verdict = v
		}
	}
	rec := &runstore.Record{
		Version:     runstore.Version,
		Seq:         1,
		RecordedAt:  mustTime("2026-08-22T12:00:00Z"),
		Corridor:    "USDC-NGNC",
		Integrity:   integrity,
		Reference:   runstore.Reference{Mid: "1350", Source: "exchangerate-api", ScoredAgainst: "exchangerate-api"},
		Rungs:       rungs,
		Recommended: runestoneRung(recommended),
	}
	return rec
}

func runestoneRung(sendAmount string) *runstore.Rung {
	return &runstore.Rung{SendAmount: sendAmount, Priced: true, Integrity: "DIRECT", Verdict: "GOOD"}
}

func TestRecomputeVerdictMatchesStored(t *testing.T) {
	rec := testRecord(map[string]string{}, "DIRECT", "0.1")
	if drifts := rederiveRecord(rec, nil); len(drifts) != 0 {
		t.Fatalf("expected no drift for a consistent record, got %d: %+v", len(drifts), drifts)
	}
}

func TestRecomputeDetectsVerdictDrift(t *testing.T) {
	// Stored 5000 as UNUSABLE but it re-derives to POOR.
	rec := testRecord(map[string]string{"5000": "UNUSABLE"}, "DIRECT", "0.1")
	drifts := rederiveRecord(rec, nil)
	if len(drifts) != 1 {
		t.Fatalf("expected 1 drift, got %d: %+v", len(drifts), drifts)
	}
	if drifts[0].Field != "rung[5000].verdict" || drifts[0].Stored != "UNUSABLE" || drifts[0].Derived != route.VerdictPoor.String() {
		t.Errorf("unexpected drift: %+v", drifts[0])
	}
}

func TestRecomputeDetectsIntegrityDrift(t *testing.T) {
	// All stored paths route through XLM (a bridge, not fiat), so the
	// corridor re-derives as DIRECT. Storing DERIVATIVE is drift.
	rec := testRecord(map[string]string{}, "DERIVATIVE", "0.1")
	drifts := rederiveRecord(rec, nil)
	if len(drifts) != 1 {
		t.Fatalf("expected 1 drift, got %d: %+v", len(drifts), drifts)
	}
	if drifts[0].Field != "integrity" || drifts[0].Stored != "DERIVATIVE" || drifts[0].Derived != route.IntegrityDirect.String() {
		t.Errorf("unexpected drift: %+v", drifts[0])
	}
}

func TestRecomputeDetectsRecommendationDrift(t *testing.T) {
	// Stored recommended is 5000, but the first acceptable rung is 0.1.
	rec := testRecord(map[string]string{}, "DIRECT", "5000")
	drifts := rederiveRecord(rec, nil)
	if len(drifts) != 1 {
		t.Fatalf("expected 1 drift, got %d: %+v", len(drifts), drifts)
	}
	if drifts[0].Field != "recommended" || drifts[0].Stored != "5000" || drifts[0].Derived != "0.1" {
		t.Errorf("unexpected drift: %+v", drifts[0])
	}
}

func TestRecomputeAcknowledgedDriftIsMarked(t *testing.T) {
	rec := testRecord(map[string]string{"5000": "UNUSABLE"}, "DIRECT", "0.1")
	drifts := rederiveRecord(rec, nil)
	if len(drifts) != 1 {
		t.Fatalf("expected 1 drift, got %d: %+v", len(drifts), drifts)
	}
	ack := map[string]bool{"USDC-NGNC/1/rung[5000].verdict": true}
	if !acknowledged(ack, drifts[0]) {
		t.Error("acknowledgement key did not match the drift")
	}
}

func TestPathThroughFiatMakesDerivative(t *testing.T) {
	rec := &runstore.Record{
		Version:   runstore.Version,
		Seq:       1,
		Corridor:  "USDC-GHSC",
		Integrity: "DIRECT",
		Reference: runstore.Reference{Mid: "11", Source: "x", ScoredAgainst: "x"},
		Rungs: []runstore.Rung{
			{SendAmount: "0.1", Priced: true, Integrity: "DERIVATIVE", ReceiveAmount: "11", EffectiveRate: "110", LossPct: "0", Verdict: "GOOD", Path: "USDC -> NGNC -> GHSC"},
		},
		Recommended: &runstore.Rung{SendAmount: "0.1", Priced: true, Integrity: "DERIVATIVE", Verdict: "GOOD"},
	}
	drifts := rederiveRecord(rec, nil)
	if len(drifts) != 1 {
		t.Fatalf("expected 1 integrity drift, got %d: %+v", len(drifts), drifts)
	}
	if drifts[0].Derived != route.IntegrityDerivative.String() {
		t.Errorf("expected DERIVATIVE, got %q", drifts[0].Derived)
	}
}

func TestNoPricedRungIsNoMarket(t *testing.T) {
	rec := &runstore.Record{
		Version:   runstore.Version,
		Seq:       1,
		Corridor:  "USDC-KESC",
		Integrity: "DIRECT",
		Reference: runstore.Reference{Mid: "130", Source: "x", ScoredAgainst: "x"},
		Rungs: []runstore.Rung{
			{SendAmount: "0.1", Priced: false, Integrity: "NO-MARKET"},
		},
	}
	drifts := rederiveRecord(rec, nil)
	if len(drifts) != 1 {
		t.Fatalf("expected 1 integrity drift, got %d: %+v", len(drifts), drifts)
	}
	if drifts[0].Derived != route.IntegrityNoMarket.String() {
		t.Errorf("expected NO-MARKET, got %q", drifts[0].Derived)
	}
}

// TestEmbeddedRescoreAcknowledgedDriftReturnsZero tests that when using an
// embedded store (dataDir == "") with an acknowledged drift in the embedded
// acknowledgements file, rescore returns exit code 0.
func TestEmbeddedRescoreAcknowledgedDriftReturnsZero(t *testing.T) {
	// Create a record with a known drift: 5000 stored as UNUSABLE but derives to POOR.
	rec := testRecord(map[string]string{"5000": "UNUSABLE"}, "DIRECT", "0.1")

	store := &testEmbeddedStore{
		records: map[string][]*runstore.Record{
			"USDC-NGNC": {rec},
		},
	}

	// Load acknowledgements from the test embedded FS (which acknowledges the drift).
	ack, err := loadAcknowledgementsFromTestFS("", testAckFS)
	if err != nil {
		t.Fatalf("loading acknowledgements: %v", err)
	}

	// Verify the acknowledgement is loaded.
	if !ack["USDC-NGNC/1/rung[5000].verdict"] {
		t.Fatal("acknowledgement not loaded from embedded FS")
	}

	// Run rescore logic directly to verify exit code 0.
	ctx := context.Background()
	corridors, err := store.Corridors(ctx)
	if err != nil {
		t.Fatalf("listing corridors: %v", err)
	}

	var drifts []drift
	checked := 0
	for _, corridor := range corridors {
		records, err := store.All(ctx, corridor)
		if err != nil {
			t.Fatalf("reading corridor history: %v", err)
		}
		for _, rec := range records {
			checked++
			drifts = append(drifts, rederiveRecord(rec, ack)...)
		}
	}

	unacknowledged := 0
	for _, d := range drifts {
		if acknowledged(ack, d) {
			continue
		}
		unacknowledged++
	}

	if unacknowledged > 0 {
		t.Fatalf("expected 0 unacknowledged drifts, got %d", unacknowledged)
	}
	if checked != 1 {
		t.Fatalf("expected 1 checked record, got %d", checked)
	}
}

// loadAcknowledgementsFromTestFS reads acknowledgements from the test embedded FS.
func loadAcknowledgementsFromTestFS(dataDir string, embeddedFS embed.FS) (map[string]bool, error) {
	ack := map[string]bool{}
	var data []byte
	var err error
	if dataDir == "" {
		data, err = embeddedFS.ReadFile("testdata/data/rescore-acknowledgements.json")
	} else {
		// Not used in this test
		return ack, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return ack, nil
	}
	var list []acknowledgement
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	for _, e := range list {
		ack[ackKey(e.Corridor, e.Seq, e.Field)] = true
	}
	return ack, nil
}
