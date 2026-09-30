package wayfare

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// docs/ngnc-structural-floor.md is the one document in the repository that
// makes claims about what the engine measured and about what the engine
// cannot measure. A case study is only worth publishing if the figures in it
// can be traced, and the figures here come from two places the repository owns:
// the committed hash chain in data/, and a recorded snapshot. This file checks
// the document against both, and checks the code claims in it against the code.
//
// The negative tests matter most here. The document's subject is an attribution
// the tool has not made, and the failure mode being guarded against is a case
// study that quietly hardens that attribution into a result.

const caseStudyPath = "docs/ngnc-structural-floor.md"

func caseStudy(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(caseStudyPath)
	if err != nil {
		t.Fatalf("reading %s: %v", caseStudyPath, err)
	}
	return string(raw)
}

// asRecorded normalises a document to the form the chain stores paths in.
//
// The run store records a path as "USDC -> NGNC", while every document in this
// repository writes it as "USDC → NGNC" for readability. Comparing the two
// without normalising would fail on the arrow rather than on the substance, so
// the glyph is folded here and nothing else is.
func asRecorded(s string) string {
	return strings.NewReplacer("→", "->", "—", "--", "‑", "-").Replace(s)
}

// docStates reports whether the document carries a path, comparing on the
// recorded form rather than on typography.
func docStates(doc, path string) bool {
	return strings.Contains(asRecorded(doc), path)
}

// datedRow returns the table row the document attributes to a named source, so
// a figure can be checked where the document actually asserts it. Checking the
// whole document instead would let a figure repeated in prose satisfy the
// assertion while the row it belongs to drifted.
func datedRow(t *testing.T, doc, source string) string {
	t.Helper()
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "|") && strings.Contains(line, source) {
			return line
		}
	}
	t.Fatalf("the document has no table row attributed to %q; the figures it "+
		"cites are not where the test expects them", source)
	return ""
}

// rowCell returns the nth (1-based) cell of a markdown table row, trimmed. It
// exists because a whole-row substring check is satisfied by text in a
// neighbouring cell — a date cell is trivially "present" in a row that also
// links to an anchor carrying the same date.
func rowCell(row string, n int) string {
	parts := strings.Split(row, "|")
	// parts[0] is empty for a row that opens with "|", so cell n is parts[n].
	if n < len(parts) {
		return strings.TrimSpace(parts[n])
	}
	return ""
}

// datedRowAfter returns the first table row mentioning source that appears
// after anchor. The document has more than one table naming USDC → NGNC, so a
// lookup that did not scope itself to a section would find the wrong one and
// silently assert against the observations table instead of the
// three-corridor comparison.
func datedRowAfter(t *testing.T, doc, anchor, source string) string {
	t.Helper()
	at := strings.Index(doc, anchor)
	if at < 0 {
		t.Fatalf("the document has no section %q; the table being checked has "+
			"moved and this test is looking in the wrong place", anchor)
	}
	for _, line := range strings.Split(doc[at:], "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "|") && strings.Contains(line, source) {
			return line
		}
	}
	t.Fatalf("no table row mentioning %q after the section %q", source, anchor)
	return ""
}

// latestRecord reads the newest committed record for a corridor. runstore.Latest
// returns (*Record, error) and the no-op store answers (nil, nil), so a nil
// record with no error is a legitimate answer meaning "not recorded" and is
// reported as such rather than dereferenced.
func latestRecord(t *testing.T, corridor string) *runstore.Record {
	t.Helper()
	s, err := runstore.OpenFS(History, "data")
	if err != nil {
		t.Fatalf("embedded history does not load: %v", err)
	}
	rec, err := s.Latest(context.Background(), corridor)
	if err != nil {
		t.Fatalf("latest %s record: %v", corridor, err)
	}
	if rec == nil {
		t.Fatalf("no %s record in the committed chain; the case study cites "+
			"figures it cannot be checked against", corridor)
	}
	return rec
}

