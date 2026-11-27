package wayfare

// The headline findings in docs/corridor-measurements.md and
// docs/ngnc-structural-floor.md are the project's argument. Issue #702: if a
// pricing change alters what a snapshot produces, the documents become wrong
// silently. This file closes that gap.
//
// Every table in both documents is preceded by a marker the renderer hides:
//
//	<!-- figures: id=<unique id>; kind=<kind>; key=value; ... -->
//
// and every figure in the table is recomputed from the source the marker
// names, through the same route.Engine the product uses:
//
//   - kind=snapshot replays recorded Horizon bytes and requires every cell to
//     equal the engine's output, formatted as cmd/ladder prints it.
//   - kind=live has no recorded bytes — the runs were never snapshotted — so
//     the observed receive amounts are served to the engine by a stub Horizon,
//     and every column the engine derives from them (effective rate, loss,
//     verdict, integrity) is regraded. A change to how loss is computed, how
//     it is clamped, or where a verdict band begins moves these figures and
//     fails here.
//   - kind=derived recomputes a table from the tables it cites.
//   - kind=chain reads the committed hash chain in data/.
//   - kind=observation is a read with no recorded bytes; it carries no
//     computed figure, and the tables that cite it are checked against it.
//
// Prose figures are derived the same way, from the verified tables, and a
// percentage or dollar figure added to the prose without a derivation fails
// the build: an unannotated figure is exactly the drift this guards against.
//
// The negative tests at the bottom are the acceptance criterion made
// executable: a pricing change that moves a published figure fails CI.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

var figureDocs = []string{
	"docs/corridor-measurements.md",
	"docs/ngnc-structural-floor.md",
}

// figureMarkersHeading introduces the section documenting the marker syntax.
// Its table describes the markers rather than citing figures, so it is the one
// table exempt from carrying one.
const figureMarkersHeading = "## The figure markers"

// figureReporter is the subset of *testing.T a check needs. The negative tests
// pass a recorder instead, so they can assert that a check fails without
// failing themselves.
type figureReporter interface {
	Helper()
	Errorf(format string, args ...any)
}

type figureRecorder struct{ errs []string }

func (r *figureRecorder) Helper() {}
func (r *figureRecorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

// ---------------------------------------------------------------------------
// Parsing

type figureTable struct {
	doc    string
	line   int
	attrs  map[string]string
	header []string
	rows   [][]string
}

func (ft *figureTable) id() string { return ft.attrs["id"] }

func (ft *figureTable) where() string {
	return fmt.Sprintf("%s:%d (%s)", ft.doc, ft.line, ft.id())
}

// col returns the index of the first header cell starting with prefix.
func (ft *figureTable) col(prefix string) int {
	for i, h := range ft.header {
		if strings.HasPrefix(h, prefix) {
			return i
		}
	}
	return -1
}

func (ft *figureTable) cell(row []string, prefix string) string {
	i := ft.col(prefix)
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}

// rowWhere returns the first row whose first cell equals key.
func (ft *figureTable) rowWhere(key string) []string {
	for _, r := range ft.rows {
		if len(r) > 0 && r[0] == key {
			return r
		}
	}
	return nil
}

type figureSet struct {
	docs     map[string]string
	tables   map[string]*figureTable
	ordered  []*figureTable
	problems []string // structural annotation problems, reported by the annotation test
}

var figureMarkerRE = regexp.MustCompile(`^<!-- figures: (.+) -->$`)

func parseFigureAttrs(spec string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(spec, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, _ := strings.Cut(part, "=")
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
	}
	return out
}

func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

func parseFigureDoc(set *figureSet, path, raw string) {
	lines := strings.Split(raw, "\n")
	var (
		inFence bool
		pending map[string]string
		pendLn  int
		heading string
	)
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			heading = trimmed
		}
		if m := figureMarkerRE.FindStringSubmatch(trimmed); m != nil {
			if pending != nil {
				set.problems = append(set.problems, fmt.Sprintf(
					"%s:%d: figures marker is not followed by a table", path, pendLn))
			}
			pending, pendLn = parseFigureAttrs(m[1]), i+1
			continue
		}
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "|") {
			if pending != nil {
				set.problems = append(set.problems, fmt.Sprintf(
					"%s:%d: figures marker is not immediately followed by a table", path, pendLn))
				pending = nil
			}
			continue
		}

		// A table: header, separator, rows.
		start := i
		var block []string
		for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
			block = append(block, lines[i])
		}
		i-- // the loop increments

		if pending == nil {
			if heading != figureMarkersHeading {
				set.problems = append(set.problems, fmt.Sprintf(
					"%s:%d: table has no figures marker; every table of figures "+
						"must name its source so CI can recompute it (#702)", path, start+1))
			}
			continue
		}
		ft := &figureTable{doc: path, line: start + 1, attrs: pending, header: splitTableRow(block[0])}
		for _, r := range block[min(2, len(block)):] {
			ft.rows = append(ft.rows, splitTableRow(r))
		}
		pending = nil

		id := ft.id()
		if id == "" {
			set.problems = append(set.problems, fmt.Sprintf("%s:%d: figures marker has no id", path, start+1))
			continue
		}
		if prev, dup := set.tables[id]; dup {
			set.problems = append(set.problems, fmt.Sprintf(
				"%s: figures id %q is already used at %s", ft.where(), id, prev.where()))
			continue
		}
		set.tables[id] = ft
		set.ordered = append(set.ordered, ft)
	}
	if pending != nil {
		set.problems = append(set.problems, fmt.Sprintf(
			"%s:%d: figures marker at end of document has no table", path, pendLn))
	}
}

func loadFigureSet(t *testing.T) *figureSet {
	t.Helper()
	set := &figureSet{docs: map[string]string{}, tables: map[string]*figureTable{}}
	for _, p := range figureDocs {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		set.docs[p] = string(raw)
		parseFigureDoc(set, p, string(raw))
	}
	return set
}

// ---------------------------------------------------------------------------
// Cell helpers

var figureNumberRE = regexp.MustCompile(`-?\d[\d,]*(?:\.\d+)?`)

// figNum reads the first number in a cell, ignoring thousands separators,
// emphasis, units and the percent sign.
func figNum(cell string) (decimal.Decimal, bool) {
	m := figureNumberRE.FindString(cell)
	if m == "" {
		return decimal.Zero, false
	}
	d, err := decimal.NewFromString(strings.ReplaceAll(m, ",", ""))
	return d, err == nil
}

