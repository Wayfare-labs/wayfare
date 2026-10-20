package server

// The history-first cold path (issue #614).
//
// A history-first deployment asked for a corridor it has never swept has three
// candidate answers, and two of them are lies. It can measure live and return
// the reading as though that were what the mode promised; or it can fail at
// the upstream and answer 502 measurement_failed, which tells the caller the
// service is broken when the only true fact is that nothing has been recorded
// yet. Neither is the finding. The honest answer is an explicit absence: this
// instance has no measurement for that corridor, and here is how to ask for
// one. That is #189's distinction — an absent record is not a corridor that
// could not be measured — applied to the request rather than to the history.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// historyFirstServer builds the deployed configuration: history-first on, a
// store that may or may not hold the corridor, and an upstream that answers,
// so a live measurement would succeed if one were attempted.
func historyFirstServer(t *testing.T, store runstore.Store) *httptest.Server {
	t.Helper()

	horizon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(liveNGNCPaths))
	}))
	t.Cleanup(horizon.Close)

	s := &Server{
		Engine: &route.Engine{
			DEX: &dex.Client{HorizonURL: horizon.URL},
			RefRate: refrate.NewStatic(map[string]decimal.Decimal{
				"USD/NGN": decimal.RequireFromString("1500"),
			}),
		},
		Store:        store,
		HistoryFirst: true,
	}
	api := httptest.NewServer(s.Handler())
	t.Cleanup(api.Close)
	return api
}

// deadHistoryFirstServer is the same deployment with an unreachable upstream,
// which is the shape that used to reach 502 measurement_failed.
func deadHistoryFirstServer(t *testing.T, store runstore.Store) *httptest.Server {
	t.Helper()

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	s := &Server{
		Engine:       newEngine(deadURL, "1500"),
		Store:        store,
		HistoryFirst: true,
	}
	api := httptest.NewServer(s.Handler())
	t.Cleanup(api.Close)
	return api
}

func emptyStore(t *testing.T) runstore.Store {
	t.Helper()
	st, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func getRaw(t *testing.T, url string) (int, map[string]any, error) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return resp.StatusCode, nil, errors.New("body is not a JSON object: " + string(raw))
	}
	return resp.StatusCode, body, nil
}

// TestColdCorridorOnHistoryFirstIsNoHistory is the issue's headline case: a
// registered destination (NGNT is in the verified registry) that has never
// been swept. The upstream would have answered, so a 200 here could only come
// from a measurement this deployment mode did not promise to make.
func TestColdCorridorOnHistoryFirstIsNoHistory(t *testing.T) {
	srv := historyFirstServer(t, emptyStore(t))

	status, body, err := getRaw(t, srv.URL+"/api/corridor?to=NGNT&sizes=100")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %v", status, body)
	}
	if code, _ := body["code"].(string); code != codeNoHistory {
		t.Errorf("code = %q, want %q", code, codeNoHistory)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "live=1") {
		t.Errorf("error prose %q does not name how to get a reading", msg)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "USDC-NGNT") {
		t.Errorf("error prose %q does not name the corridor that has no history", msg)
	}

	// The response must not resemble a measurement. This is the same rule
	// TestNoHistoryIsAnErrorNotANumber pins for the failed-live case, applied
	// to the cold path: a dashboard that parses an error as data is the
	// failure mode, and no_history is the most likely error to be mistaken
	// for an empty-but-real reading because it is the one a first deploy
	// answers with.
	for _, forbidden := range []string{
		"floor_loss_pct", "worst_loss_pct", "rungs", "finding",
		"integrity", "live", "reference_mid", "recommended",
	} {
		if _, present := body[forbidden]; present {
			t.Errorf("no_history response carries %q; it must not resemble a measurement", forbidden)
		}
	}
}

// TestColdCorridorWithDeadUpstreamsIsStillNoHistory separates the two causes
// the old behaviour collapsed. With nothing stored and an upstream that fails,
// the true first fact is that this instance has no history; measurement_failed
// would report an outage the caller never asked about and hides the one thing
// they need to know.
func TestColdCorridorWithDeadUpstreamsIsStillNoHistory(t *testing.T) {
	srv := deadHistoryFirstServer(t, emptyStore(t))

	status, body, err := getRaw(t, srv.URL+"/api/corridor?to=NGNT&sizes=100")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %v", status, body)
	}
	if code, _ := body["code"].(string); code != codeNoHistory {
		t.Errorf("code = %q, want %q rather than an outage code", code, codeNoHistory)
	}
}

// TestHistoryFirstColdCorridorWithLiveAsksAnyway is the control. Without it, a
// handler that refused every cold corridor request would pass the tests above,
// and the deployment would have no way to obtain a first reading at all.
func TestHistoryFirstColdCorridorWithLiveAsksAnyway(t *testing.T) {
	srv := historyFirstServer(t, emptyStore(t))

	status, body, err := getRaw(t, srv.URL+"/api/corridor?to=NGNC&sizes=100&live=1")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 for an explicit live request; body %v", status, body)
	}
	if body["live"] != true {
		t.Errorf("live = %v, want true: an explicit live request must be labelled as one", body["live"])
	}
	if body["rungs"] == nil {
		t.Error("a live measurement returned no rungs")
	}
}