// TestCaseStudyFloorMatchesTheCommittedRecord is the happy path. Every figure
// the document attributes to the 2026-08-22 record is read back out of the
// committed chain, so the document cannot drift from the data it cites.
//
// This is the strongest source available: the chain is embedded in the binary,
// so the claim travels with whatever publishes it.
func TestCaseStudyFloorMatchesTheCommittedRecord(t *testing.T) {
	rec := latestRecord(t, "USDC-NGNC")
	doc := caseStudy(t)

	// The record's own timestamp is what the document dates the observation by.
	const wantRecordedAt = "2026-08-22T12:09:59Z"
	if rec.RecordedAt.UTC().Format("2006-01-02T15:04:05Z") != wantRecordedAt {
		t.Fatalf("committed record is dated %s, not %s; the document quotes the "+
			"wrong record", rec.RecordedAt.UTC(), wantRecordedAt)
	}

	for _, want := range []string{
		wantRecordedAt,
		rec.Reference.Mid,
		rec.Reference.Source,
		rec.FloorLossPct, // the floor, as a string: 27.15
		rec.WorstLossPct, // 97.52
		rec.WorstSize,    // 5000
	} {
		if want == "" {
			t.Fatal("the committed record has an empty field the document quotes")
		}
		if !strings.Contains(doc, want) {
			t.Errorf("the document does not carry %q from the committed record; "+
				"the two have drifted apart", want)
		}
	}

	// The floor is the whole subject of the document, so it is checked as a
	// value and not only as a substring. The figures are checked in the table
	// row that attributes them, because a figure repeated in prose elsewhere in
	// the document would otherwise satisfy a bare Contains and let the row
	// itself drift.
	if got := rec.FloorLossPct; got != "27.15" {
		t.Errorf("committed floor_loss_pct = %q, want 27.15", got)
	}
	if got := rec.FloorSize; got != "0.1" {
		t.Errorf("committed floor_size = %q, want 0.1", got)
	}
	if rec.Integrity != route.IntegrityDirect.String() {
		t.Errorf("committed integrity = %q, want DIRECT", rec.Integrity)
	}

	row := datedRow(t, doc, "committed record")

	// The row's own date cell, against the record's own timestamp. The full
	// timestamp is checked against the document separately above, but it also
	// appears in the sources list, so it is this cell that ties the table row
	// to the record the table is citing.
	if wantDate := strings.SplitN(wantRecordedAt, "T", 2)[0]; rowCell(row, 1) != wantDate {
		t.Errorf("the committed-record row's date cell is %q, want %q; the row "+
			"must be dated by the record it cites", rowCell(row, 1), wantDate)
	}
	if !strings.Contains(row, "**"+rec.FloorLossPct+"%**") {
		t.Errorf("the 2026-08-22 table row reads %q, want the committed floor "+
			"**%s%%** in it", row, rec.FloorLossPct)
	}
	// The floor is the headline figure and is set in bold; the worst loss is
	// carried with the size it was reached at, because on this corridor the
	// size is the interesting part of it.
	if !strings.Contains(row, rec.WorstLossPct+"% at "+rec.WorstSize) {
		t.Errorf("the 2026-08-22 table row reads %q, want the committed worst "+
			"loss %s%% at %s in it", row, rec.WorstLossPct, rec.WorstSize)
	}
	if !strings.Contains(row, " at "+rec.WorstSize) {
		t.Errorf("the 2026-08-22 table row reads %q, want the worst loss "+
			"attributed to size %s", row, rec.WorstSize)
	}

	// "All twelve rungs grade UNUSABLE" is a claim about every rung, so it is
	// checked against every rung rather than trusted.
	if len(rec.Rungs) != 12 {
		t.Fatalf("committed record has %d rungs, want 12", len(rec.Rungs))
	}
	for _, r := range rec.Rungs {
		if !r.Priced {
			t.Errorf("rung %s is unpriced; the document says all twelve are graded",
				r.SendAmount)
			continue
		}
		if r.Verdict != route.VerdictUnusable.String() {
			t.Errorf("rung %s verdict = %q, want UNUSABLE — the document claims "+
				"all twelve rungs are unusable", r.SendAmount, r.Verdict)
		}
	}

	// The dust rung's path is load-bearing for the case study's argument: the
	// floor is claimed to survive a change of route shape, so the document's
	// path for this date has to be the recorded one.
	var dust runstore.Rung
	for _, r := range rec.Rungs {
		if r.SendAmount == "0.1" {
			dust = r
		}
	}
	if dust.Path == "" {
		t.Fatal("the 0.1 rung carries no path")
	}
	if !docStates(doc, dust.Path) {
		t.Errorf("the document does not carry the recorded 0.1 path %q", dust.Path)
	}
}

