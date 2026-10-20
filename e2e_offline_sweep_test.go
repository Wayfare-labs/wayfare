package wayfare

// This file is the end-to-end join between the layers that had only unit
// tests: one scheduled sweep replayed from recorded snapshots writes to a
// temporary run store, the chain verifies, and a history-first server then
// serves those records through /api/corridor, /api/corridor/trend and
// /healthz. It is issue #756's test: the production path that broke silently
// (sweep → record → embed → serve) is exercised as one path, not as four
// independently green packages.
//
// No request leaves the process. The DEX client's transport is a snapshot
// replayer selected by destination asset, and the reference provider is a
// static map, so the test is deterministic and safe for the offline CI job.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/monitor"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
	"github.com/Wayfare-labs/wayfare/server"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// e2eTarget is one corridor in the sweep: the registry entry the monitor
// would measure, the snapshot that answers its Horizon requests, and the
// integrity the replay must produce.
type e2eTarget struct {
	corridor  monitor.Corridor
	prefix    string
	integrity string
}

// e2eTargets are registered corridors with committed snapshots, chosen to
// cover all three structural states: NGNC is DIRECT, GHSC is DERIVATIVE, and
// KESC is NO-MARKET. A replay that silently falls through to the network
// would not produce these states.
func e2eTargets(t *testing.T) []e2eTarget {
	t.Helper()

	specs := []struct {
		code      string
		prefix    string
		integrity string
	}{
		{"NGNC", "usdc-ngnc", "DIRECT"},
		{"GHSC", "usdc-ghsc", "DERIVATIVE"},
		{"KESC", "usdc-kesc", "NO-MARKET"},
	}

	out := make([]e2eTarget, 0, len(specs))
	for _, spec := range specs {
		c, ok := registeredCorridor(spec.code)
		if !ok {
			t.Fatalf("%s is not in monitor.DefaultCorridors(); the e2e sweep would measure a corridor the monitor does not", spec.code)
		}
		out = append(out, e2eTarget{
			corridor:  c,
			prefix:    spec.prefix,
			integrity: spec.integrity,
		})
	}
	return out
}

// registeredCorridor finds a destination in the monitor's sweep set.
func registeredCorridor(code string) (monitor.Corridor, bool) {
	for _, c := range monitor.DefaultCorridors() {
		if c.Receive.Code == code {
			return c, true
		}
	}
	return monitor.Corridor{}, false
}

// snapshotRouter answers each Horizon request from the snapshot for the
// destination asset in the query. A destination with no snapshot is an
// error: falling through to the network would make this test intermittently
// live, and passing that way is worse than failing.
type snapshotRouter map[string]*snapshot.Replayer

func (r snapshotRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	dest := req.URL.Query().Get("destination_assets")
	code, _, _ := strings.Cut(dest, ":")
	replay, ok := r[code]
	if !ok {
		return nil, fmt.Errorf("offline sweep: no recorded response for destination %q (%s)", dest, req.URL)
	}
	return replay.RoundTrip(req)
}

func loadSweepSnapshot(t *testing.T, prefix string) *snapshot.Manifest {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("testdata/snapshots", prefix+"-*"))
	if err != nil {
		t.Fatalf("finding snapshot %q: %v", prefix, err)
	}
	if len(matches) == 0 {
		t.Fatalf("no snapshot matching %q; the e2e sweep cannot replay this corridor", prefix)
	}
	m, err := snapshot.Load(matches[0])
	if err != nil {
		t.Fatalf("loading snapshot %s: %v", matches[0], err)
	}
	return m
}