// withThousands formats a fixed-point string the way the prose tables print
// large amounts: 65100.14 -> 65,100.14.
func withThousands(s string) string {
	intPart, frac, hasFrac := strings.Cut(s, ".")
	neg := strings.HasPrefix(intPart, "-")
	intPart = strings.TrimPrefix(intPart, "-")
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if neg {
		out = "-" + out
	}
	if hasFrac {
		out += "." + frac
	}
	return out
}

// asDocPath folds the recorded "->" arrow into the documents' "→".
func asDocPath(p string) string { return strings.ReplaceAll(p, " -> ", " → ") }

func hopsLabel(path string) string {
	n := strings.Count(path, "→")
	if n == 1 {
		return "1 hop"
	}
	return fmt.Sprintf("%d hops", n)
}

func figureAsset(rep figureReporter, ft *figureTable, key string) (asset.Asset, bool) {
	rep.Helper()
	code := ft.attrs[key]
	a, ok := asset.Lookup(code)
	if !ok {
		rep.Errorf("%s: %s=%q is not in the asset registry", ft.where(), key, code)
	}
	return a, ok
}

func figureEngine(horizon string, client *http.Client, pair string, mid decimal.Decimal) *route.Engine {
	return &route.Engine{
		DEX: &dex.Client{HorizonURL: horizon, HTTPClient: client},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			pair: mid,
		}),
	}
}

func figureMid(rep figureReporter, ft *figureTable) (decimal.Decimal, bool) {
	rep.Helper()
	mid, err := decimal.NewFromString(ft.attrs["mid"])
	if err != nil {
		rep.Errorf("%s: mid=%q is not a number", ft.where(), ft.attrs["mid"])
		return decimal.Zero, false
	}
	return mid, true
}

func latestChainRecord(corridor string) (*runstore.Record, error) {
	s, err := runstore.OpenFS(History, "data")
	if err != nil {
		return nil, err
	}
	rec, err := s.Latest(context.Background(), corridor)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("no %s record in the committed chain", corridor)
	}
	return rec, nil
}

// ---------------------------------------------------------------------------
// Ladder tables: kind=snapshot and kind=live

// ladderSizes reads the Send column, which every ladder table leads with.
func ladderSizes(rep figureReporter, ft *figureTable) []decimal.Decimal {
	rep.Helper()
	var out []decimal.Decimal
	for _, r := range ft.rows {
		d, err := decimal.NewFromString(r[0])
		if err != nil {
			rep.Errorf("%s: send cell %q is not a size", ft.where(), r[0])
			continue
		}
		out = append(out, d)
	}
	return out
}

func runFigureLadder(rep figureReporter, ft *figureTable, e *route.Engine, sizes []decimal.Decimal, send, receive asset.Asset) *route.LadderResult {
	rep.Helper()
	base, quote, _ := strings.Cut(ft.attrs["pair"], "/")
	ladder, err := e.Ladder(context.Background(), route.LadderRequest{
		SendAsset:      send,
		ReceiveAsset:   receive,
		Sizes:          sizes,
		ReferenceBase:  base,
		ReferenceQuote: quote,
	})
	if err != nil {
		rep.Errorf("%s: running the ladder: %v", ft.where(), err)
		return nil
	}
	if len(ladder.Rungs) != len(ft.rows) {
		rep.Errorf("%s: the engine priced %d rungs and the table prints %d",
			ft.where(), len(ladder.Rungs), len(ft.rows))
		return nil
	}
	return ladder
}

// checkLadderWide holds the claims a marker makes about a whole table.
func checkLadderWide(rep figureReporter, ft *figureTable) {
	rep.Helper()
	if want := ft.attrs["verdicts"]; want != "" {
		for _, r := range ft.rows {
			if got := ft.cell(r, "Verdict"); got != want {
				rep.Errorf("%s: the marker says every verdict is %s; the %s row reads %s",
					ft.where(), want, r[0], got)
			}
		}
	}
	if want := ft.attrs["integrity"]; want != "" && ft.col("Integrity") >= 0 {
		for _, r := range ft.rows {
			if got := ft.cell(r, "Integrity"); got != want {
				rep.Errorf("%s: the marker says integrity is %s; the %s row reads %s",
					ft.where(), want, r[0], got)
			}
		}
	}
	if ft.col("Best path") >= 0 {
		for _, r := range ft.rows {
			p := ft.cell(r, "Best path")
			if send := ft.attrs["send"]; send != "" && !strings.HasPrefix(p, send+" → ") {
				rep.Errorf("%s: the %s row's path %q does not start at %s", ft.where(), r[0], p, send)
			}
			if recv := ft.attrs["receive"]; recv != "" && !strings.HasSuffix(p, " → "+recv) {
				rep.Errorf("%s: the %s row's path %q does not end at %s", ft.where(), r[0], p, recv)
			}
			if via := ft.attrs["via"]; via != "" && !strings.Contains(p, " → "+via+" → ") {
				rep.Errorf("%s: the marker says every path runs via %s; the %s row's path is %q",
					ft.where(), via, r[0], p)
			}
		}
	}
}

// checkSnapshotTable replays recorded bytes and requires an exact match.
func checkSnapshotTable(rep figureReporter, ft *figureTable, mid decimal.Decimal) {
	rep.Helper()
	m, err := snapshot.Load(ft.attrs["snapshot"])
	if err != nil {
		rep.Errorf("%s: loading the snapshot: %v", ft.where(), err)
		return
	}
	send := asset.Asset{Code: m.Corridor.Send.Code, Issuer: m.Corridor.Send.Issuer}
	receive := asset.Asset{Code: m.Corridor.Receive.Code, Issuer: m.Corridor.Receive.Issuer}

	ladder := runFigureLadder(rep, ft, figureEngine("https://horizon.stellar.org", m.HTTPClient(), ft.attrs["pair"], mid),
		ladderSizes(rep, ft), send, receive)
	if ladder == nil {
		return
	}
	if want := ft.attrs["integrity"]; want != "" && ladder.Integrity.String() != want {
		rep.Errorf("%s: replayed integrity is %s, the marker says %s", ft.where(), ladder.Integrity, want)
	}
	if ft.attrs["verdicts"] == route.VerdictUnusable.String() && ladder.Viable() {
		rep.Errorf("%s: the replayed ladder recommends a size; the table says none is usable", ft.where())
	}

	for i, r := range ft.rows {
		rung := ladder.Rungs[i]
		if rung.Result == nil || len(rung.Result.Quotes) == 0 {
			rep.Errorf("%s: the %s rung did not price on replay", ft.where(), r[0])
			continue
		}
		q := rung.Result.Quotes[0]
		want := map[string]string{
			"Receive":        q.ReceiveAmount.StringFixed(2),
			"Effective rate": q.EffectiveRate.StringFixed(2),
			"Loss vs mid":    q.LossPct.StringFixed(2) + "%",
			"Verdict":        q.Verdict.String(),
			"Integrity":      rung.Result.Integrity.String(),
			"Best path":      asDocPath(q.Description),
		}
		for col, w := range want {
			if ft.col(col) < 0 {
				continue
			}
			if got := ft.cell(r, col); got != w {
				rep.Errorf("%s: the %s row's %q is %s; replaying the snapshot produces %s",
					ft.where(), r[0], col, got, w)
			}
		}
	}
	checkLadderWide(rep, ft)
}