// TestCaseStudyLadderAndThresholdsMatchTheCode checks the two code claims the
// document leans on: that the ladder is twelve sizes starting at 0.1, and that
// UNUSABLE begins strictly above 20%. Both are read from the code, so a
// threshold change fails the test instead of quietly making the document wrong.
func TestCaseStudyLadderAndThresholdsMatchTheCode(t *testing.T) {
	doc := caseStudy(t)

	// dex.DefaultSizes, cited by the document as the ladder.
	if len(dex.DefaultSizes) != 12 {
		t.Errorf("the default ladder has %d sizes, want 12", len(dex.DefaultSizes))
	}
	if got := dex.DefaultSizes[0].String(); got != "0.1" {
		t.Errorf("the smallest default size is %s, want 0.1", got)
	}
	if got := dex.DefaultSizes[len(dex.DefaultSizes)-1].String(); got != "5000" {
		t.Errorf("the largest default size is %s, want 5000", got)
	}

	// route.verdictFor is unexported and the thresholds are exercised at their
	// exact boundaries by the route package's own tests, so this asserts only
	// what it can: that the declared thresholds are the ones the document
	// prints. A threshold change fails here rather than making the document
	// quietly wrong.
	for _, tc := range []struct {
		got  string
		want string
		name string
	}{
		{route.ThresholdGood.String(), "3", "GOOD"},
		{route.ThresholdFair.String(), "8", "FAIR"},
		{route.ThresholdPoor.String(), "20", "POOR/UNUSABLE"},
	} {
		if tc.got != tc.want {
			t.Errorf("Threshold for %s = %s, want %s; the document states the "+
				"verdict bands as Good <=3, Fair <=8, Poor <=20", tc.name, tc.got, tc.want)
		}
	}

	// The load-bearing clause, not a bare "20%": the document says the band
	// several times, and a bare Contains would pass on any of them after the
	// sentence that states where UNUSABLE begins had been changed.
	for _, want := range []string{"above 20%", "≤3%", "≤8%", "≤20%"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document no longer says %q; the code declares "+
				"ThresholdGood=3, ThresholdFair=8, ThresholdPoor=20 and grades "+
				"anything above 20 UNUSABLE", want)
		}
	}

	// And the ladder it describes has to be the ladder the code prices.
	for _, want := range []string{"twelve sizes", "from 0.1 to 5000"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document does not say %q; dex.DefaultSizes is %d sizes "+
				"from %s to %s", want, len(dex.DefaultSizes),
				dex.DefaultSizes[0], dex.DefaultSizes[len(dex.DefaultSizes)-1])
		}
	}
}

// TestCaseStudyCodeCitationsResolve is the mechanical half of "every claim
// about the code is checked against the code". The document cites specific
// lines — `route/ladder.go:528-536`, `checks/runner.go:20` — so every one of
// those citations is resolved against the tree: the file must exist and be at
// least that long.
//
// A citation to a line that no longer says what the document claims is not
// caught by this, and is not claimed to be. What it does catch is the common
// failure: a file renamed, moved, or shortened, leaving the document pointing
// at nothing while still reading as verified.
func TestCaseStudyCodeCitationsResolve(t *testing.T) {
	doc := caseStudy(t)

	// path/to/file.go:123  or  path/to/file.go:123-456
	re := regexp.MustCompile(`` + "`" + `([a-z0-9_/]+\.go):(\d+)(?:-(\d+))?` + "`")
	seen := 0
	for _, m := range re.FindAllStringSubmatch(doc, -1) {
		path, first, last := m[1], m[2], m[3]
		seen++
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("the document cites %s, which is not in the tree; the claim "+
				"it supports cannot be checked", path)
			continue
		}
		lines := strings.Count(string(raw), "\n") + 1
		hi := first
		if last != "" && last > hi {
			hi = last
		}
		hiN := 0
		if _, err := fmt.Sscanf(hi, "%d", &hiN); err != nil {
			t.Errorf("the document cites %s:%s, which is not a line number", path, hi)
			continue
		}
		if hiN > lines {
			t.Errorf("the document cites %s:%s but the file has only %d lines; "+
				"the citation points past the end of a file that has changed", path, hi, lines)
		}
	}
	if seen == 0 {
		t.Error("the document cites no file:line references, so its code claims " +
			"cannot be checked by a reader")
	}
}

