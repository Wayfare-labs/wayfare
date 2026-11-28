package main

// This file carries issue #869's first acceptance criterion: ladder -json and
// /api/corridor must produce the same reference_* fields for the same replayed
// snapshot. Before the CLI was moved onto refrate.DefaultStack it could only
// ever say SINGLE — no secondary mid, no divergence, no diagnostic, never
// MALFUNCTION — so "reproduce it with the CLI" reproduced a weaker claim than
// the API published. The test drives both producers over one snapshot and
// compares the whole reference block, not just the field set.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/server"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// referenceFixtureAt is the moment the synthetic reference bodies describe.
// It is recent on purpose: the stack applies the same absolute freshness bound
// the binaries use, and a fixed past timestamp would fail that check before
// the reference fields could be compared.
var referenceFixtureAt = time.Now().UTC().Truncate(time.Second)

// replayableReferenceSnapshot copies the committed USDC-NGNC snapshot into a
// temporary directory and adds the reference responses the shared stack
// requests, so the whole benchmark can be replayed offline.
//
// The committed fixtures predate issue #869 and contain only currency-api:
// they were captured by a CLI that used one provider. Adding the two other
// bodies here is what makes the default stack replayable without touching the
// published fixtures.
func replayableReferenceSnapshot(t *testing.T) *snapshot.Manifest {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join("..", "..", "testdata", "snapshots", "usdc-ngnc-*"))
	if err != nil || len(matches) == 0 {
		t.Skip("no USDC-NGNC snapshot fixture")
	}
	base, err := snapshot.Load(matches[0])
	if err != nil {
		t.Fatalf("loading base snapshot: %v", err)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "responses"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &snapshot.Manifest{
		Format:      snapshot.Format,
		Version:     snapshot.Version,
		RecordedAt:  base.RecordedAt,
		GitRevision: base.GitRevision,
		Corridor:    base.Corridor,
		Sizes:       base.Sizes,
		Sources: snapshot.Sources{
			Horizon: base.Sources.Horizon,
			Reference: snapshot.SourceRef{
				Provider: "exchangerate-api",
				BaseURL:  refrate.DefaultExchangeRateAPI,
			},
		},
		Notes: []string{"synthetic reference stack for issue #869 parity test"},
	}

	for _, in := range base.Interactions {
		body, ok := base.Body(in.Key)
		if !ok {
			t.Fatalf("base snapshot has no body for %q", in.Key)
		}
		m.Interactions = append(m.Interactions, in)
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(in.BodyFile)), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	addReference := func(rawURL, body string) {
		t.Helper()
		u, err := url.Parse(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		file := "responses/9" + strconv.Itoa(len(m.Interactions)) + "-reference-fixture.json"
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(file)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		m.Interactions = append(m.Interactions, snapshot.Interaction{
			Kind:       snapshot.KindReference,
			Method:     http.MethodGet,
			Key:        snapshot.Key(http.MethodGet, u),
			URL:        rawURL,
			Status:     http.StatusOK,
			RecordedAt: referenceFixtureAt,
			BodyFile:   file,
			BodySHA256: "sha256:" + sha256Hex([]byte(body)),
		})
	}

	// A genuine 5% disagreement: inside the malfunction bound, so the
	// conservative rule picks the higher mid and the diagnostic attributes
	// the outlier. Both producers must publish exactly that.
	addReference("https://open.er-api.com/v6/latest/USD", fmt.Sprintf(
		`{"result":"success","base_code":"USD","time_last_update_unix":%d,`+
			`"rates":{"USD":1,"NGN":1300}}`, referenceFixtureAt.Unix()))
	addReference("https://latest.currency-api.pages.dev/v1/currencies/usd.json",
		fmt.Sprintf(`{"date":"%s","usd":{"ngn":1365}}`,
			referenceFixtureAt.Format("2006-01-02")))
	addReference("https://www.floatrates.com/daily/usd.json",
		fmt.Sprintf(`{"ngn":{"rate":"1350","date":%q}}`,
			referenceFixtureAt.Format(http.TimeFormat)))

	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, snapshot.ManifestFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := snapshot.Load(dir)
	if err != nil {
		t.Fatalf("loading synthetic snapshot: %v", err)
	}
	return loaded
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func replayEngine(m *snapshot.Manifest) *route.Engine {
	return &route.Engine{
		DEX: &dex.Client{
			HorizonURL: "https://horizon.stellar.org",
			HTTPClient: m.HTTPClient(),
		},
		// The same constructor cmd/ladder and cmd/wayfared now use.
		RefRate: refrate.DefaultStack(refrate.StackConfig{
			Client: m.HTTPClient(),
			MaxAge: refrate.DefaultMaxAge,
		}),
	}
}

// TestLadderAndAPIReferenceFieldsAgreeOnReplayedSnapshot is issue #869's
// acceptance test. One snapshot, two producers, one reference block.
func TestLadderAndAPIReferenceFieldsAgreeOnReplayedSnapshot(t *testing.T) {
	m := replayableReferenceSnapshot(t)

	sizes := make([]decimal.Decimal, 0, len(m.Sizes))
	for _, s := range m.Sizes {
		amt, err := decimal.NewFromString(s)
		if err != nil {
			t.Fatalf("size %q: %v", s, err)
		}
		sizes = append(sizes, amt)
	}
	req := route.LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   asset.NGNC(),
		Sizes:          sizes,
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
	}

	// ladder -json: the same encoding path main() uses.
	result, err := replayEngine(m).Ladder(context.Background(), req)
	if err != nil {
		t.Fatalf("ladder: %v", err)
	}
	var ladderBuf bytes.Buffer
	if err := encodeCorridorJSON(&ladderBuf, result, "USD/NGN", nil); err != nil {
		t.Fatalf("encodeCorridorJSON: %v", err)
	}
	var ladderDoc route.CorridorJSON
	if err := json.Unmarshal(ladderBuf.Bytes(), &ladderDoc); err != nil {
		t.Fatalf("decoding ladder JSON: %v", err)
	}

	// /api/corridor: a fresh engine over the same snapshot, as a server
	// process would build it.
	srv := &server.Server{Engine: replayEngine(m), Timeout: 90 * time.Second}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/corridor?from=USDC&to=NGNC&sizes="+strings.Join(m.Sizes, ","), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/corridor status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var apiDoc route.CorridorJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &apiDoc); err != nil {
		t.Fatalf("decoding API JSON: %v", err)
	}

	// Guard against the regression itself: a single-provider run would agree
	// with itself and pass every field comparison while proving nothing.
	if ladderDoc.ReferenceAgreement == refrate.AgreementSingle.String() {
		t.Fatal("ladder -json is SINGLE; it did not cross-check the reference stack")
	}
	if got, want := ladderDoc.ReferenceAgreement, refrate.AgreementDisagree.String(); got != want {
		t.Errorf("reference_agreement = %q, want %q from the fixture bodies", got, want)
	}
	if ladderDoc.ReferenceDiagnostic == nil {
		t.Fatal("a DISAGREE must carry the third-provider attribution")
	}

	compare := func(field, ladder, api string) {
		t.Helper()
		if ladder != api {
			t.Errorf("%s: ladder = %q, api = %q; the shared stack must produce one answer",
				field, ladder, api)
		}
		if ladder == "" {
			t.Errorf("%s is empty on both sides; the field must carry the cross-check", field)
		}
	}
	compare("reference_mid", ladderDoc.ReferenceMid, apiDoc.ReferenceMid)
	compare("reference_source", ladderDoc.ReferenceSource, apiDoc.ReferenceSource)
	compare("reference_pair", ladderDoc.ReferencePair, apiDoc.ReferencePair)
	compare("reference_agreement", ladderDoc.ReferenceAgreement, apiDoc.ReferenceAgreement)
	compare("reference_secondary_mid", ladderDoc.ReferenceSecondaryMid, apiDoc.ReferenceSecondaryMid)
	compare("reference_secondary_source", ladderDoc.ReferenceSecondarySource, apiDoc.ReferenceSecondarySource)
	compare("reference_divergence_pct", ladderDoc.ReferenceDivergencePct, apiDoc.ReferenceDivergencePct)
	compare("reference_note", ladderDoc.ReferenceNote, apiDoc.ReferenceNote)
	compare("reference_as_of", ladderDoc.ReferenceAsOf, apiDoc.ReferenceAsOf)
	compare("reference_secondary_as_of", ladderDoc.ReferenceSecondaryAsOf, apiDoc.ReferenceSecondaryAsOf)
	if ladderDoc.Scored != apiDoc.Scored {
		t.Errorf("scored: ladder = %v, api = %v", ladderDoc.Scored, apiDoc.Scored)
	}
	if !ladderDoc.Scored {
		t.Error("a DISAGREE inside the malfunction bound is scoreable")
	}

	ld, ad := ladderDoc.ReferenceDiagnostic, apiDoc.ReferenceDiagnostic
	if ld == nil || ad == nil {
		t.Fatalf("diagnostic: ladder = %+v, api = %+v", ld, ad)
	}
	for _, f := range []struct{ name, ladder, api string }{
		{"diagnostic.source", ld.Source, ad.Source},
		{"diagnostic.mid", ld.Mid, ad.Mid},
		{"diagnostic.outlier", ld.Outlier, ad.Outlier},
		{"diagnostic.outlier_distance_pct", ld.OutlierDistancePct, ad.OutlierDistancePct},
		{"diagnostic.other_distance_pct", ld.OtherDistancePct, ad.OtherDistancePct},
	} {
		if f.ladder != f.api {
			t.Errorf("%s: ladder = %q, api = %q", f.name, f.ladder, f.api)
		}
	}
	if ld.Outlier == "" {
		t.Error("the diagnostic should attribute the outlier for these bodies")
	}
}
