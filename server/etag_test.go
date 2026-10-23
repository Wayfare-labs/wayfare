package server

// Conditional requests on history-served corridor responses (issue #613).
//
// A stored record is immutable, so a client polling a history-first instance
// can be told which record it is holding and asked whether that is still the
// one the store has. A matching answer is a 304 with no body; a live
// measurement has no validator at all, and must never be answered
// conditionally.
//
// These tests pin the two halves of that contract: the tag a history-served
// response carries, and the absence of any tag on a live one.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/runstore"
)

// etagRequest performs a GET with optional headers and returns the status,
// the response headers and the raw body.
//
// The raw body matters: a 304 must carry none, and a JSON decoder would turn
// an empty body into an error rather than into the fact that is being tested.
func etagRequest(t *testing.T, url string, headers map[string]string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, raw
}

// latestRecord reads the most recent stored record for a corridor, so a test
// can compare the served tag against the seal the store holds.
func latestRecord(t *testing.T, st runstore.Store) *runstore.Record {
	t.Helper()
	rec, err := st.Latest(context.Background(), runstore.CorridorKey("USDC", "NGNC"))
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("the store holds no record for USDC-NGNC")
	}
	return rec
}

// TestHistoryServedResponseCarriesTheRecordsSealAsItsETag is the headline of
// the issue: a history-served response is tagged with the record's own seal
// hash — the value the chain verifies against — so a client can hold a
// validator it did not have to compute.
func TestHistoryServedResponseCarriesTheRecordsSealAsItsETag(t *testing.T) {
	st := storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))
	rec := latestRecord(t, st)

	srv := historyFirstServer(t, st)
	status, header, raw := etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a reading served from history; body %s", status, raw)
	}

	etag := header.Get("ETag")
	if etag == "" {
		t.Fatal("a history-served response carries no ETag; a polling client cannot tell whether it already has this record")
	}
	if want := `"` + rec.Hash + `"`; etag != want {
		t.Errorf("ETag = %q, want %q (the record's own seal hash, quoted)", etag, want)
	}

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body["live"] != false {
		t.Errorf("live = %v, want false: the response this tag identifies is a stored reading", body["live"])
	}
}

// TestMatchingIfNoneMatchIsAnsweredNotModified is the first acceptance
// criterion: 304 on a matching ETag.
//
// The 304 must carry the tag back — it is the answer to the question the
// client asked — and no body.
func TestMatchingIfNoneMatchIsAnsweredNotModified(t *testing.T) {
	st := storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))

	srv := historyFirstServer(t, st)
	_, header, _ := etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100", nil)
	etag := header.Get("ETag")
	if etag == "" {
		t.Fatal("the first response carries no ETag to revalidate against")
	}

	status, header, raw := etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100",
		map[string]string{"If-None-Match": etag})
	if status != http.StatusNotModified {
		t.Fatalf("status = %d, want 304 when the client's ETag matches; body %s", status, raw)
	}
	if len(raw) != 0 {
		t.Errorf("a 304 carried a body of %d bytes; it must carry none", len(raw))
	}
	if got := header.Get("ETag"); got != etag {
		t.Errorf("ETag on the 304 = %q, want %q: the answer names the record the client already has", got, etag)
	}
}

// TestNonMatchingIfNoneMatchServesTheDocument is the control for the test
// above. A tag that does not name this record must not silence the response:
// serving 304 for a different record would leave the client reading a
// measurement that is no longer the store's.
func TestNonMatchingIfNoneMatchServesTheDocument(t *testing.T) {
	st := storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))

	srv := historyFirstServer(t, st)
	status, _, raw := etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100",
		map[string]string{"If-None-Match": `"sha256:0000000000000000000000000000000000000000000000000000000000000000"`})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a tag that does not match; body %s", status, raw)
	}
	if !strings.Contains(string(raw), "\"live\":false") {
		t.Errorf("body = %s, want the stored reading", raw)
	}
}

// TestIfNoneMatchListMatchesAnyMember covers the header's real shape: a
// client may send a list, and a match anywhere in it counts.
func TestIfNoneMatchListMatchesAnyMember(t *testing.T) {
	st := storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))
	rec := latestRecord(t, st)

	srv := historyFirstServer(t, st)
	headerValue := `"sha256:0000000000000000000000000000000000000000000000000000000000000000", "` + rec.Hash + `"`

	status, _, raw := etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100",
		map[string]string{"If-None-Match": headerValue})
	if status != http.StatusNotModified {
		t.Fatalf("status = %d, want 304 when the tag appears in the list; body %s", status, raw)
	}
}

// TestWeakIfNoneMatchIsAnsweredNotModified pins the comparison RFC 9110 asks
// for: If-None-Match is compared weakly, so a client that prefixes the tag
// it received with W/ is answered the same way.
func TestWeakIfNoneMatchIsAnsweredNotModified(t *testing.T) {
	st := storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))
	rec := latestRecord(t, st)

	srv := historyFirstServer(t, st)
	status, _, raw := etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100",
		map[string]string{"If-None-Match": `W/"` + rec.Hash + `"`})
	if status != http.StatusNotModified {
		t.Fatalf("status = %d, want 304 for a weak match; body %s", status, raw)
	}
}