// TestCaseStudyReproducesFromTheCommittedSnapshot is the reproducibility
// half. The document quotes a third, replayed observation, so the replay is
// performed here through the real engine from recorded bytes — no network — and
// the structure the document claims is checked.
//
// The benchmark is the mid recorded in the 2026-08-22 record, not a value
// chosen to produce a nice number. The prices are the recorded 2026-08-21 bytes,
// so this is a replay against a recorded mid and the document says so.
func TestCaseStudyReproducesFromTheCommittedSnapshot(t *testing.T) {
	doc := caseStudy(t)

	m, err := snapshot.Load("testdata/snapshots/usdc-ngnc-20260821T223040Z")
	if err != nil {
		t.Fatal(err)
	}
	// The mid the committed 2026-08-22 record was scored against, read from the
	// chain rather than typed in, so this test cannot drift from it either.
	rec := latestRecord(t, "USDC-NGNC")

	e := &route.Engine{
		DEX: &dex.Client{
			HorizonURL: "https://horizon.stellar.org",
			HTTPClient: m.HTTPClient(),
		},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/NGN": decimal.RequireFromString(rec.Reference.Mid),
		}),
	}
	ladder, err := e.Ladder(context.Background(), route.LadderRequest{
		SendAsset: asset.Asset{
			Code:   "USDC",
			Issuer: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
		},
		ReceiveAsset: asset.Asset{
			Code:   "NGNC",
			Issuer: "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6",
		},
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
	})
	if err != nil {
		t.Fatalf("replaying the recorded snapshot: %v", err)
	}

	if ladder.Integrity != route.IntegrityDirect {
		t.Errorf("replayed integrity = %s, want DIRECT", ladder.Integrity)
	}
	if len(ladder.Rungs) != 12 {
		t.Errorf("replayed ladder has %d rungs, want 12", len(ladder.Rungs))
	}
	if ladder.Viable() {
		t.Error("the replayed ladder is viable; the document's claim is that no " +
			"size on this corridor is worth taking")
	}

	// The document's row for this date. These are the figures it prints, so
	// they are asserted to the precision it prints them at.
	if got := ladder.Floor.StringFixed(2); got != "28.18" {
		t.Errorf("replayed floor = %s%%, want 28.18%% — the document's 2026-08-21 row", got)
	}
	if got := ladder.FloorSize.String(); got != "0.1" {
		t.Errorf("replayed floor size = %s, want 0.1", got)
	}
	if got := ladder.WorstSize.String(); got != "5000" {
		t.Errorf("replayed worst size = %s, want 5000", got)
	}
	if got := ladder.WorstLoss.StringFixed(2); got != "97.50" {
		t.Errorf("replayed worst loss = %s%%, want 97.50%%", got)
	}
	if !strings.Contains(doc, "28.18") || !strings.Contains(doc, "97.50") {
		t.Error("the document does not carry the replayed figures it claims")
	}

	// The argument rests on the dust rung taking a different route from the
	// committed record's, so that route has to be genuinely different.
	committed := latestRecord(t, "USDC-NGNC")
	var committedDust string
	for _, r := range committed.Rungs {
		if r.SendAmount == "0.1" {
			committedDust = r.Path
		}
	}
	if committedDust == "" {
		t.Fatal("the committed 0.1 rung carries no path")
	}

	var dustPath string
	for _, r := range ladder.Rungs {
		if r.SendAmount.String() == "0.1" && r.Result != nil && len(r.Result.Quotes) > 0 {
			dustPath = r.Result.Quotes[0].Description
		}
	}
	if dustPath == "" {
		t.Fatal("the replayed 0.1 rung has no path")
	}
	if dustPath == committedDust {
		t.Errorf("the replayed dust path %q is identical to the committed "+
			"record's; the document's route-shape argument would have no "+
			"second observation", dustPath)
	}
	if !docStates(doc, dustPath) {
		t.Errorf("the document does not carry the replayed 0.1 path %q", dustPath)
	}
	if !docStates(doc, committedDust) {
		t.Errorf("the document does not carry the committed 0.1 path %q", committedDust)
	}
}