// TestHistoryFirstWithAStoredRunIsUnchanged pins that the new branch did not
// move the warm path: a swept corridor is still served from history, labelled,
// with its age.
func TestHistoryFirstWithAStoredRunIsUnchanged(t *testing.T) {
	dir := t.TempDir()
	st := storedRun(t, dir, time.Now().UTC().Add(-6*time.Hour))

	srv := historyFirstServer(t, st)
	status, body, err := getRaw(t, srv.URL+"/api/corridor?to=NGNC&sizes=100")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 when history exists; body %v", status, body)
	}
	if body["live"] != false {
		t.Errorf("live = %v, want false for a reading served from history", body["live"])
	}
	if body["stale"] == nil {
		t.Error("a history-served reading must carry the stale envelope")
	}
	if body["floor_loss_pct"] != "25.02" {
		t.Errorf("floor_loss_pct = %v, want the stored 25.02", body["floor_loss_pct"])
	}
}

// unreadableStore answers every call from a real store except the one the cold
// path depends on: reading the chain tip fails.
type unreadableStore struct {
	runstore.Store
}

func (unreadableStore) Latest(context.Context, string) (*runstore.Record, error) {
	return nil, errors.New("chain file is unreadable")
}

// TestUnreadableStoreIsNotReportedAsNoHistory holds the line between "there is
// nothing recorded" and "I could not tell". A store that fails to answer would
// otherwise be documented — to every client switching on the code — as a
// corridor that has never been swept, which is a claim about the corridor's
// history that the server is in no position to make.
func TestUnreadableStoreIsNotReportedAsNoHistory(t *testing.T) {
	srv := historyFirstServer(t, unreadableStore{emptyStore(t)})

	status, body, err := getRaw(t, srv.URL+"/api/corridor?to=NGNC&sizes=100")
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := body["code"].(string); code == codeNoHistory {
		t.Fatalf("a store read failure was reported as no_history; absence was claimed from an error")
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 from the live measurement a failed store read falls through to; body %v",
			status, body)
	}
	if body["live"] != true {
		t.Errorf("live = %v, want true: the reading came from measuring, not from history", body["live"])
	}
}

// TestNilStoreHistoryFirstStillMeasures keeps history optional on this path
// too: an instance configured history-first with no store at all has not
// promised history, so it measures rather than answering 404 about a store it
// never opened.
func TestNilStoreHistoryFirstStillMeasures(t *testing.T) {
	srv := historyFirstServer(t, nil)

	status, body, err := getRaw(t, srv.URL+"/api/corridor?to=NGNC&sizes=100")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %v", status, body)
	}
	if body["live"] != true {
		t.Errorf("live = %v, want true with no store configured", body["live"])
	}
}

// TestUINoHistoryIsADistinctEmptyState guards the third acceptance criterion
// of #614 — that the reader sees a state of its own. A source-text assertion,
// like the other UI tests: it proves the routing exists and says which panel
// it renders, not how the panel paints (docs/qa/README.md owns that).
//
// The distinction it protects is the one the API already makes. Routing
// no_history through the shared "could not load corridor data" banner would
// tell a reader the service failed, which is precisely the reading the issue
// exists to prevent, so the branch must build an empty state and must not
// raise the banner.
func TestUINoHistoryIsADistinctEmptyState(t *testing.T) {
	raw, err := uiFS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	start := strings.Index(page, `e.code === 'no_history'`)
	if start == -1 {
		t.Fatal("the UI never inspects the no_history code; a cold corridor would render as a load failure")
	}
	end := strings.Index(page[start:], "} else {")
	if end == -1 {
		t.Fatal("the no_history branch is not the else-if of a chain; it cannot fall through to the generic error")
	}
	branch := page[start : start+end]

	for _, want := range []string{"emptyState(", "No measurement recorded yet", "Measure live"} {
		if !strings.Contains(branch, want) {
			t.Errorf("the no_history branch does not contain %q; the distinct empty state is incomplete", want)
		}
	}
	if strings.Contains(branch, "showErrorBanner(") {
		t.Error("the no_history branch raises an error banner; an absence of history must not read as an outage")
	}

	// The branch sits inside loadCorridor's catch, after the cached-reading
	// arm: a reader with a cached measurement still sees it, because showing
	// an old reading beats showing nothing.
	load := strings.Index(page, "async function loadCorridor")
	if load == -1 || start < load {
		t.Fatal("the no_history branch is not in loadCorridor, where the history-first request is made")
	}
	cached := strings.Index(page[load:start], "readCorridorReading(to)")
	if cached == -1 {
		t.Error("the no_history branch comes before the cached-reading fallback, so a cached corridor would be replaced by an empty state")
	}
}
