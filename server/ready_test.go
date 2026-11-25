package server

// Readiness contract tests (issue #704).
//
// `/readyz` answers one question a platform acts on: should this process
// receive traffic? `/healthz` answers a different one — is it up, and how old
// is its data — and those two came apart in the case the issue was written
// for: a wayfared that could not verify the history it carries logs a warning,
// falls back to an empty store, and then reports itself healthy while serving
// readings nobody checked. Liveness stays true through that; readiness must
// not.
//
// So these tests pin three things: the two gates each fail on their own, the
// states that refuse traffic are distinguishable in the body, and `/healthz`
// does not change when `/readyz` says no — the separation is the feature, and
// a future edit that "helpfully" reddened liveness would silently take a
// working instance out of the pool.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// verifiedHistory is the verdict a process records when it opened a chain it
// checked.
func verifiedHistory(note string) *Verification {
	return &Verification{ChainVerified: true, Note: note}
}

// readyStore is a store holding one corridor's worth of history, so a
// readiness read has something real to count.
func readyStore(t *testing.T) runstore.Store {
	t.Helper()
	st, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	if err := st.Append(context.Background(), &runstore.Record{
		Version:    runstore.Version,
		Seq:        1,
		RecordedAt: now,
		Corridor:   "USDC-NGNC",
		Integrity:  "DIRECT",
		Reference: runstore.Reference{
			Mid:           "1349.67",
			Source:        "exchangerate-api",
			AsOf:          now.Format(time.RFC3339),
			ScoredAgainst: "exchangerate-api",
		},
		FloorLossPct: "27.15",
		FloorSize:    "0.1",
		WorstLossPct: "97.52",
		WorstSize:    "5000",
		Finding:      "Stored corridor for readiness tests.",
		Rungs:        []runstore.Rung{},
	}); err != nil {
		t.Fatal(err)
	}
	return st
}

// countingUpstream is a Horizon root endpoint that records being asked. It
// lets the cheap-readiness test prove both halves: that /readyz never asks,
// and that this very server does ask for /healthz — without the second half
// the first would only show that nothing in the fixture reaches anywhere.
type countingUpstream struct {
	hits atomic.Int64
	srv  *httptest.Server
}

func newCountingUpstream(t *testing.T) *countingUpstream {
	t.Helper()
	c := &countingUpstream{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.hits.Add(1)
		w.Header().Set("Content-Type", "application/hal+json")
		_, _ = w.Write([]byte(`{"history_latest_ledger":"5400"}`))
	}))
	t.Cleanup(c.srv.Close)
	return c
}

// readyServer serves s exactly as wayfared would: through Handler(), with the
// limiter and CORS wrappers in front, so a route registered on the mux is
// tested as the deployed route rather than as the handler function.
func readyServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	api := httptest.NewServer(s.Handler())
	t.Cleanup(api.Close)
	return api
}

func fetchReadyz(t *testing.T, url string) (int, readinessJSON, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", url, err)
	}
	var doc readinessJSON
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decoding %s: %v\n%s", url, err, raw)
	}
	return resp.StatusCode, doc, raw
}

func TestReadyzReadyWithAVerifiedChainAndAReadableStore(t *testing.T) {
	api := readyServer(t, &Server{
		Store:        readyStore(t),
		Verification: verifiedHistory("1 corridors verified at open"),
	})

	status, doc, raw := fetchReadyz(t, api.URL+"/readyz")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, raw)
	}
	if doc.Status != statusReady {
		t.Errorf("status = %q, want %q", doc.Status, statusReady)
	}
	if doc.Checks.Verification.State != stateVerified {
		t.Errorf("verification.state = %q, want %q", doc.Checks.Verification.State, stateVerified)
	}
	if doc.Checks.Verification.Note != "1 corridors verified at open" {
		t.Errorf("verification.note = %q, want the verdict's own words", doc.Checks.Verification.Note)
	}
	if doc.Checks.Store.State != stateReadable {
		t.Errorf("store.state = %q, want %q", doc.Checks.Store.State, stateReadable)
	}
	if doc.Checks.Store.Corridors == nil || *doc.Checks.Store.Corridors != 1 {
		t.Errorf("store.corridors = %v, want 1 (the corridor this store holds)", doc.Checks.Store.Corridors)
	}
}