// TestWildcardIfNoneMatchIsAnsweredNotModified covers "*", which RFC 9110
// defines as matching any current representation. A history-served response
// always has one — the record the store holds.
func TestWildcardIfNoneMatchIsAnsweredNotModified(t *testing.T) {
	st := storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))

	srv := historyFirstServer(t, st)
	status, _, raw := etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100",
		map[string]string{"If-None-Match": "*"})
	if status != http.StatusNotModified {
		t.Fatalf("status = %d, want 304 for the wildcard; body %s", status, raw)
	}
}

// TestHistoryServedResponseMustBeRevalidated pins the cache directive that
// makes the validator the only path to a reused body. A response with no
// explicit lifetime leaves freshness to the client's heuristic, and some of
// them will reuse the stored body without ever asking.
func TestHistoryServedResponseMustBeRevalidated(t *testing.T) {
	st := storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))
	rec := latestRecord(t, st)

	srv := historyFirstServer(t, st)
	_, header, _ := etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100", nil)
	if got := header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-cache")
	}

	// The 304 carries it too: RFC 9111 asks a 304 to repeat the cache
	// directives of the response it stands in for.
	_, header, _ = etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100",
		map[string]string{"If-None-Match": `"` + rec.Hash + `"`})
	if got := header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control on the 304 = %q, want %q", got, "no-cache")
	}
}

// TestETagIsReadableAcrossOrigins guards the browser half of the contract.
// A cross-origin fetch hides every response header the server did not expose,
// so without this the tag would exist and no page could read it — the
// conditional-request contract would be unreachable for exactly the clients
// most likely to poll.
func TestETagIsReadableAcrossOrigins(t *testing.T) {
	st := storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))

	srv := historyFirstServer(t, st)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/corridor?to=NGNC&sizes=100", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://consumer.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	exposed := resp.Header.Get("Access-Control-Expose-Headers")
	if !strings.Contains(exposed, "ETag") {
		t.Errorf("Access-Control-Expose-Headers = %q, want it to name ETag; a browser consumer cannot read the tag otherwise", exposed)
	}
}

// TestLiveResponseCarriesNoETag is the second acceptance criterion: no ETag
// on live responses.
//
// A live measurement's figures depend on the market at the moment of the
// request. A tag on it would claim the body is reusable, and a client that
// trusted it would keep reading a measurement that was never taken again.
func TestLiveResponseCarriesNoETag(t *testing.T) {
	st := storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))

	srv := historyFirstServer(t, st)
	status, header, raw := etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100&live=1", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 for an explicit live request; body %s", status, raw)
	}
	if etag := header.Get("ETag"); etag != "" {
		t.Errorf("ETag = %q on a live response; a live measurement has no validator", etag)
	}
}

// TestLiveResponseIsNeverAnsweredNotModified is the control for the test
// above. A client that sends a conditional request must not be told "not
// modified" about a live measurement — including with the wildcard, which
// would otherwise match anything.
func TestLiveResponseIsNeverAnsweredNotModified(t *testing.T) {
	st := storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))
	rec := latestRecord(t, st)

	srv := historyFirstServer(t, st)
	for _, headerValue := range []string{"*", `"` + rec.Hash + `"`} {
		status, header, raw := etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100&live=1",
			map[string]string{"If-None-Match": headerValue})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 for a live measurement with If-None-Match %q; body %s",
				status, headerValue, raw)
		}
		if etag := header.Get("ETag"); etag != "" {
			t.Errorf("ETag = %q on a live response; a live measurement has no validator", etag)
		}
	}
}

// TestHistoryFallbackAfterAFailedLiveMeasurementCarriesAnETag covers the
// other route to a live:false response: the measurement failed and the
// stored run was served instead. That response describes the same immutable
// record, so it carries the same kind of validator and answers the same way.
func TestHistoryFallbackAfterAFailedLiveMeasurementCarriesAnETag(t *testing.T) {
	st := storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))
	rec := latestRecord(t, st)

	srv := deadServer(t, st)
	status, header, raw := etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 when history can be served; body %s", status, raw)
	}
	if want := `"` + rec.Hash + `"`; header.Get("ETag") != want {
		t.Errorf("ETag = %q, want %q", header.Get("ETag"), want)
	}

	status, _, raw = etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100",
		map[string]string{"If-None-Match": `"` + rec.Hash + `"`})
	if status != http.StatusNotModified {
		t.Fatalf("status = %d, want 304 on a matching tag; body %s", status, raw)
	}
}

// TestNoHistoryResponseCarriesNoETag keeps the tag attached to a record. An
// absence is not a representation: there is nothing to revalidate against,
// and a tag on it would let a client believe it already has the answer.
func TestNoHistoryResponseCarriesNoETag(t *testing.T) {
	srv := historyFirstServer(t, emptyStore(t))

	status, header, raw := etagRequest(t, srv.URL+"/api/corridor?to=NGNT&sizes=100", nil)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a corridor with no history; body %s", status, raw)
	}
	if etag := header.Get("ETag"); etag != "" {
		t.Errorf("ETag = %q on a no_history response; there is no representation to tag", etag)
	}
}