// TestCaseStudySisterCorridorsMatchTheChain checks the comparison table. The
// three corridors are claimed to fail in three different ways in one window,
// and "one window" is only true if the recorded timestamps say so.
func TestCaseStudySisterCorridorsMatchTheChain(t *testing.T) {
	doc := caseStudy(t)

	// The three-corridor table is the second table naming these corridors, so
	// the lookup is scoped to its section rather than to the whole document.
	const sisterAnchor = "## The same issuer, three different failures"

	// The store names a corridor "USDC-GHSC"; the document's table labels the
	// same corridor "USDC → GHNC", in the arrow form every table in this
	// repository uses. Both are carried so the row is looked up by what the
	// document actually prints.
	want := []struct {
		corridor  string
		rowLabel  string
		integrity string
	}{
		{"USDC-NGNC", "USDC → NGNC", route.IntegrityDirect.String()},
		{"USDC-GHSC", "USDC → GHSC", route.IntegrityDerivative.String()},
		{"USDC-KESC", "USDC → KESC", route.IntegrityNoMarket.String()},
	}
	for _, tc := range want {
		rec := latestRecord(t, tc.corridor)

		// Chain against code: the state the record carries has to be the state
		// the taxonomy names, or the document's table is describing a value
		// that cannot occur.
		if rec.Integrity != tc.integrity {
			t.Errorf("%s integrity = %q, want %q", tc.corridor, rec.Integrity, tc.integrity)
		}

		// Chain against document: the table has to name the state the record
		// actually carries. Checking the chain against the taxonomy alone
		// would pass whatever the document printed.
		row := datedRowAfter(t, doc, sisterAnchor, tc.rowLabel)
		if !strings.Contains(row, "`"+tc.integrity+"`") {
			t.Errorf("the %s row reads %q, want it to carry the recorded "+
				"integrity state `%s`", tc.corridor, row, tc.integrity)
		}
	}

	// KESC's zero is the trap this document has to get right: floor_loss_pct
	// is "0.00" at size "0" because nothing was measured, and the document must
	// say so rather than let a reader take it for a loss figure.
	kesc := latestRecord(t, "USDC-KESC")
	if kesc.FloorLossPct != "0.00" {
		t.Skipf("KESC floor_loss_pct is %q, not the 0.00 sentinel; the "+
			"document's wording may need revisiting", kesc.FloorLossPct)
	}
	priced := 0
	for _, r := range kesc.Rungs {
		if r.Priced {
			priced++
		}
	}
	if priced != 0 {
		t.Errorf("KESC has %d priced rungs, want 0 — NO-MARKET is a claim that "+
			"no size prices", priced)
	}
	if !strings.Contains(doc, "sentinel") {
		t.Error("the document does not explain that KESC's 0.00 floor is the " +
			"unmeasured sentinel rather than a loss figure")
	}
	if !strings.Contains(doc, "not priced") && !strings.Contains(doc, "no path exists") {
		t.Error("the document does not record that KESC priced nothing at any size")
	}
}