// TestReadyzRefusesAnUnverifiedChainIsNotARefusedHealthz is the separation the
// issue asks for: the same process, same store, same everything, gets a 200
// from liveness and a 503 from readiness.
func TestReadyzRefusesAnUnverifiedChainIsNotARefusedHealthz(t *testing.T) {
	st := readyStore(t)
	s := &Server{
		Store: st,
		// The fallback wayfared takes when the embedded history will not
		// verify: keep serving, keep the reason.
		Verification: &Verification{Note: "embedded history did not open or verify: " +
			"runstore: USDC-NGNC: runstore: record seq 1 has hash 424b33fcf120… " +
			"but its contents hash to 3c4afa7aa3ef…; it was modified after it was written"},
	}
	api := readyServer(t, s)

	status, doc, raw := fetchReadyz(t, api.URL+"/readyz")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", status, raw)
	}
	if doc.Checks.Verification.State != stateUnverified {
		t.Errorf("verification.state = %q, want %q", doc.Checks.Verification.State, stateUnverified)
	}
	if !strings.Contains(doc.Checks.Verification.Note, "it was modified after it was written") {
		t.Errorf("verification.note = %q, want the startup failure echoed", doc.Checks.Verification.Note)
	}
	// The store itself is fine; only the verdict on it is not. Reporting the
	// gate that passed as though it had failed would send an operator to the
	// wrong file.
	if doc.Checks.Store.State != stateReadable {
		t.Errorf("store.state = %q, want %q", doc.Checks.Store.State, stateReadable)
	}

	health, err := http.Get(api.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer health.Body.Close()
	if health.StatusCode != http.StatusOK {
		t.Errorf("/healthz status = %d, want 200: liveness does not depend on the verdict",
			health.StatusCode)
	}
}

func TestReadyzRefusesAnUnrecordedVerdict(t *testing.T) {
	// A Server assembled without opening a store — the zero value, and every
	// test in this package that builds one. "Nobody checked" is not
	// "something failed", and it is not a pass either.
	api := readyServer(t, &Server{Store: readyStore(t)})

	status, doc, raw := fetchReadyz(t, api.URL+"/readyz")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", status, raw)
	}
	if doc.Checks.Verification.State != stateUnrecorded {
		t.Errorf("verification.state = %q, want %q", doc.Checks.Verification.State, stateUnrecorded)
	}
	if doc.Checks.Verification.State == stateUnverified {
		t.Error("an absent verdict was reported as a failed one")
	}
}

func TestReadyzRefusesAnUnreadableStore(t *testing.T) {
	api := readyServer(t, &Server{
		Store:        unreadableCorridorsStore{readyStore(t)},
		Verification: verifiedHistory("1 corridors verified at open"),
	})

	status, doc, raw := fetchReadyz(t, api.URL+"/readyz")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", status, raw)
	}
	if doc.Checks.Store.State != stateUnreadable {
		t.Errorf("store.state = %q, want %q", doc.Checks.Store.State, stateUnreadable)
	}
	if doc.Checks.Store.Corridors != nil {
		t.Errorf("store.corridors = %d, want null: a store that could not be asked has no count",
			*doc.Checks.Store.Corridors)
	}
	// The zero value of a Go int is what makes this worth checking on the
	// wire rather than only on the decoded struct.
	if strings.Contains(string(raw), `"corridors":0`) {
		t.Errorf("body renders an unreadable store as a count of zero: %s", raw)
	}
	if !strings.Contains(string(raw), `"corridors":null`) {
		t.Errorf("body does not render the unknown as null: %s", raw)
	}
	if !strings.Contains(doc.Checks.Store.Error, "chain file is unreadable") {
		t.Errorf("store.error = %q, want the read failure echoed", doc.Checks.Store.Error)
	}
	// The verdict still stands on its own: a store that fell over at read
	// time does not retroactively un-verify what was checked at startup.
	if doc.Checks.Verification.State != stateVerified {
		t.Errorf("verification.state = %q, want %q", doc.Checks.Verification.State, stateVerified)
	}
}

func TestReadyzRefusesAnAbsentStore(t *testing.T) {
	api := readyServer(t, &Server{Verification: verifiedHistory("0 corridors verified at open")})

	status, doc, raw := fetchReadyz(t, api.URL+"/readyz")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", status, raw)
	}
	if doc.Checks.Store.State != stateAbsent {
		t.Errorf("store.state = %q, want %q", doc.Checks.Store.State, stateAbsent)
	}
	if doc.Checks.Store.Corridors != nil {
		t.Errorf("store.corridors = %d, want null with no store configured", *doc.Checks.Store.Corridors)
	}
}