// TestOfflineSweepRecordAndServe is issue #756's acceptance test.
func TestOfflineSweepRecordAndServe(t *testing.T) {
	ctx := context.Background()
	targets := e2eTargets(t)

	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	router := make(snapshotRouter, len(targets))
	for _, target := range targets {
		router[target.corridor.Receive.Code] = loadSweepSnapshot(t, target.prefix).Replay()
	}

	// Static reference rates keep the test offline while giving the sweep the
	// same benchmark question the production monitor asks.
	staticRef := refrate.NewStatic(map[string]decimal.Decimal{
		"USD/NGN": decimal.RequireFromString("1350.2568"),
		"USD/GHS": decimal.RequireFromString("11.0912"),
		"USD/KES": decimal.RequireFromString("129.4263"),
	})

	fixedNow := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	scheduler := &monitor.Scheduler{
		Engine: &route.Engine{
			DEX: &dex.Client{
				HorizonURL: "https://horizon.stellar.org",
				HTTPClient: &http.Client{Transport: router},
			},
			RefRate: staticRef,
		},
		Store:     store,
		Corridors: targetCorridors(targets),
		Now:       func() time.Time { return fixedNow },
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// One full scheduled sweep: every corridor is measured and appended.
	if err := scheduler.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// --- every configured corridor produced a record and a valid chain ---
	for _, target := range targets {
		key := target.corridor.Key()
		rec, err := store.Latest(ctx, key)
		if err != nil {
			t.Fatalf("reading latest %s: %v", key, err)
		}
		if rec == nil {
			t.Fatalf("sweep produced no record for registered corridor %s; the served history would have a silent hole", key)
		}
		if rec.Seq != 1 {
			t.Errorf("%s Seq = %d, want 1", key, rec.Seq)
		}
		if rec.Integrity != target.integrity {
			t.Errorf("%s Integrity = %s, want %s (snapshot replay did not reproduce the recorded state)",
				key, rec.Integrity, target.integrity)
		}
		if len(rec.Rungs) != len(route.DefaultSizes) {
			t.Errorf("%s recorded %d rungs, want %d", key, len(rec.Rungs), len(route.DefaultSizes))
		}
		if err := store.Verify(ctx, key); err != nil {
			t.Fatalf("chain verification failed for %s: %v", key, err)
		}
	}

	stored, err := store.Corridors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != len(targets) {
		t.Errorf("store holds %d corridors, sweep covered %d: %v", len(stored), len(targets), stored)
	}

	// --- serve the new records history-first ---
	//
	// Engine is deliberately nil: history-first must answer from the store,
	// so a request that reaches for a live measurement would fail loudly
	// instead of quietly hiding an empty store behind a fresh call.
	api := httptest.NewServer((&server.Server{
		Store:        store,
		HistoryFirst: true,
	}).Handler())
	t.Cleanup(api.Close)

	for _, target := range targets {
		key := target.corridor.Key()
		code := target.corridor.Receive.Code

		// /api/corridor: the same record, marked not live and aged.
		var corridorDoc route.CorridorJSON
		getJSONDoc(t, api.URL+"/api/corridor?to="+code, &corridorDoc)
		if corridorDoc.Live {
			t.Errorf("%s: /api/corridor marked a stored run as live", key)
		}
		if corridorDoc.Stale == nil {
			t.Errorf("%s: /api/corridor served a stored run without its age", key)
		}
		if corridorDoc.ReceiveAsset.Code != code {
			t.Errorf("%s: /api/corridor served receive asset %q, want %q",
				key, corridorDoc.ReceiveAsset.Code, code)
		}
		if corridorDoc.Integrity != target.integrity {
			t.Errorf("%s: /api/corridor integrity = %s, want %s",
				key, corridorDoc.Integrity, target.integrity)
		}
		if len(corridorDoc.Rungs) != len(route.DefaultSizes) {
			t.Errorf("%s: /api/corridor served %d rungs, want %d",
				key, len(corridorDoc.Rungs), len(route.DefaultSizes))
		}
		if corridorDoc.ReferenceMid == "" || corridorDoc.ReferenceSource == "" {
			t.Errorf("%s: /api/corridor lost the reference that the record carries", key)
		}

		// /api/corridor/trend: exactly the run the sweep just appended.
		var trend server.TrendJSON
		getJSONDoc(t, api.URL+"/api/corridor/trend?to="+code, &trend)
		if trend.Corridor != key {
			t.Errorf("%s: trend corridor = %q, want %q", key, trend.Corridor, key)
		}
		if trend.Count != 1 || len(trend.Runs) != 1 {
			t.Fatalf("%s: trend count = %d (runs %d), want the one run the sweep appended",
				key, trend.Count, len(trend.Runs))
		}
		if trend.Runs[0].Seq != 1 {
			t.Errorf("%s: trend run seq = %d, want 1", key, trend.Runs[0].Seq)
		}
		if trend.Runs[0].Integrity != target.integrity {
			t.Errorf("%s: trend run integrity = %s, want %s",
				key, trend.Runs[0].Integrity, target.integrity)
		}
		// Static references are single-provider, so the run has nothing to
		// diverge from. The field is still present and honestly reports an
		// empty sample rather than a fabricated zero.
		if trend.DivergenceStats.ObservationCount != 0 {
			t.Errorf("%s: divergence observations = %d, want 0 for a single-provider run",
				key, trend.DivergenceStats.ObservationCount)
		}
		if trend.DivergenceStats.Determined {
			t.Errorf("%s: divergence reported determined with no observations", key)
		}
	}

	// /healthz: the freshness block counts the new records, and the per-
	// corridor block names every one of them.
	var health struct {
		Status string `json:"status"`
		Data   map[string]struct {
			RecordedAt string `json:"recorded_at"`
			AgeSeconds int64  `json:"age_seconds"`
		} `json:"data"`
		Freshness struct {
			RecordCount    *int64  `json:"record_count"`
			NewestRecordAt *string `json:"newest_record_at"`
			ChainHead      *int64  `json:"chain_head"`
		} `json:"freshness"`
	}
	getJSONDoc(t, api.URL+"/healthz", &health)

	if health.Status != "ok" {
		t.Errorf("healthz status = %q, want ok", health.Status)
	}
	for _, target := range targets {
		key := target.corridor.Key()
		entry, ok := health.Data[key]
		if !ok {
			t.Errorf("healthz data has no %s entry; the served history omitted a swept corridor", key)
			continue
		}
		if entry.RecordedAt == "" {
			t.Errorf("healthz %s entry has no recorded_at", key)
		}
	}
	if len(health.Data) != len(targets) {
		t.Errorf("healthz data covers %d corridors, want %d", len(health.Data), len(targets))
	}
	if health.Freshness.RecordCount == nil {
		t.Fatal("healthz freshness has no record_count; the store was not read")
	}
	if got := *health.Freshness.RecordCount; got != int64(len(targets)) {
		t.Errorf("healthz record_count = %d, want %d", got, len(targets))
	}
	if health.Freshness.NewestRecordAt == nil {
		t.Error("healthz freshness has no newest_record_at")
	}
	// With no engine configured there is no chain-head source, so the field
	// must be unknown (nil), not zero: the offline server makes no claim it
	// could not measure.
	if health.Freshness.ChainHead != nil {
		t.Errorf("healthz chain_head = %d with no engine; an unknown was rendered as a value",
			*health.Freshness.ChainHead)
	}
}

func targetCorridors(targets []e2eTarget) []monitor.Corridor {
	out := make([]monitor.Corridor, len(targets))
	for i, target := range targets {
		out[i] = target.corridor
	}
	return out
}

func getJSONDoc(t *testing.T, url string, dst any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s: HTTP %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decoding %s: %v", url, err)
	}
}