// TestETagChangesWhenANewRecordLands is the reason the tag is worth having:
// it moves exactly when the served record does. A client that saw the old tag
// must be served the new document rather than told nothing has changed.
func TestETagChangesWhenANewRecordLands(t *testing.T) {
	dir := t.TempDir()
	st := storedRun(t, dir, time.Now().UTC().Add(-6*time.Hour))
	first := latestRecord(t, st)

	srv := historyFirstServer(t, st)
	_, header, _ := etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100", nil)
	firstTag := header.Get("ETag")
	if firstTag != `"`+first.Hash+`"` {
		t.Fatalf("ETag = %q, want the first record's seal", firstTag)
	}

	// The next sweep lands.
	if err := st.Append(context.Background(), &runstore.Record{
		Corridor:   "USDC-NGNC",
		Integrity:  "DIRECT",
		Reference:  runstore.Reference{Mid: "1350.2568", Source: "currency-api"},
		Finding:    "A newer run.",
		RecordedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	second := latestRecord(t, st)
	if second.Hash == first.Hash {
		t.Fatal("the new record has the same hash as the old one")
	}

	_, header, _ = etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100", nil)
	secondTag := header.Get("ETag")
	if secondTag != `"`+second.Hash+`"` {
		t.Errorf("ETag = %q, want the new record's seal %q", secondTag, `"`+second.Hash+`"`)
	}
	if secondTag == firstTag {
		t.Error("the ETag did not change when the record changed")
	}

	// The old tag names a record that is no longer the store's latest, so it
	// must not silence the response.
	status, _, raw := etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100",
		map[string]string{"If-None-Match": firstTag})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a tag naming a superseded record; body %s", status, raw)
	}
}

// unsealedStore serves a record whose seal was never written.
//
// It stands for a store implementation that hands back a record without a
// hash. The response must still be served in full: there is no validator to
// derive, and inventing one would be a claim about the bytes the store
// cannot back.
type unsealedStore struct {
	runstore.Store
}

func (s unsealedStore) Latest(ctx context.Context, corridor string) (*runstore.Record, error) {
	rec, err := s.Store.Latest(ctx, corridor)
	if err != nil || rec == nil {
		return rec, err
	}
	clone := *rec
	clone.Hash = ""
	return &clone, nil
}

// TestUnsealedRecordIsServedWithoutAnETag pins the honest fallback.
func TestUnsealedRecordIsServedWithoutAnETag(t *testing.T) {
	st := storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))

	srv := historyFirstServer(t, unsealedStore{st})
	status, header, raw := etagRequest(t, srv.URL+"/api/corridor?to=NGNC&sizes=100", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, raw)
	}
	if etag := header.Get("ETag"); etag != "" {
		t.Errorf("ETag = %q on a record with no seal; there is nothing to derive it from", etag)
	}
}

// TestRecordETagQuotesTheSeal is the unit form of the header's shape: an
// entity tag is a quoted string, and the seal it carries is the record's own.
func TestRecordETagQuotesTheSeal(t *testing.T) {
	if got := recordETag(&runstore.Record{Hash: "sha256:abc"}); got != `"sha256:abc"` {
		t.Errorf("recordETag = %q, want %q", got, `"sha256:abc"`)
	}
	if got := recordETag(&runstore.Record{}); got != "" {
		t.Errorf("recordETag of an unsealed record = %q, want an empty tag", got)
	}
	if got := recordETag(nil); got != "" {
		t.Errorf("recordETag(nil) = %q, want an empty tag", got)
	}
}

// TestIfNoneMatchRejectsAnEmptyTag pins the rule that a response with no
// validator is never answered conditionally.
func TestIfNoneMatchRejectsAnEmptyTag(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://example.invalid/api/corridor", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("If-None-Match", "*")
	if ifNoneMatch(req, "") {
		t.Error("a request with no tag to compare was answered not-modified")
	}
	if ifNoneMatch(req, `"sha256:abc"`) != true {
		t.Error("the wildcard did not match a tag that exists")
	}
}

// TestETagSurvivesCompression is a property of the middleware stack rather
// than of the handler: a compressed history-served response must still carry
// its tag, because a client that cannot read the tag cannot revalidate. The
// 304 in particular must arrive uncompressed and empty.
func TestETagSurvivesCompression(t *testing.T) {
	st := storedRun(t, t.TempDir(), time.Now().UTC().Add(-6*time.Hour))
	rec := latestRecord(t, st)

	s := &Server{
		Engine:       newEngine("http://127.0.0.1:1", "1500"),
		Store:        st,
		HistoryFirst: true,
	}
	api := httptest.NewServer(s.Handler())
	t.Cleanup(api.Close)

	req, err := http.NewRequest(http.MethodGet, api.URL+"/api/corridor?to=NGNC&sizes=100", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("ETag") != `"`+rec.Hash+`"` {
		t.Errorf("ETag = %q, want the record's seal on a compressed response", resp.Header.Get("ETag"))
	}
}