// TestReadyzIsCheap is why readiness does not reuse the health path: a probe
// a platform polls every few seconds must not cost a Horizon round trip, and
// must not go red because an upstream did.
func TestReadyzIsCheap(t *testing.T) {
	upstream := newCountingUpstream(t)

	api := readyServer(t, &Server{
		Engine:       &route.Engine{DEX: &dex.Client{HorizonURL: upstream.srv.URL}},
		Store:        readyStore(t),
		Verification: verifiedHistory("1 corridors verified at open"),
	})

	status, _, raw := fetchReadyz(t, api.URL+"/readyz")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 with an upstream answering; body %s", status, raw)
	}
	if got := upstream.hits.Load(); got != 0 {
		t.Errorf("/readyz made %d upstream calls, want 0", got)
	}

	// The control, on the same server: liveness does ask the upstream, so a
	// count of zero above is a statement about readiness rather than about a
	// fixture nothing ever reaches.
	health, err := http.Get(api.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer health.Body.Close()
	_, _ = io.ReadAll(health.Body)
	if got := upstream.hits.Load(); got == 0 {
		t.Error("/healthz made no upstream call, so the readiness count proved nothing")
	}
}

func TestReadyzDocumentShape(t *testing.T) {
	api := readyServer(t, &Server{
		Store:        readyStore(t),
		Verification: verifiedHistory("shape"),
	})

	status, _, raw := fetchReadyz(t, api.URL+"/readyz")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	top := objectFields(t, raw)
	want := []string{"checks", "code_commit", "data_commit", "status"}
	if len(top) != len(want) {
		t.Errorf("top-level fields = %v, want exactly %v", sortedKeys(top), want)
	}
	for _, f := range want {
		if _, ok := top[f]; !ok {
			t.Errorf("%s is missing from the readiness document", f)
		}
	}

	checks := objectFields(t, top["checks"])
	if got := sortedKeys(checks); strings.Join(got, ",") != "store,verification" {
		t.Errorf("checks fields = %v, want exactly [store verification]", got)
	}

	// A negative answer is the same document, not an error envelope: the
	// probe asked a valid question either way.
	bad := readyServer(t, &Server{Store: readyStore(t)})
	_, negDoc, negRaw := fetchReadyz(t, bad.URL+"/readyz")
	if negDoc.Status != statusNotReady {
		t.Errorf("status = %q, want %q", negDoc.Status, statusNotReady)
	}
	if got, want := sortedKeys(objectFields(t, negRaw)), sortedKeys(top); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("a not-ready body has fields %v, want the same document as ready: %v", got, want)
	}
	if strings.Contains(string(negRaw), `"error":`) && strings.Contains(string(negRaw), `"code":`) {
		t.Errorf("a 503 readiness used the error envelope: %s", negRaw)
	}
}

func TestReadyzAcceptsGETAndHEADOnly(t *testing.T) {
	api := readyServer(t, &Server{Store: readyStore(t), Verification: verifiedHistory("shape")})

	resp, err := http.Post(api.URL+"/readyz", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", resp.StatusCode)
	}

	head, err := http.Head(api.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	defer head.Body.Close()
	if head.StatusCode != http.StatusOK {
		t.Errorf("HEAD status = %d, want 200", head.StatusCode)
	}
	if ct := head.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("HEAD Content-Type = %q, want application/json", ct)
	}
}

func TestReadyzHonoursPretty(t *testing.T) {
	api := readyServer(t, &Server{Store: readyStore(t), Verification: verifiedHistory("shape")})

	_, _, pretty := fetchReadyz(t, api.URL+"/readyz?pretty=1")
	if !strings.Contains(string(pretty), "\n  \"status\"") {
		t.Errorf("?pretty=1 did not indent the body: %s", pretty)
	}
}

// unreadableCorridorsStore passes every read through to a real store except
// the one readiness makes.
type unreadableCorridorsStore struct {
	runstore.Store
}

func (unreadableCorridorsStore) Corridors(context.Context) ([]string, error) {
	return nil, errors.New("chain file is unreadable")
}

func sortedKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
