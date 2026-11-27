package monitor

import (
	"context"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/failover"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fixedReference struct {
	name string
	mid  string
}

func (r fixedReference) Name() string { return r.name }

func (r fixedReference) Rate(_ context.Context, base, quote string) (refrate.Rate, error) {
	return refrate.Rate{
		Base: base, Quote: quote, Mid: decimal.RequireFromString(r.mid), Source: r.name,
	}, nil
}

// schedulerOver builds a scheduler measuring one corridor from a snapshot, so
// the whole test runs offline.
func schedulerOver(t *testing.T, prefix string, recv asset.Asset, quote, mid string, store runstore.Store) *Scheduler {
	t.Helper()

	snap := loadSnapshotFixture(t, prefix)

	return &Scheduler{
		Engine: &route.Engine{
			DEX: &dex.Client{
				HorizonURL: "https://horizon.stellar.org",
				HTTPClient: snap.HTTPClient(),
			},
			RefRate: refrate.NewStatic(map[string]decimal.Decimal{
				"USD/" + quote: decimal.RequireFromString(mid),
			}),
		},
		Store: store,
		Corridors: []Corridor{{
			Send: asset.USDC(), Receive: recv,
			ReferenceBase: "USD", ReferenceQuote: quote,
		}},
		Logger: quiet(),
	}
}

// snapshotHTTPClient loads the snapshot a schedulerOver call would use and
// returns its replaying client, so a test can point a checks runner at the same
// recording.
func snapshotHTTPClient(t *testing.T, prefix string) *http.Client {
	t.Helper()
	return loadSnapshotFixture(t, prefix).HTTPClient()
}