// stubHorizon serves, for each send size, a single direct strict-send record
// paying the given receive amount. It stands in for Horizon where the
// observation is in the document but the bytes were never recorded.
func stubHorizon(send, receive asset.Asset, pay map[string]string) *httptest.Server {
	typ := func(a asset.Asset) string {
		if len(a.Code) <= 4 {
			return "credit_alphanum4"
		}
		return "credit_alphanum12"
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/paths/strict-send" {
			http.NotFound(w, r)
			return
		}
		amount := r.URL.Query().Get("source_amount")
		d, err := decimal.NewFromString(amount)
		dest, ok := pay[d.String()]
		if err != nil || !ok {
			http.NotFound(w, r)
			return
		}
		record := map[string]any{
			"source_asset_type":        typ(send),
			"source_asset_code":        send.Code,
			"source_asset_issuer":      send.Issuer,
			"source_amount":            amount,
			"destination_asset_type":   typ(receive),
			"destination_asset_code":   receive.Code,
			"destination_asset_issuer": receive.Issuer,
			"destination_amount":       dest,
			"path":                     []any{},
		}
		w.Header().Set("Content-Type", "application/hal+json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_embedded": map[string]any{"records": []any{record}},
		})
	}))
}

// checkLiveTable regrades a run that was never recorded. The receive amounts
// are the observation; everything the engine computes from them is asserted.
//
// The receive amounts are printed to two decimals, so the engine sees a value
// up to 0.005 away from the one it measured. Each comparison allows exactly
// the error that rounding can introduce, plus the half-unit of the printed
// figure itself, and no more. Verdicts are compared exactly.
func checkLiveTable(rep figureReporter, ft *figureTable, mid decimal.Decimal) {
	rep.Helper()
	send, ok1 := figureAsset(rep, ft, "send")
	receive, ok2 := figureAsset(rep, ft, "receive")
	if !ok1 || !ok2 {
		return
	}
	pay := map[string]string{}
	for _, r := range ft.rows {
		size, err := decimal.NewFromString(r[0])
		amt, ok := figNum(ft.cell(r, "Receive"))
		if err != nil || !ok {
			rep.Errorf("%s: row %q has no readable size and receive amount", ft.where(), r[0])
			return
		}
		pay[size.String()] = amt.String()
	}
	srv := stubHorizon(send, receive, pay)
	defer srv.Close()

	e := figureEngine(srv.URL, srv.Client(), ft.attrs["pair"], mid)
	ladder := runFigureLadder(rep, ft, e, ladderSizes(rep, ft), send, receive)
	if ladder == nil {
		return
	}

	half := decimal.RequireFromString("0.005")
	hundred := decimal.NewFromInt(100)
	for i, r := range ft.rows {
		rung := ladder.Rungs[i]
		if rung.Result == nil || len(rung.Result.Quotes) == 0 {
			rep.Errorf("%s: the %s rung did not price through the stub", ft.where(), r[0])
			continue
		}
		q := rung.Result.Quotes[0]
		size := rung.SendAmount

		// Rate error from a receive amount rounded to 0.005.
		rateTol := half.Div(size).Add(half)
		if c := ft.cell(r, "Effective rate"); c != "" {
			got, _ := figNum(c)
			if got.Sub(q.EffectiveRate).Abs().GreaterThan(rateTol) {
				rep.Errorf("%s: the %s row's effective rate is %s; the engine computes %s from "+
					"the printed receive amount (tolerance %s)",
					ft.where(), r[0], c, q.EffectiveRate.StringFixed(4), rateTol.StringFixed(4))
			}
		}
		if c := ft.cell(r, "Loss vs mid"); c != "" {
			got, _ := figNum(c)
			lossTol := half.Div(size).Div(mid).Mul(hundred).Add(half)
			if got.Sub(q.LossPct).Abs().GreaterThan(lossTol) {
				rep.Errorf("%s: the %s row's loss is %s; the engine computes %s%% against "+
					"mid %s (tolerance %s)",
					ft.where(), r[0], c, q.LossPct.StringFixed(4), mid, lossTol.StringFixed(4))
			}
		}
		if c := ft.cell(r, "Verdict"); c != "" && c != q.Verdict.String() {
			rep.Errorf("%s: the %s row's verdict is %s; the engine grades it %s",
				ft.where(), r[0], c, q.Verdict)
		}
	}
	checkLadderWide(rep, ft)
}

// ---------------------------------------------------------------------------
// Derived, chain and observation checks

type figureCheck func(rep figureReporter, set *figureSet, ft *figureTable)

var figureChecks = map[string]figureCheck{
	"marginal":          checkMarginal,
	"comparison":        checkComparison,
	"depth":             checkDepth,
	"failure-modes":     checkFailureModes,
	"pattern":           checkPattern,
	"case-study-floors": checkCaseStudyFloors,
	"sister-corridors":  checkSisterCorridors,
	"ladder-range":      checkLadderRange,
}

func fromTable(rep figureReporter, set *figureSet, ft *figureTable, id string) *figureTable {
	rep.Helper()
	found := false
	for _, f := range strings.Split(ft.attrs["from"], ",") {
		if strings.TrimSpace(f) == id {
			found = true
		}
	}
	if !found {
		rep.Errorf("%s: the check reads %s but the marker's from= does not name it", ft.where(), id)
	}
	src := set.tables[id]
	if src == nil {
		rep.Errorf("%s: cites %s, which no figures marker defines", ft.where(), id)
	}
	return src
}