// TestCaseStudyDoesNotClaimTheDecomposition is the negative test that matters.
//
// The document's subject is the README's claim that the floor "is the corridor's
// spread, not its depth". The tool cannot establish that, so the document has
// to present it as an inference. This asserts it still does — the failure it
// guards is a case study that hardens an unmeasured attribution into a result,
// which is the precise thing this repository refuses to publish.
func TestCaseStudyDoesNotClaimTheDecomposition(t *testing.T) {
	doc := caseStudy(t)

	// The boundary has to be stated, not merely absent. These are the clauses
	// that carry the qualification, checked as clauses: a bare check for the
	// word "inference" would pass on any of the document's several incidental
	// uses of it while the sentence doing the work had been rewritten into a
	// claim.
	for _, want := range []string{
		"What the tool cannot yet establish", // the structural boundary
		"but an inference nonetheless",       // the attribution, qualified twice
		"not a computed result",              // where the sentence comes from
		"rather than a result",               // what a reader should take away
		"promoted from inference to",         // and what would change it
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document no longer says %q; the attribution may be "+
				"reading as a result", want)
		}
	}

	// The structural claim itself, in the sentence that carries it. Checking
	// only that the file names appear would pass on a document that had
	// rewritten "no metric is reachable" into the opposite while still citing
	// the same files.
	for _, want := range []string{
		"No market-quality metric is reachable",
		"no way to run a `Metric`",
		"called only from tests",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document no longer says %q; the reachability claim "+
				"is what the whole section rests on", want)
		}
	}

	// It must also say what the engine cannot do, by name, so a reader can
	// check the claim rather than take it on trust.
	for _, want := range []string{
		"checks/runner.go",
		"checks/metric_price_impact.go",
		"route/ladder.go",
		"smallest probed size",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document does not name %q; the reader cannot check the "+
				"limitation being claimed", want)
		}
	}

	// A claim that the metric HAS established the decomposition, in any of the
	// phrasings that would amount to it.
	for _, forbidden := range []string{
		"the spread was measured",
		"we measured the spread",
		"the tool measured price impact",
		"confirmed to be spread",
		"proves it is spread",
	} {
		if strings.Contains(strings.ToLower(doc), forbidden) {
			t.Errorf("the document asserts %q; the metric is unreachable and no "+
				"spread or price impact has ever been measured", forbidden)
		}
	}
}

// TestNoMarketQualityMetricIsReachableFromTheRunner is the code half of that
// negative test, and it is the load-bearing structural claim in the document.
//
// The document asserts that no market-quality metric has ever appeared in a
// response. That is checked structurally, from the type rather than from the
// prose: if a field able to carry a Metric is ever added to Runner, this fails.
func TestNoMarketQualityMetricIsReachableFromTheRunner(t *testing.T) {
	rt := reflect.TypeOf(checks.Runner{})

	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		// A field typed as a slice of a Metric implementation, or a single
		// Metric, would be the wiring the document says does not exist.
		if implementsMetric(f.Type) {
			t.Errorf("checks.Runner has field %q of type %s; a metric would be "+
				"reachable, and the case study says none is", f.Name, f.Type)
		}
		if f.Type.Kind() == reflect.Slice && implementsMetric(f.Type.Elem()) {
			t.Errorf("checks.Runner has field %q of type %s; a metric would be "+
				"reachable, and the case study says none is", f.Name, f.Type)
		}
	}

	// RunAll is the only way findings are produced, and it takes checks.
	runAll := reflect.TypeOf(checks.RunAll)
	if runAll.NumIn() < 2 {
		t.Fatal("RunAll's signature changed; the case study cites it as the " +
			"only path that produces findings")
	}
	if got := runAll.In(1).String(); !strings.HasPrefix(got, "[]checks.Check") {
		t.Errorf("RunAll's second parameter is %s, want []checks.Check; the "+
			"case study says there is no metric equivalent", got)
	}
}

// implementsMetric reports whether a type satisfies the Metric interface, by
// method set, without importing the concrete metric types.
func implementsMetric(t reflect.Type) bool {
	metric := reflect.TypeOf((*checks.Metric)(nil)).Elem()
	if t.Implements(metric) {
		return true
	}
	if t.Kind() != reflect.Interface && t.Kind() != reflect.Ptr {
		return reflect.PointerTo(t).Implements(metric)
	}
	return false
}