// TestRunOnceWritesOneRecordPerCorridor is the scheduler's basic contract.
func TestRunOnceWritesOneRecordPerCorridor(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", store)

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	rec, err := store.Latest(context.Background(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("RunOnce wrote no record")
	}
	if rec.Seq != 1 {
		t.Errorf("Seq = %d, want 1", rec.Seq)
	}
	if rec.Integrity != "DIRECT" {
		t.Errorf("Integrity = %s, want DIRECT", rec.Integrity)
	}
	if rec.Recommended != nil {
		t.Error("a corridor measured at a heavy loss must record no recommendation")
	}
	if rec.FloorLossPct == "" || rec.WorstLossPct == "" {
		t.Error("the record should carry the floor and worst figures")
	}
	if rec.Reference.ScoredAgainst == "" {
		t.Error("the record must name which mid produced its verdicts")
	}
}

func TestRunOnceDoesNotRecordUnscoredLossesOrScoringSource(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", store)
	s.Engine.RefRate = &refrate.Cross{
		Primary:   fixedReference{"primary", "1350.2568"},
		Secondary: fixedReference{"secondary", "135.02568"},
	}

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	rec, err := store.Latest(context.Background(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("RunOnce wrote no record")
	}
	if rec.Reference.ScoredAgainst != "" {
		t.Errorf("ScoredAgainst = %q, want empty for an unscored benchmark", rec.Reference.ScoredAgainst)
	}
	if rec.FloorLossPct != "" || rec.WorstLossPct != "" {
		t.Errorf("unscored summary persisted loss values: floor=%q worst=%q",
			rec.FloorLossPct, rec.WorstLossPct)
	}
	for i, rung := range rec.Rungs {
		if rung.LossPct != "" || rung.Verdict != "UNKNOWN" {
			t.Errorf("unscored rung %d persisted loss=%q verdict=%q", i, rung.LossPct, rung.Verdict)
		}
	}
}

func TestSchedulerLoadsIssuerDriftBaselineFromLatestRecord(t *testing.T) {
	ctx := context.Background()
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := checks.IssuerDriftBaseline{
		AuthRequired:  true,
		AuthImmutable: true,
		TOMLHash:      strings.Repeat("a", 64),
	}
	record := &runstore.Record{
		Corridor: "USDC-NGNC",
		Checks: []checks.CheckJSON{{
			ID: "issuer.drift",
			Evidence: []checks.EvidenceJSON{{
				Source:   "https://horizon.stellar.org/accounts/issuer → drift-check",
				Observed: "auth_required=true auth_revocable=false auth_clawback_enabled=false auth_immutable=true toml_sha256=" + want.TOMLHash,
			}},
		}},
	}
	if err := store.Append(ctx, record); err != nil {
		t.Fatal(err)
	}

	scheduler := &Scheduler{Store: store, Logger: quiet()}
	got := scheduler.issuerDriftBaseline(ctx, "USDC-NGNC")
	if got == nil || *got != want {
		t.Fatalf("baseline = %#v, want %#v", got, want)
	}
}

// TestRunOnceRecordsMetrics is the second acceptance criterion of #577: a
// scheduled sweep must store its metrics, not only serve them live. The
// snapshot these tests replay predates metrics, so the recorded set is
// undetermined — what is asserted is that the record carries it, with a reason,
// rather than dropping it and making "measured, could not determine"
// indistinguishable from "never measured".
func TestRunOnceRecordsMetrics(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", store)
	// The runner replays the same snapshot, so the sweep stays offline even
	// though the default metric set reads the order book.
	s.Checks = &checks.Runner{HTTPClient: snapshotHTTPClient(t, "usdc-ngnc")}

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	rec, err := store.Latest(context.Background(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("RunOnce wrote no record")
	}
	if len(rec.Metrics) == 0 {
		t.Fatal("the stored record carries no metrics; a scheduled sweep measured " +
			"nothing but still reported a findings block")
	}
	for _, m := range rec.Metrics {
		if m.ID == "" {
			t.Error("a stored metric has no ID; the record is not attributable")
		}
		if !m.Determined && m.Reason == "" {
			t.Errorf("stored metric %q is undetermined with no reason", m.ID)
		}
	}
}

func TestRunOnceStoresSnapshotAndLinksItsHash(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	snap := loadSnapshotFixture(t, "usdc-ngnc")
	artifactDir := t.TempDir()
	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", store)
	s.SnapshotDir = artifactDir
	s.SnapshotEngineFactory = func(recorder *snapshot.Recorder) *route.Engine {
		recorder.Transport = snap.Replay()
		client := &http.Client{Transport: recorder}
		return &route.Engine{
			DEX: &dex.Client{
				HorizonURL: "https://horizon.stellar.org",
				HTTPClient: client,
			},
			RefRate: refrate.NewStatic(map[string]decimal.Decimal{
				"USD/NGN": decimal.RequireFromString("1350.2568"),
			}),
		}
	}
	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	rec, err := store.Latest(context.Background(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil || !strings.HasPrefix(rec.Snapshot, "sha256:") {
		t.Fatalf("run record snapshot = %q, want a content hash", rec.Snapshot)
	}
	path := filepath.Join(artifactDir, strings.TrimPrefix(rec.Snapshot, "sha256:"))
	if got, err := snapshot.ContentHash(path); err != nil || got != rec.Snapshot {
		t.Errorf("stored artifact hash = %q, %v; want %q", got, err, rec.Snapshot)
	}
	if err := store.VerifySnapshots(context.Background(), "USDC-NGNC", artifactDir); err != nil {
		t.Errorf("VerifySnapshots: %v", err)
	}
}

// TestRunOnceRecordsTheAnsweringHorizon is issue #701's recording half. A sweep
// answered by the fallback host has to publish the fallback's address: the
// artifact is what a reader verifies a published figure against, and one that
// names a host which never served the bytes is a dead end.
//
// Both branches are here because the same three lines of wiring are wrong if
// either one misreports — a note on a healthy run would accuse a working
// provider of an outage it did not have.
func TestRunOnceRecordsTheAnsweringHorizon(t *testing.T) {
	const configured = "https://horizon.stellar.org"

	tests := []struct {
		name        string
		primaryDown bool
	}{
		{name: "the configured host is down", primaryDown: true},
		{name: "the configured host answers", primaryDown: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store, err := runstore.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			fixture := loadSnapshotFixture(t, "usdc-ngnc")
			failoverHost := replayHost(t, fixture.Replay())

			tr, err := failover.New(configured, failoverHost.URL, outagedPrimary{
				want:   configured,
				down:   tc.primaryDown,
				replay: fixture.Replay(),
			})
			if err != nil {
				t.Fatalf("failover.New: %v", err)
			}

			artifactDir := t.TempDir()
			s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", store)
			s.SnapshotDir = artifactDir
			s.SnapshotSources = snapshot.Sources{
				Horizon: snapshot.SourceRef{BaseURL: configured},
			}
			s.SnapshotNotes = []string{"configured primary: " + configured}
			s.SnapshotEngineFactory = func(recorder *snapshot.Recorder) *route.Engine {
				// Beneath the recorder, exactly as cmd/wayfared wires it: the
				// archive must hold the bytes that answered and not the
				// primary's discarded refusal.
				recorder.Transport = tr
				return &route.Engine{
					DEX:     &dex.Client{HorizonURL: configured, HTTPClient: &http.Client{Transport: recorder}},
					RefRate: refrate.NewStatic(map[string]decimal.Decimal{"USD/NGN": decimal.RequireFromString("1350.2568")}),
				}
			}

			if err := s.RunOnce(context.Background()); err != nil {
				t.Fatalf("RunOnce: %v", err)
			}

			rec, err := store.Latest(context.Background(), "USDC-NGNC")
			if err != nil {
				t.Fatal(err)
			}
			if rec == nil || !strings.HasPrefix(rec.Snapshot, "sha256:") {
				t.Fatalf("run record snapshot = %q, want a content hash", rec.Snapshot)
			}
			manifest, err := snapshot.Load(filepath.Join(artifactDir, strings.TrimPrefix(rec.Snapshot, "sha256:")))
			if err != nil {
				t.Fatalf("loading the recorded manifest: %v", err)
			}

			wantHost := configured
			if tc.primaryDown {
				wantHost = failoverHost.URL
			}
			if got := manifest.Sources.Horizon.BaseURL; got != wantHost {
				t.Errorf("manifest sources.horizon.base_url = %q, want %q", got, wantHost)
			}

			// The interaction URL is the other half of the same promise, and it
			// is the one a body-level audit reads: a manifest that named the
			// fallback host while each interaction still claimed the primary
			// would describe a run that did not happen.
			horizonCall, ok := manifest.FirstOfKind(snapshot.KindHorizon)
			if !ok {
				t.Fatal("the manifest recorded no Horizon interaction")
			}
			if !strings.HasPrefix(horizonCall.URL, wantHost) {
				t.Errorf("recorded Horizon interaction URL = %q, want it aimed at %q", horizonCall.URL, wantHost)
			}
			// The refusal the failover exists to survive must not be archived as
			// ground truth for the query the second host answered.
			if horizonCall.Status != http.StatusOK {
				t.Errorf("recorded Horizon interaction status = %d, want the answer that was kept", horizonCall.Status)
			}

			// Matched by its prefix rather than the whole sentence: the failure
			// worth catching on a healthy run is a note that accuses some
			// provider of an outage it did not have, and comparing one composed
			// string against another would let a note naming the wrong host pass.
			lines := notesWithPrefix(manifest.Notes, "answered by the Horizon failover host: ")
			if !tc.primaryDown {
				if len(lines) != 0 {
					t.Errorf("a run the configured host answered recorded %v", lines)
				}
			} else if len(lines) != 1 || lines[0] != failoverHost.URL {
				t.Errorf("failover notes = %v, want one naming %q (%v)", lines, failoverHost.URL, manifest.Notes)
			}
			// Per-corridor notes must not leak into the scheduler's shared set,
			// which every later corridor in the same sweep reads.
			if len(s.SnapshotNotes) != 1 {
				t.Errorf("Scheduler.SnapshotNotes grew to %d entries: %v", len(s.SnapshotNotes), s.SnapshotNotes)
			}
		})
	}
}

// notesWithPrefix returns what follows the prefix on each note carrying it, so
// a test can assert on the host a note names instead of on a sentence it
// composes itself.
func notesWithPrefix(notes []string, prefix string) []string {
	var out []string
	for _, note := range notes {
		if rest, found := strings.CutPrefix(note, prefix); found {
			out = append(out, rest)
		}
	}
	return out
}

// loadSnapshotFixture reads the recorded run a schedulerOver call replays.
func loadSnapshotFixture(t *testing.T, prefix string) *snapshot.Manifest {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("../testdata/snapshots", prefix+"-*"))
	if err != nil || len(matches) == 0 {
		t.Skipf("no snapshot matching %q", prefix)
	}
	snap, err := snapshot.Load(matches[0])
	if err != nil {
		t.Fatalf("loading snapshot: %v", err)
	}
	return snap
}

// replayHost serves a recorded snapshot's answers over real HTTP on a loopback
// address, so a test can put a second Horizon behind an address without any
// fixture knowing it. Replay matches on method, path and query alone, which is
// what makes the same bytes answer from a different host.
func replayHost(t *testing.T, replayer http.RoundTripper) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, err := replayer.RoundTrip(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// outagedPrimary is the configured Horizon as the failover transport meets it.
// It answers from the recording rather than dialling the address it pretends to
// own, because a healthy primary that reaches the real network would make an
// offline suite depend on a public provider being up. Requests for any other
// origin belong to the failover host, which is a loopback address this test
// started, and go to the real transport.
type outagedPrimary struct {
	want   string
	down   bool
	replay http.RoundTripper
}

func (o outagedPrimary) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme+"://"+req.URL.Host != o.want {
		return http.DefaultTransport.RoundTrip(req)
	}
	if !o.down {
		return o.replay.RoundTrip(req)
	}
	return &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader("horizon is down")),
		Request:    req,
	}, nil
}

// TestRunOnceIsIdempotentUnderReRun checks that running twice appends two
// records that chain, rather than rewriting or duplicating history.
func TestRunOnceIsIdempotentUnderReRun(t *testing.T) {
	dir := t.TempDir()
	store, err := runstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", store)

	for i := 0; i < 3; i++ {
		if err := s.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce %d: %v", i, err)
		}
	}

	recent, err := store.Recent(context.Background(), "USDC-NGNC", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 3 {
		t.Fatalf("got %d records after 3 runs, want 3", len(recent))
	}
	// Same corridor, same snapshot: the measurements must agree.
	for _, r := range recent {
		if r.Integrity != recent[0].Integrity || r.FloorLossPct != recent[0].FloorLossPct {
			t.Errorf("re-running the same snapshot produced a different measurement: %s/%s vs %s/%s",
				r.Integrity, r.FloorLossPct, recent[0].Integrity, recent[0].FloorLossPct)
		}
	}
	if err := store.Verify(context.Background(), "USDC-NGNC"); err != nil {
		t.Errorf("chain does not verify after repeated runs: %v", err)
	}
}

func TestSchedulerAppendConcurrentWithFileStoreReaders(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	scheduler := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", store)

	const readerCount = 4
	const sweepCount = 4
	start := make(chan struct{})
	ready := make(chan struct{}, readerCount)
	done := make(chan struct{})
	var readers sync.WaitGroup
	readers.Add(readerCount)
	for i := 0; i < readerCount; i++ {
		go func() {
			defer readers.Done()
			<-start
			assertReadableRunstore(t, store)
			ready <- struct{}{}
			for {
				select {
				case <-done:
					return
				default:
					assertReadableRunstore(t, store)
				}
			}
		}()
	}
	close(start)
	for i := 0; i < readerCount; i++ {
		<-ready
	}
	for i := 0; i < sweepCount; i++ {
		if err := scheduler.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce %d: %v", i, err)
		}
	}
	close(done)
	readers.Wait()

	all, err := store.All(context.Background(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != sweepCount {
		t.Fatalf("All returned %d records, want %d", len(all), sweepCount)
	}
	if err := store.Verify(context.Background(), "USDC-NGNC"); err != nil {
		t.Errorf("chain after concurrent scheduler writes does not verify: %v", err)
	}
}

func assertReadableRunstore(t *testing.T, store runstore.Store) {
	t.Helper()
	ctx := context.Background()

	latest, err := store.Latest(ctx, "USDC-NGNC")
	if err != nil {
		t.Errorf("Latest: %v", err)
	} else if latest != nil {
		if err := latest.VerifySelf(); err != nil {
			t.Errorf("Latest returned a partial record: %v", err)
		}
	}

	recent, err := store.Recent(ctx, "USDC-NGNC", 3)
	if err != nil {
		t.Errorf("Recent: %v", err)
	} else {
		for i, record := range recent {
			if err := record.VerifySelf(); err != nil {
				t.Errorf("Recent record %d is partial: %v", i, err)
			}
			if i > 0 && (recent[i-1].Seq != record.Seq+1 || recent[i].Hash != recent[i-1].PrevHash) {
				t.Errorf("Recent records are not a complete descending chain: seq %d then %d",
					recent[i-1].Seq, record.Seq)
			}
		}
	}

	all, err := store.All(ctx, "USDC-NGNC")
	if err != nil {
		t.Errorf("All: %v", err)
		return
	}
	for i, record := range all {
		if err := record.VerifySelf(); err != nil {
			t.Errorf("All record %d is partial: %v", i, err)
		}
		if i > 0 && record.PrevHash != all[i-1].Hash {
			t.Errorf("All has a broken link between seq %d and %d", all[i-1].Seq, record.Seq)
		}
	}
}

// TestNoMarketCorridorIsRecorded checks that a corridor with no market still
// produces a record. It is a finding, and a history that skipped it would show
// a gap where a real result belongs.
func TestNoMarketCorridorIsRecorded(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := schedulerOver(t, "usdc-kesc", asset.KESC(), "KES", "129.4263", store)

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	rec, err := store.Latest(context.Background(), "USDC-KESC")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("no record written for a no-market corridor")
	}
	if rec.Integrity != "NO-MARKET" {
		t.Errorf("Integrity = %s, want NO-MARKET", rec.Integrity)
	}
}

// TestDerivativeDependencyIsRecorded pins that the dependency survives into
// history, since #24 compares it across runs.
func TestDerivativeDependencyIsRecorded(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := schedulerOver(t, "usdc-ghsc", asset.GHSC(), "GHS", "11.0912", store)

	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	rec, err := store.Latest(context.Background(), "USDC-GHSC")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Integrity != "DERIVATIVE" {
		t.Fatalf("Integrity = %s, want DERIVATIVE", rec.Integrity)
	}
	if len(rec.DependsOn) != 1 || rec.DependsOn[0] != "NGNC" {
		t.Errorf("DependsOn = %v, want [NGNC]; #24 needs this to detect a "+
			"dependency change that an integrity comparison alone would miss",
			rec.DependsOn)
	}
}

// TestSchedulerRunsWithoutAStore pins that history is optional. The scheduler
// must be runnable with nothing configured, which is also how it behaves if a
// volume fails to mount.
func TestSchedulerRunsWithoutAStore(t *testing.T) {
	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", nil)
	if err := s.RunOnce(context.Background()); err != nil {
		t.Errorf("RunOnce with no store: %v", err)
	}
}

// TestRunStopsOnContextCancel checks the shutdown path, so a deploy does not
// hang waiting for a ticker.
func TestRunStopsOnContextCancel(t *testing.T) {
	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", nil)
	s.Interval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// The first sweep runs immediately, so a freshly started monitor has
	// data without waiting a full interval.
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Run should return the context error on cancel")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

// TestRunSweepsImmediately checks the first measurement does not wait for one
// full interval, which on a six-hour cadence would mean a freshly deployed
// instance served nothing all morning.
func TestRunSweepsImmediately(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := schedulerOver(t, "usdc-ngnc", asset.NGNC(), "NGN", "1350.2568", store)
	s.Interval = time.Hour
	s.Jitter = time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec, _ := store.Latest(context.Background(), "USDC-NGNC")
		if rec != nil {
			return // measured well inside the one-hour interval
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("no measurement recorded; the first sweep waited for the interval")
}

func TestSchedulerJitterIsDeterministicAndBounded(t *testing.T) {
	seed := int64(42)
	scheduler := &Scheduler{
		Interval:   6 * time.Hour,
		Jitter:     5 * time.Minute,
		JitterSeed: &seed,
	}
	first := scheduler.randomSource()
	second := scheduler.randomSource()
	var varied bool
	for i := 0; i < 10; i++ {
		got := scheduler.nextInterval(first)
		replay := scheduler.nextInterval(second)
		if got != replay {
			t.Fatalf("seeded interval %d = %s, replay = %s", i, got, replay)
		}
		if got < scheduler.Interval || got >= scheduler.Interval+scheduler.Jitter {
			t.Errorf("interval %s is outside [%s, %s)", got, scheduler.Interval, scheduler.Interval+scheduler.Jitter)
		}
		varied = varied || got > scheduler.Interval
	}
	if !varied {
		t.Fatal("seeded jitter never added an offset")
	}
}

func TestSchedulerZeroJitterKeepsBaseInterval(t *testing.T) {
	scheduler := &Scheduler{Interval: 3 * time.Hour}
	if got := scheduler.nextInterval(rand.New(rand.NewSource(1))); got != scheduler.Interval {
		t.Errorf("interval with zero jitter = %s, want %s", got, scheduler.Interval)
	}
}

// TestSchedulerNeedsAnEngine guards the obvious misconfiguration.
func TestSchedulerNeedsAnEngine(t *testing.T) {
	s := &Scheduler{Logger: quiet()}
	if err := s.RunOnce(context.Background()); err == nil {
		t.Error("expected an error with no engine configured")
	}
	if err := s.Run(context.Background()); err == nil {
		t.Error("expected Run to refuse with no engine configured")
	}
}

// TestDefaultCorridorsCoverEveryRegisteredFiatDestination verifies that the
// default sweep includes every registered non-USD fiat-pegged destination.
func TestDefaultCorridorsCoverEveryRegisteredFiatDestination(t *testing.T) {
	got := DefaultCorridors()
	gotKeys := make(map[string]bool, len(got))
	for _, corridor := range got {
		key := corridor.Key()
		if gotKeys[key] {
			t.Errorf("default corridors contain duplicate %s", key)
		}
		gotKeys[key] = true
		if corridor.ReferenceBase != "USD" || corridor.ReferenceQuote == "" {
			t.Errorf("%s has invalid reference pair %q/%q", key, corridor.ReferenceBase, corridor.ReferenceQuote)
		}
	}
	// Pending assets remain included: their declared status is a finding.
	for _, entry := range asset.Registry() {
		if entry.Peg == "" || entry.Peg == "USD" {
			continue // settlement asset or same-peg swap (issue #671)
		}
		key := "USDC-" + entry.Code
		if !gotKeys[key] {
			t.Errorf("default corridors omit registered fiat destination %s", key)
		}
		delete(gotKeys, key)
	}
	for unexpected := range gotKeys {
		t.Errorf("default corridors include unregistered or excluded destination %s", unexpected)
	}
}