func receiveAt(src *figureTable, size string) decimal.Decimal {
	d, _ := figNum(src.cell(src.rowWhere(size), "Receive"))
	return d
}

func expectCell(rep figureReporter, ft *figureTable, rowKey, col, got, want string) {
	rep.Helper()
	if got != want {
		rep.Errorf("%s: the %s row's %q is %q; recomputed from its sources it is %q",
			ft.where(), rowKey, col, got, want)
	}
}

// checkMarginal: each "a → b" row is the difference between two rungs.
func checkMarginal(rep figureReporter, set *figureSet, ft *figureTable) {
	rep.Helper()
	src := fromTable(rep, set, ft, "ngnc-20260808")
	if src == nil {
		return
	}
	for _, r := range ft.rows {
		a, b, ok := strings.Cut(r[0], " → ")
		if !ok || src.rowWhere(a) == nil || src.rowWhere(b) == nil {
			rep.Errorf("%s: step %q does not name two rungs of %s", ft.where(), r[0], src.id())
			continue
		}
		sent := decimal.RequireFromString(b).Sub(decimal.RequireFromString(a))
		got := receiveAt(src, b).Sub(receiveAt(src, a))
		expectCell(rep, ft, r[0], "Extra USDC sent", ft.cell(r, "Extra USDC sent"), sent.String())
		expectCell(rep, ft, r[0], "Extra NGNC received", ft.cell(r, "Extra NGNC received"), withThousands(got.StringFixed(2)))
		expectCell(rep, ft, r[0], "Marginal rate", ft.cell(r, "Marginal rate"), got.Div(sent).StringFixed(2))
	}
}

// recordedRouteFixture reads the 2026-08-04 Horizon body committed in the
// route package's tests: the only bytes the repository holds from that day.
func recordedRouteFixture(rep figureReporter, ft *figureTable) string {
	rep.Helper()
	ref := ft.attrs["recorded-20260804"]
	file, name, _ := strings.Cut(ref, ":")
	raw, err := os.ReadFile(file)
	if err != nil {
		rep.Errorf("%s: reading %s: %v", ft.where(), file, err)
		return ""
	}
	m := regexp.MustCompile("(?s)const " + regexp.QuoteMeta(name) + " = `(.*?)`").FindStringSubmatch(string(raw))
	if m == nil {
		rep.Errorf("%s: %s no longer declares %s; the 2026-08-04 column has lost its source",
			ft.where(), file, name)
		return ""
	}
	return m[1]
}