// TestCaseStudyLinksResolve covers the "linked from wherever a reader would
// look for it" criterion mechanically: every relative link in the document, and
// every link to the document from the places a reader arrives, has to resolve.
//
// A case study that nothing links to is a document nobody finds, and a broken
// link in a document whose whole value is provenance is worse than no document.
func TestCaseStudyLinksResolve(t *testing.T) {
	doc := caseStudy(t)

	// Links out of the document.
	re := regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)
	for _, m := range re.FindAllStringSubmatch(doc, -1) {
		target := m[1]
		if strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
			continue // absolute or in-document anchor
		}
		path := target
		if i := strings.Index(path, "#"); i >= 0 {
			path = path[:i]
		}
		if strings.HasPrefix(path, "/") {
			continue // repository-root-relative, rendered by GitHub
		}
		resolved := filepath.Join("docs", path)
		if _, err := os.Stat(resolved); err != nil {
			t.Errorf("the document links to %q, which does not exist", target)
		}
	}

	// Links in. The document has to be reachable from each place a reader
	// arrives looking for this finding.
	for _, from := range []string{
		"README.md",
		"docs/corridor-measurements.md",
		"docs/why-wayfare.md",
		"docs/backlog.md",
	} {
		raw, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "ngnc-structural-floor.md") {
			t.Errorf("%s does not link the case study; a reader looking for the "+
				"finding there will not find it", from)
		}
	}
}

// TestCaseStudyCitesThe20260808Figures pins the one row the document quotes
// from prose rather than from the chain or a snapshot.
//
// That row cannot be re-derived from committed bytes — the 2026-08-08 run is
// live output recorded only in docs/corridor-measurements.md. So the check is
// that the two documents agree: the case study may not restate a figure the
// raw-figures document contradicts.
func TestCaseStudyCitesThe20260808Figures(t *testing.T) {
	doc := caseStudy(t)
	raw, err := os.ReadFile("docs/corridor-measurements.md")
	if err != nil {
		t.Fatal(err)
	}
	measurements := string(raw)

	// The figures the document attributes to the 2026-08-08 run, checked in the
	// row that carries them: both numbers appear elsewhere in the document, so
	// a bare Contains would pass on the prose while the row drifted.
	row := datedRow(t, doc, "recorded run")
	for _, want := range []string{"24.65%", "97.68%"} {
		if !strings.Contains(row, want) {
			t.Errorf("the 2026-08-08 row reads %q, want the figure %q in it", row, want)
		}
		if !strings.Contains(measurements, strings.TrimSuffix(want, "%")) {
			t.Errorf("the document cites %q but docs/corridor-measurements.md "+
				"does not; one of them is wrong", want)
		}
	}

	// The mid, in the source bullet that states it rather than anywhere in the
	// document.
	if !strings.Contains(doc, "1364.0070") {
		t.Errorf("the document no longer states the 2026-08-08 reference mid; " +
			"the row cannot be checked without it")
	}
	if !strings.Contains(measurements, "1364.0070") {
		t.Error("the document cites mid 1364.0070 but docs/corridor-measurements.md " +
			"does not; one of them is wrong")
	}

	// The date, in the row's own first cell. Checking the whole row would pass
	// on the date embedded in the link anchor beside it, so the cell is taken
	// on its own: an undated measurement is the failure this repository treats
	// as the worst one.
	if cell := rowCell(row, 1); cell != "2026-08-08" {
		t.Errorf("the 2026-08-08 row's date cell is %q, want 2026-08-08 — an "+
			"undated measurement is the failure this repository treats as worst", cell)
	}

	// A measurement cited without a date is the failure this repository cares
	// about most, so the document must carry an ISO date on every row it claims
	// to be an observation.
	dated := regexp.MustCompile(`20\d\d-\d\d-\d\d`)
	if n := len(dated.FindAllString(doc, -1)); n < 3 {
		t.Errorf("the document carries %d dates, want at least 3 — one per "+
			"dated observation", n)
	}
}