// checkComparison: the 2026-08-08 column restates its run's 100 USDC rung; the
// 2026-08-04 column replays the recorded body through the engine.
func checkComparison(rep figureReporter, set *figureSet, ft *figureTable) {
	rep.Helper()
	src := fromTable(rep, set, ft, "ngnc-20260808")
	if src == nil {
		return
	}
	row := func(label string) []string {
		for _, r := range ft.rows {
			if r[0] == label {
				return r
			}
		}
		rep.Errorf("%s: no %q row", ft.where(), label)
		return nil
	}
	recv, rate, midRow, loss := row("100 USDC receives"), row("Effective rate"), row("Reference mid"), row("Loss")
	if recv == nil || rate == nil || midRow == nil || loss == nil {
		return
	}

	// 2026-08-08, from the run's own table.
	r100 := src.rowWhere("100")
	col := ft.col("2026-08-08")
	expectCell(rep, ft, "100 USDC receives", "2026-08-08", recv[col],
		withThousands(receiveAt(src, "100").StringFixed(2))+" NGNC")
	expectCell(rep, ft, "Effective rate", "2026-08-08", rate[col], src.cell(r100, "Effective rate"))
	expectCell(rep, ft, "Reference mid", "2026-08-08", midRow[col], src.attrs["mid"])
	expectCell(rep, ft, "Loss", "2026-08-08", loss[col], src.cell(r100, "Loss vs mid"))

	// 2026-08-04, replayed from the recorded body.
	body := recordedRouteFixture(rep, ft)
	if body == "" {
		return
	}
	mid, err := decimal.NewFromString(ft.attrs["mid-20260804"])
	if err != nil {
		rep.Errorf("%s: mid-20260804 is not a number", ft.where())
		return
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/hal+json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	e := figureEngine(srv.URL, srv.Client(), "USD/NGN", mid)
	usdc, _ := asset.Lookup("USDC")
	ngnc, _ := asset.Lookup("NGNC")
	res, err := e.Quote(context.Background(), route.Request{
		SendAsset: usdc, ReceiveAsset: ngnc, SendAmount: decimal.NewFromInt(100),
		ReferenceBase: "USD", ReferenceQuote: "NGN",
	})
	if err != nil || len(res.Quotes) == 0 {
		rep.Errorf("%s: replaying the 2026-08-04 body: %v", ft.where(), err)
		return
	}
	q := res.Quotes[0]
	col = ft.col("2026-08-04")
	expectCell(rep, ft, "100 USDC receives", "2026-08-04", recv[col], withThousands(q.ReceiveAmount.StringFixed(2))+" NGNC")
	expectCell(rep, ft, "Effective rate", "2026-08-04", rate[col], q.EffectiveRate.StringFixed(2))
	expectCell(rep, ft, "Reference mid", "2026-08-04", midRow[col], ft.attrs["mid-20260804"])
	expectCell(rep, ft, "Loss", "2026-08-04", loss[col], q.LossPct.StringFixed(1)+"%")
}

// checkDepth: the largest receive amount per corridor, valued at its mid.
func checkDepth(rep figureReporter, set *figureSet, ft *figureTable) {
	rep.Helper()
	sources := map[string]*figureTable{
		"NGNC": fromTable(rep, set, ft, "ngnc-20260808-window"),
		"GHSC": fromTable(rep, set, ft, "ghsc-20260808"),
	}
	kesc := fromTable(rep, set, ft, "kesc-20260808")
	if sources["NGNC"] == nil || sources["GHSC"] == nil || kesc == nil {
		return
	}

	total := decimal.Zero
	for _, r := range ft.rows {
		code := r[0]
		if code == "**Total**" {
			want := "**$" + total.StringFixed(2) + "**"
			expectCell(rep, ft, code, "Value", ft.cell(r, "Value"), want)
			continue
		}
		src := sources[code]
		if src == nil {
			if code != "KESC" {
				rep.Errorf("%s: unexpected corridor row %q", ft.where(), code)
				continue
			}
			// No market: nothing to value. The observation it rests on must
			// still say so.
			if !strings.Contains(kesc.cell(kesc.rows[0], "Result"), "NO ROUTE") {
				rep.Errorf("%s: KESC is valued at zero but %s does not record NO ROUTE", ft.where(), kesc.id())
			}
			expectCell(rep, ft, code, "Max receive", ft.cell(r, "Max receive"), "0")
			expectCell(rep, ft, code, "Value", ft.cell(r, "Value"), "$0.00")
			continue
		}
		maxRecv, maxAt := decimal.Zero, ""
		for _, sr := range src.rows {
			if v := receiveAt(src, sr[0]); v.GreaterThan(maxRecv) {
				maxRecv, maxAt = v, sr[0]
			}
		}
		if maxAt != "5000" {
			rep.Errorf("%s: the header says the maximum is at 5000 USDC; in %s it is at %s",
				ft.where(), src.id(), maxAt)
		}
		mid := decimal.RequireFromString(src.attrs["mid"])
		value := decimal.RequireFromString(maxRecv.Div(mid).StringFixed(2))
		total = total.Add(value)
		expectCell(rep, ft, code, "Max receive", ft.cell(r, "Max receive"), withThousands(maxRecv.StringFixed(2))+" "+code)
		expectCell(rep, ft, code, "At mid", ft.cell(r, "At mid"), src.attrs["mid"])
		expectCell(rep, ft, code, "Value", ft.cell(r, "Value"), "$"+value.StringFixed(2))
	}
}

func floorAndWorst(src *figureTable) (floor, worst []string) {
	floor = src.rows[0]
	worst = src.rows[0]
	for _, r := range src.rows {
		a, _ := figNum(src.cell(r, "Loss vs mid"))
		b, _ := figNum(src.cell(worst, "Loss vs mid"))
		if a.GreaterThan(b) {
			worst = r
		}
	}
	return floor, worst
}

func rowByCorridor(rep figureReporter, ft *figureTable, label string) []string {
	rep.Helper()
	for _, r := range ft.rows {
		for _, c := range r {
			if c == label {
				return r
			}
		}
	}
	rep.Errorf("%s: no row for %s", ft.where(), label)
	return nil
}

func checkFailureModes(rep figureReporter, set *figureSet, ft *figureTable) {
	rep.Helper()
	ngnc := fromTable(rep, set, ft, "ngnc-20260808-window")
	ghsc := fromTable(rep, set, ft, "ghsc-20260808")
	kesc := fromTable(rep, set, ft, "kesc-20260808")
	if ngnc == nil || ghsc == nil || kesc == nil {
		return
	}
	if r := rowByCorridor(rep, ft, "USDC → NGNC"); r != nil {
		floor, worst := floorAndWorst(ngnc)
		obs := ft.cell(r, "Observation")
		for _, want := range []string{
			ngnc.cell(floor, "Loss vs mid") + " at " + floor[0] + " USDC",
			ngnc.cell(worst, "Loss vs mid") + " at " + worst[0],
		} {
			if !strings.Contains(obs, want) {
				rep.Errorf("%s: the NGNC observation does not carry %q from %s", ft.where(), want, ngnc.id())
			}
		}
	}
	if r := rowByCorridor(rep, ft, "USDC → GHSC"); r != nil {
		_, worst := floorAndWorst(ghsc)
		want := "`" + ghsc.cell(worst, "Best path") + "`"
		if !strings.Contains(ft.cell(r, "Observation"), want) {
			rep.Errorf("%s: the GHSC observation does not quote the recorded path %s", ft.where(), want)
		}
		if ghsc.attrs["via"] != "NGNC" {
			rep.Errorf("%s: the GHSC observation says every path traverses NGNC but %s does not assert via=NGNC",
				ft.where(), ghsc.id())
		}
	}
	if r := rowByCorridor(rep, ft, "USDC → KESC"); r != nil {
		if !strings.Contains(ft.cell(r, "Observation"), "zero paths") ||
			!strings.Contains(kesc.cell(kesc.rows[0], "Result"), "NO ROUTE") {
			rep.Errorf("%s: the KESC observation and %s disagree about whether any path exists", ft.where(), kesc.id())
		}
	}
}

func checkPattern(rep figureReporter, set *figureSet, ft *figureTable) {
	rep.Helper()
	sep1 := fromTable(rep, set, ft, "issuer-sep1")
	ladders := map[string]*figureTable{
		"NGNC": fromTable(rep, set, ft, "ngnc-20260808-window"),
		"GHSC": fromTable(rep, set, ft, "ghsc-20260808"),
	}
	kesc := fromTable(rep, set, ft, "kesc-20260808")
	if sep1 == nil || ladders["NGNC"] == nil || ladders["GHSC"] == nil || kesc == nil {
		return
	}
	for _, code := range []string{"NGNC", "GHSC", "KESC"} {
		r := rowByCorridor(rep, ft, "USDC → "+code)
		if r == nil {
			continue
		}
		status := strings.Trim(sep1.cell(sep1.rowWhere(code), "`status`"), "`")
		expectCell(rep, ft, code, "Issuer status", ft.cell(r, "Issuer status"), status)

		src := ladders[code]
		if src == nil {
			expectCell(rep, ft, code, "Best result", ft.cell(r, "Best result"), "no route at any size")
			expectCell(rep, ft, code, "Structural floor", ft.cell(r, "Structural floor"), "—")
			expectCell(rep, ft, code, "Verdict", ft.cell(r, "Verdict"), "No market")
			continue
		}
		floor, _ := floorAndWorst(src)
		loss := src.cell(floor, "Loss vs mid")
		lossN, _ := figNum(loss)
		expectCell(rep, ft, code, "Best result", ft.cell(r, "Best result"), loss+" loss at "+floor[0]+" USDC")
		expectCell(rep, ft, code, "Structural floor", ft.cell(r, "Structural floor"), "~"+lossN.Round(0).String()+"%")
		allUnusable := true
		for _, sr := range src.rows {
			if src.cell(sr, "Verdict") != route.VerdictUnusable.String() {
				allUnusable = false
			}
		}
		if allUnusable {
			expectCell(rep, ft, code, "Verdict", ft.cell(r, "Verdict"), "Unusable at every size")
		} else if strings.Contains(ft.cell(r, "Verdict"), "every size") {
			rep.Errorf("%s: %s is called unusable at every size but %s has a usable rung", ft.where(), code, src.id())
		}
	}
}

// checkCaseStudyFloors: each dated row of the case study's table restates a
// floor from a different source; every one of them is recomputed.
func checkCaseStudyFloors(rep figureReporter, set *figureSet, ft *figureTable) {
	rep.Helper()
	cmp := fromTable(rep, set, ft, "ngnc-comparison")
	live := fromTable(rep, set, ft, "ngnc-20260808")
	replay := fromTable(rep, set, ft, "ngnc-20260821-replay")
	if cmp == nil || live == nil || replay == nil {
		return
	}

	type floorRow struct{ path, floor, worst string }
	fromLadder := func(src *figureTable) floorRow {
		floor, worst := floorAndWorst(src)
		p := src.cell(floor, "Best path")
		return floorRow{
			path:  "`" + p + "`",
			floor: "**" + src.cell(floor, "Loss vs mid") + "**",
			worst: src.cell(worst, "Loss vs mid") + " at " + worst[0],
		}
	}

	rec, err := latestChainRecord(ft.attrs["chain"])
	if err != nil {
		rep.Errorf("%s: %v", ft.where(), err)
		return
	}
	var dust string
	for _, r := range rec.Rungs {
		if r.SendAmount == rec.FloorSize {
			dust = asDocPath(r.Path)
		}
	}
	lossRow := cmp.rows[len(cmp.rows)-1]
	want := map[string]floorRow{
		"2026-08-04": {path: "—", floor: "—", worst: lossRow[cmp.col("2026-08-04")] + " at 100 USDC"},
		"2026-08-08": fromLadder(live),
		"2026-08-21": fromLadder(replay),
		"2026-08-22": {
			path:  "`" + dust + "`",
			floor: "**" + rec.FloorLossPct + "%**",
			worst: rec.WorstLossPct + "% at " + rec.WorstSize,
		},
	}
	if rec.RecordedAt.UTC().Format("2006-01-02") != "2026-08-22" {
		rep.Errorf("%s: the latest %s record is dated %s; the table's chain row is 2026-08-22",
			ft.where(), ft.attrs["chain"], rec.RecordedAt.UTC())
	}

	seen := 0
	for _, r := range ft.rows {
		w, ok := want[r[0]]
		if !ok {
			rep.Errorf("%s: row dated %s has no source the check knows", ft.where(), r[0])
			continue
		}
		seen++
		pathCell := ft.cell(r, "0.1 USDC rung")
		if w.path == "—" {
			expectCell(rep, ft, r[0], "path", pathCell, "—")
		} else {
			if !strings.HasPrefix(pathCell, w.path) {
				rep.Errorf("%s: the %s row's path is %q; its source records %s", ft.where(), r[0], pathCell, w.path)
			}
			if hops := hopsLabel(strings.Trim(w.path, "`")); !strings.Contains(pathCell, hops) {
				rep.Errorf("%s: the %s row's path %q should be labelled %q", ft.where(), r[0], pathCell, hops)
			}
		}
		expectCell(rep, ft, r[0], "Floor loss at 0.1", ft.cell(r, "Floor loss"), w.floor)
		expectCell(rep, ft, r[0], "Worst loss", ft.cell(r, "Worst loss"), w.worst)
	}
	if seen != len(want) {
		rep.Errorf("%s: has %d dated rows, want %d", ft.where(), seen, len(want))
	}
}

func checkSisterCorridors(rep figureReporter, set *figureSet, ft *figureTable) {
	rep.Helper()
	for _, corridor := range strings.Split(ft.attrs["chain"], ",") {
		rec, err := latestChainRecord(corridor)
		if err != nil {
			rep.Errorf("%s: %v", ft.where(), err)
			continue
		}
		send, receive, _ := strings.Cut(corridor, "-")
		r := rowByCorridor(rep, ft, send+" → "+receive)
		if r == nil {
			continue
		}
		expectCell(rep, ft, corridor, "Integrity", ft.cell(r, "Integrity"), "`"+rec.Integrity+"`")

		priced, unusable := 0, 0
		for _, rg := range rec.Rungs {
			if rg.Priced {
				priced++
			}
			if rg.Verdict == route.VerdictUnusable.String() {
				unusable++
			}
		}
		rung := ft.cell(r, "0.1 USDC rung")
		count := ft.cell(r, "Rungs graded")
		if priced == 0 {
			if !strings.Contains(rung, "not priced") {
				rep.Errorf("%s: %s priced nothing but the row reads %q", ft.where(), corridor, rung)
			}
			expectCell(rep, ft, corridor, "Rungs graded", count, "n/a")
			continue
		}
		if !strings.HasPrefix(rung, "priced, "+rec.FloorLossPct+"% loss") {
			rep.Errorf("%s: %s's row reads %q; the committed floor is %s%%", ft.where(), corridor, rung, rec.FloorLossPct)
		}
		if strings.Contains(rung, "every path via ") {
			via := strings.TrimSpace(rung[strings.Index(rung, "every path via ")+len("every path via "):])
			for _, rg := range rec.Rungs {
				if rg.Priced && !strings.Contains(rg.Path, " -> "+via+" -> ") {
					rep.Errorf("%s: %s's row says every path runs via %s; the %s rung's path is %q",
						ft.where(), corridor, via, rg.SendAmount, rg.Path)
				}
			}
		}
		expectCell(rep, ft, corridor, "Rungs graded", count, fmt.Sprintf("%d of %d", unusable, len(rec.Rungs)))
	}
}

// checkLadderRange: an observation over "every size tested" must name the
// ladder the code actually runs.
func checkLadderRange(rep figureReporter, _ *figureSet, ft *figureTable) {
	rep.Helper()
	sizes := dex.DefaultSizes
	want := sizes[0].String() + " – " + sizes[len(sizes)-1].String()
	expectCell(rep, ft, "sizes", "Send", ft.rows[0][0], want)
}

// ---------------------------------------------------------------------------
// Dispatch

// checkFigureTable runs the check a table's marker names. midShift, when
// non-nil, rescales every live and snapshot mid; the negative tests use it to
// stand in for a change in how the benchmark is applied.
func checkFigureTable(rep figureReporter, set *figureSet, ft *figureTable, midShift *decimal.Decimal) {
	rep.Helper()
	mid := func() (decimal.Decimal, bool) {
		m, ok := figureMid(rep, ft)
		if ok && midShift != nil {
			m = m.Mul(*midShift)
		}
		return m, ok
	}
	switch kind := ft.attrs["kind"]; kind {
	case "snapshot":
		if from := ft.attrs["mid-from"]; from != "" {
			rec, err := latestChainRecord(from)
			if err != nil {
				rep.Errorf("%s: %v", ft.where(), err)
			} else if rec.Reference.Mid != ft.attrs["mid"] {
				rep.Errorf("%s: mid=%s, but the latest %s record was scored at %s",
					ft.where(), ft.attrs["mid"], from, rec.Reference.Mid)
			}
		}
		if m, ok := mid(); ok {
			checkSnapshotTable(rep, ft, m)
		}
	case "live":
		if m, ok := mid(); ok {
			checkLiveTable(rep, ft, m)
		}
	case "derived", "chain":
		check := figureChecks[ft.attrs["check"]]
		if check == nil {
			rep.Errorf("%s: check=%q is not a known derivation", ft.where(), ft.attrs["check"])
			return
		}
		check(rep, set, ft)
	case "observation":
		if name := ft.attrs["check"]; name != "" {
			check := figureChecks[name]
			if check == nil {
				rep.Errorf("%s: check=%q is not a known derivation", ft.where(), name)
				return
			}
			check(rep, set, ft)
		}
	default:
		rep.Errorf("%s: kind=%q is not one of snapshot, live, derived, chain, observation", ft.where(), kind)
	}
}

// ---------------------------------------------------------------------------
// Prose

type proseFigure struct {
	doc   string
	token string // exactly as the prose prints it
	want  string // recomputed
	why   string
}

func proseFigures(set *figureSet) []proseFigure {
	cm, cs := figureDocs[0], figureDocs[1]
	tbl := set.tables
	loss := func(id, size string) string {
		t := tbl[id]
		return t.cell(t.rowWhere(size), "Loss vs mid")
	}
	threshold := func(d decimal.Decimal) string { return d.String() + "%" }

	var out []proseFigure
	for _, doc := range figureDocs {
		out = append(out,
			proseFigure{doc, threshold(route.ThresholdGood), threshold(route.ThresholdGood), "route.ThresholdGood"},
			proseFigure{doc, threshold(route.ThresholdFair), threshold(route.ThresholdFair), "route.ThresholdFair"},
			proseFigure{doc, threshold(route.ThresholdPoor), threshold(route.ThresholdPoor), "route.ThresholdPoor"},
		)
	}

	// corridor-measurements.md
	out = append(out,
		proseFigure{cm, "24.65%", loss("ngnc-20260808", "0.1"), "the 2026-08-08 floor"},
		proseFigure{cm, "97.68%", loss("ngnc-20260808", "5000"), "the 2026-08-08 worst rung"},
		proseFigure{cm, "25.02%", loss("ngnc-20260808-window", "0.1"), "the same-window floor"},
		proseFigure{cm, "74.14%", loss("ghsc-20260808", "0.1"), "the GHSC floor"},
	)
	if t := tbl["ngnt-20260827"]; t != nil {
		for _, r := range t.rows {
			if t.cell(r, "Loss vs mid") == "0.00%" {
				out = append(out, proseFigure{cm, "0.00%", "0.00%", "the NGNT rungs printed at zero loss"})
				break
			}
		}
	}
	{
		t := tbl["ngnc-20260808"]
		r1000, r5000 := receiveAt(t, "1000"), receiveAt(t, "5000")
		more := r5000.Div(r1000).Sub(decimal.NewFromInt(1)).Mul(decimal.NewFromInt(100))
		out = append(out, proseFigure{cm, "12.4%", more.StringFixed(1) + "%", "5000 vs 1000 USDC receive"})
	}
	{
		f, _ := figNum(loss("ngnc-20260821-replay", "0.1"))
		want := "not under 30%"
		if f.LessThan(decimal.NewFromInt(30)) && f.GreaterThanOrEqual(decimal.NewFromInt(25)) {
			want = "30%"
		}
		out = append(out, proseFigure{cm, "30%", want, "the replay floor is just under 30%"})
	}
	{
		g, _ := figNum(loss("ghsc-20260808", "0.1"))
		n, _ := figNum(loss("ngnc-20260808-window", "0.1"))
		one, hundred := decimal.NewFromInt(1), decimal.NewFromInt(100)
		leg := one.Sub(one.Sub(g.Div(hundred)).Div(one.Sub(n.Div(hundred)))).Mul(hundred)
		want := "not ~65%"
		if leg.Sub(decimal.NewFromInt(65)).Abs().LessThan(one) {
			want = "~65%"
		}
		out = append(out, proseFigure{cm, "~65%", want, "the NGNC → GHSC leg, " + leg.StringFixed(2) + "%"})
		out = append(out, proseFigure{cm, "25%", n.Round(0).String() + "%", "the window floor, rounded"})
	}
	if t := tbl["ngnc-comparison"]; t != nil && len(t.rows) > 0 {
		out = append(out, proseFigure{cm, "52.3%", t.rows[len(t.rows)-1][t.col("2026-08-04")],
			"the 2026-08-04 loss, replayed from the recorded body"})
	}
	if t := tbl["depth"]; t != nil {
		total, _ := figNum(t.cell(t.rowWhere("**Total**"), "Value"))
		out = append(out, proseFigure{cm, "$143", "$" + total.Round(0).String(), "the depth total, rounded"})
	}

	// ngnc-structural-floor.md
	rec, err := latestChainRecord("USDC-NGNC")
	chainFloor, chainWorst := "unavailable", "unavailable"
	if err == nil {
		chainFloor, chainWorst = rec.FloorLossPct+"%", rec.WorstLossPct+"%"
	}
	out = append(out,
		proseFigure{cs, "24.65%", loss("ngnc-20260808", "0.1"), "the 2026-08-08 floor"},
		proseFigure{cs, "28.18%", loss("ngnc-20260821-replay", "0.1"), "the replayed floor"},
		proseFigure{cs, "27.15%", chainFloor, "the committed record's floor"},
		proseFigure{cs, "97.52%", chainWorst, "the committed record's worst loss"},
	)
	return out
}

// proseFigureRE matches the figures the prose guard holds to a derivation:
// percentages and dollar amounts. Bare counts and sizes are left alone, since
// most of them are ladder sizes rather than findings.
var proseFigureRE = regexp.MustCompile(`~?\$\d[\d,]*(?:\.\d+)?|~?\d[\d,]*(?:\.\d+)?%`)

func proseLines(raw string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(raw, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") {
			inFence = !inFence
			continue
		}
		if inFence || strings.HasPrefix(t, "|") || strings.HasPrefix(t, "<!--") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// ---------------------------------------------------------------------------
// Tests

// TestCorridorFiguresAreAnnotated: every table of figures names its source,
// every marker is well formed, and every table both documents cite exists.
func TestCorridorFiguresAreAnnotated(t *testing.T) {
	set := loadFigureSet(t)
	for _, p := range set.problems {
		t.Error(p)
	}
	if len(set.ordered) == 0 {
		t.Fatal("no figures markers found; the reproduction gate would pass without checking anything")
	}
	for _, ft := range set.ordered {
		if len(ft.rows) == 0 {
			t.Errorf("%s: table has no rows", ft.where())
		}
		for _, f := range strings.Split(ft.attrs["from"], ",") {
			if f = strings.TrimSpace(f); f != "" && set.tables[f] == nil {
				t.Errorf("%s: from= names %q, which no marker defines", ft.where(), f)
			}
		}
	}
	// Both documents must carry figures; an empty one has lost its markers.
	for _, doc := range figureDocs {
		n := 0
		for _, ft := range set.ordered {
			if ft.doc == doc {
				n++
			}
		}
		if n == 0 {
			t.Errorf("%s carries no figures markers", doc)
		}
	}
}

// TestCorridorFiguresReproduce is the gate: every annotated table recomputed
// from its source.
func TestCorridorFiguresReproduce(t *testing.T) {
	set := loadFigureSet(t)
	for _, ft := range set.ordered {
		t.Run(ft.id(), func(t *testing.T) {
			checkFigureTable(t, set, ft, nil)
		})
	}
}

// TestCorridorProseFiguresDerive checks each figure the prose cites against
// its recomputation, and fails on a percentage or dollar figure in the prose
// that nothing derives.
func TestCorridorProseFiguresDerive(t *testing.T) {
	set := loadFigureSet(t)
	known := map[string]map[string]bool{}
	for _, pf := range proseFigures(set) {
		if known[pf.doc] == nil {
			known[pf.doc] = map[string]bool{}
		}
		known[pf.doc][pf.token] = true
		if pf.token != pf.want {
			t.Errorf("%s cites %s (%s); recomputed it is %s", pf.doc, pf.token, pf.why, pf.want)
		}
		if !strings.Contains(strings.Join(proseLines(set.docs[pf.doc]), "\n"), pf.token) {
			t.Errorf("%s no longer cites %s (%s); remove the derivation or restore the figure",
				pf.doc, pf.token, pf.why)
		}
	}

	for _, doc := range figureDocs {
		var stray []string
		for _, line := range proseLines(set.docs[doc]) {
			for _, tok := range proseFigureRE.FindAllString(line, -1) {
				if !known[doc][tok] && !known[doc][strings.TrimPrefix(tok, "~")] {
					stray = append(stray, tok)
				}
			}
		}
		sort.Strings(stray)
		for _, tok := range stray {
			t.Errorf("%s cites %s in prose with no derivation in corridor_figures_test.go; "+
				"add one to proseFigures, or move the figure into an annotated table (#702)", doc, tok)
		}
	}
}

// TestCorridorFiguresFailOnAPricingChange is the acceptance criterion: a change
// that moves a published figure must fail the gate. Each case perturbs one
// thing and requires the affected table's check to report it.
func TestCorridorFiguresFailOnAPricingChange(t *testing.T) {
	set := loadFigureSet(t)
	must := func(id string) *figureTable {
		ft := set.tables[id]
		if ft == nil {
			t.Fatalf("no table %s", id)
		}
		return ft
	}
	expectFailure := func(t *testing.T, ft *figureTable, shift *decimal.Decimal) {
		t.Helper()
		rec := &figureRecorder{}
		checkFigureTable(rec, set, ft, shift)
		if len(rec.errs) == 0 {
			t.Errorf("%s: the check passed after a change that moves its figures; "+
				"the gate is not load-bearing", ft.where())
		}
	}

	t.Run("benchmark applied differently", func(t *testing.T) {
		// A 0.1% change in the mid the loss is scored against moves every
		// published loss by several hundredths of a point.
		shift := decimal.RequireFromString("1.001")
		expectFailure(t, must("ngnc-20260821-replay"), &shift)
		expectFailure(t, must("ngnc-20260808"), &shift)
		expectFailure(t, must("ngnt-20260827"), &shift)
	})

	t.Run("verdict band moved", func(t *testing.T) {
		// Raising the Poor ceiling to 30% regrades the 24-30% rungs. route's
		// thresholds are package variables; no test in this package runs in
		// parallel, so the change cannot leak while it is in effect.
		saved := route.ThresholdPoor
		route.ThresholdPoor = decimal.NewFromInt(30)
		defer func() { route.ThresholdPoor = saved }()

		expectFailure(t, must("ngnc-20260821-replay"), nil)
		expectFailure(t, must("ngnc-20260808"), nil)
		expectFailure(t, must("ngnc-20260808-window"), nil)
	})

	t.Run("a cell drifts from its source", func(t *testing.T) {
		for _, id := range []string{"ngnc-20260821-replay", "ngnc-marginal", "depth", "case-study-floors"} {
			orig := must(id)
			mutated := *orig
			mutated.rows = make([][]string, len(orig.rows))
			for i, r := range orig.rows {
				mutated.rows[i] = append([]string(nil), r...)
			}
			// Nudge the last digit of the first numeric cell after the key.
			r := mutated.rows[0]
			for c := 1; c < len(r); c++ {
				if loc := regexp.MustCompile(`\d(?:[^\d]*)$`).FindStringIndex(r[c]); loc != nil && strings.ContainsAny(r[c], "0123456789") {
					b := []byte(r[c])
					for k := len(b) - 1; k >= 0; k-- {
						if b[k] >= '0' && b[k] <= '9' {
							b[k] = '0' + (b[k]-'0'+1)%10
							break
						}
					}
					r[c] = string(b)
					break
				}
			}
			rec := &figureRecorder{}
			checkFigureTable(rec, set, &mutated, nil)
			if len(rec.errs) == 0 {
				t.Errorf("%s: a drifted first row %v passed the check", orig.where(), r)
			}
		}
	})
}
