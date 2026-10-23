// Command ladder prices a corridor at a range of sizes and reports the
// effective rate, loss against the reference mid, and verdict at each.
//
// It exists to answer one question: is the loss on this corridor
// size-dependent, or is it structural? The answer decides what the product is.
//
//	go run ./cmd/ladder                  # USDC -> NGNC
//	go run ./cmd/ladder -to GHSC         # USDC -> GHSC, benchmarked against GHS
//	go run ./cmd/ladder -to NGNT         # USDC -> NGNT, benchmarked against NGN
//	go run ./cmd/ladder -to GHSC -json   # same, as JSON on stdout
//	go run ./cmd/ladder -checks=false    # same, without counterparty checks
//
//	go run ./cmd/ladder -from NGNC -to USDC   # reverse corridor: the exit leg
//
//	go run ./cmd/ladder -record testdata/snapshots   # also keep the bytes
//
// Recording is opt-in and writes the verbatim upstream responses to a new
// directory under the given parent, named by the convention in
// docs/snapshot-format.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// corridor names a destination token and the fiat currency it claims to track.
// The reference pair is the token's peg, not the token itself — nobody
// publishes a mid-market rate for "NGNC".
type corridor struct {
	dest    asset.Asset
	refPair string // ISO-4217 code of the fiat the token is pegged to
}

// defaultCorridors builds the corridor map from the asset registry.
func defaultCorridors() map[string]corridor {
	m := make(map[string]corridor)
	for _, e := range asset.Registry() {
		if e.Code == "USDC" || e.Peg == "" {
			continue
		}
		a, ok := asset.Lookup(e.Code)
		if !ok {
			a = asset.Stellar(e.Code, e.Issuer)
		}
		m[strings.ToUpper(e.Code)] = corridor{
			dest:    a,
			refPair: e.Peg,
		}
	}
	return m
}

var corridors = defaultCorridors()

func main() {
	var (
		from      = flag.String("from", "USDC", fromHelp())
		to        = flag.String("to", "NGNC", toHelp())
		sizesFlag = flag.String("sizes", "",
			"comma-separated send amounts in the send asset (default: the shared USD ladder, "+
				"or its NGN-scaled reverse ladder when -from names a fiat token)")
		jsonOut = flag.Bool("json", false, "emit JSON on stdout instead of the text table")
		record  = flag.String("record", "",
			"record upstream responses as a snapshot in a new directory under this parent")
		refName    = flag.String("ref", defaultRefProvider, refHelp())
		allowDirty = flag.Bool("allow-dirty", false,
			"record even though the working tree is modified, marking the manifest dirty")
		checksFlag = flag.Bool("checks", true,
			"run counterparty checks (anchor toml, SEP-10/SEP-24, issuer flags) and "+
				"attach the findings; set false to skip them (the JSON document then "+
				"carries no findings block)")
	)
	flag.Parse()

	run, err := resolveDirection(*from, *to)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	sizesRaw := *sizesFlag
	if strings.TrimSpace(sizesRaw) == "" {
		sizesRaw = run.defaultSizesFlag()
	}
	sizes, err := parseSizes(sizesRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	// One HTTP client feeds both upstreams, so recording swaps a single
	// transport rather than threading a flag through every caller.
	var (
		httpClient *http.Client
		recorder   *snapshot.Recorder
	)
	ref, err := referenceProvider(*refName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	refProvider := ref.provider
	dexClient := &dex.Client{}

	if *record != "" {
		dirty, err := requireCleanTree(".", *allowDirty)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		recorder = &snapshot.Recorder{
			Dirty: dirty,
			Corridor: snapshot.Corridor{
				Send:          snapshot.AssetRef{Code: run.send.Code, Issuer: run.send.Issuer},
				Receive:       snapshot.AssetRef{Code: run.recv.Code, Issuer: run.recv.Issuer},
				ReferencePair: run.pair(),
			},
			Sizes: sizeStrings(sizes),
			Sources: snapshot.Sources{
				Horizon:   snapshot.SourceRef{BaseURL: dex.DefaultHorizonURL},
				Reference: snapshot.SourceRef{Provider: refProvider.Name(), BaseURL: ref.baseURL},
			},
			GitRevision: gitRevision(),
		}
		httpClient = &http.Client{Transport: recorder, Timeout: 30 * time.Second}
		dexClient.HTTPClient = httpClient
		ref.setClient(httpClient)
	}

	eng := &route.Engine{
		DEX:     dexClient,
		RefRate: &refrate.Checked{Inner: refProvider},
	}

	// The same counterparty checks the server runs, so -json and
	// /api/corridor describe the same corridor with the same facts. Nil
	// disables them and the document then carries no findings block at all
	// — absent and empty must not look the same, which mirrors the server's
	// own Checks field. -checks=false is how an operator opts out of the
	// extra latency; the difference is then the flag they chose, not the
	// binary they ran.
	var checkRunner *checks.Runner
	if *checksFlag {
		checkRunner = &checks.Runner{HorizonURL: dex.DefaultHorizonURL}
		if httpClient != nil {
			// One transport covers the sweep, so a recording captures the
			// checks' traffic alongside the ladder's (see the Runner's own
			// HTTPClient doc for why this is the supported shape).
			checkRunner.HTTPClient = httpClient
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := eng.Ladder(ctx, route.LadderRequest{
		SendAsset:      run.send,
		ReceiveAsset:   run.recv,
		Sizes:          sizes,
		ReferenceBase:  run.refBase,
		ReferenceQuote: run.refQuote,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "measuring corridor: %v\n", err)
		os.Exit(1)
	}

	// Checks run after the measurement and cannot alter it: route.WithFindings
	// is the only composition point and branches on nothing, exactly as in
	// the server. With checks skipped this stays nil and the document says
	// "not checked", never "checked, nothing found".
	var findings *checks.Findings
	if checkRunner != nil {
		// The checks describe the corridor's fiat-token counterparty, which
		// is the destination of a forward corridor and the source of a
		// reverse one.
		findings = checkRunner.ForAsset(ctx, run.fiatToken())
	}

	if *jsonOut {
		printJSON(result, run.pair(), findings)
	} else {
		printTable(ctx, result, refProvider, findings)
	}

	// Saved after the run so a snapshot only ever exists for a measurement
	// that actually completed.
	//
	// A rung that errored means an upstream call did not return, so the
	// recording has a hole in it. Replaying that would render a corridor as
	// partially unpriced when the market was fine and the network was not,
	// which is a fabricated finding — refuse it and let the operator re-run.
	if recorder != nil {
		if failed := erroredRungs(result); len(failed) > 0 {
			fmt.Fprintf(os.Stderr,
				"\nnot recording a snapshot: %d of %d sizes failed to reach an upstream (%s).\n"+
					"A snapshot with a gap would replay as a corridor that is partly unpriced. Re-run.\n",
				len(failed), len(result.Rungs), strings.Join(failed, ", "))
			os.Exit(1)
		}
		dir := filepath.Join(*record, snapshot.DirName(recorder.Corridor, recorder.RecordedAt()))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "creating snapshot directory: %v\n", err)
			os.Exit(1)
		}
		if err := recorder.Save(dir); err != nil {
			fmt.Fprintf(os.Stderr, "saving snapshot: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "\nrecorded %d upstream responses to %s\n", recorder.Count(), dir)
	}

	// A ladder with no recommended size at any point is the normal shape of
	// a broken corridor, not an error — but it is a fact scripts need to be
	// able to detect without parsing prose, so the exit code carries it.
	if !result.Viable() {
		os.Exit(1)
	}
}

// corridorCodes returns the destination asset codes of all registered corridors
// in registry order.
//
// USDC is excluded because it is the settlement asset every corridor starts
// from, not a destination.
func corridorCodes() []string {
	var codes []string
	for _, e := range asset.Registry() {
		if e.Code == "USDC" || e.Peg == "" {
			continue
		}
		codes = append(codes, e.Code)
	}
	return codes
}

// toHelp returns the help text for the -to flag, listing all registered corridor destinations.
//
// Derived from the asset registry at startup so that adding a corridor in package
// asset automatically updates the CLI flag help without maintaining a second list
// (issue #586).
func toHelp() string {
	return fmt.Sprintf("destination asset code (%s)", strings.Join(corridorCodes(), ", "))
}

// defaultSizesFlag renders the shared ladder as the comma-separated form the
// -sizes flag accepts.
//
// The default is derived from route.DefaultSizes rather than written out as a
// second literal. The CLI, the server and the depth metric must measure the
// same ladder; a literal here would silently drift when the shared definition
// changed, which is the class of divergence #390 removed everywhere else.
func defaultSizesFlag() string {
	parts := make([]string, len(route.DefaultSizes))
	for i, size := range route.DefaultSizes {
		parts[i] = size.String()
	}
	return strings.Join(parts, ",")
}

// parseSizes reads the -sizes flag. A malformed entry is reported and
// skipped rather than aborting the whole ladder, matching the tool's
// long-standing behaviour of measuring what it can.
func parseSizes(raw string) ([]decimal.Decimal, error) {
	var out []decimal.Decimal
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		amt, err := decimal.NewFromString(s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bad size %q: %v\n", s, err)
			continue
		}
		out = append(out, amt)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid sizes in %q", raw)
	}
	return out, nil
}

// erroredRungs names the sizes whose upstream call failed.
//
// This is distinct from a size that returned no path: NO-MARKET is a finding,
// while a transport error is an absence of information, and only the second
// makes a recording unfit to publish.
func erroredRungs(l *route.LadderResult) []string {
	var out []string
	for _, r := range l.Rungs {
		if r.Err != nil {
			out = append(out, r.SendAmount.String())
		}
	}
	return out
}

const defaultRefProvider = "exchangerate-api"

// refSource bundles a reference provider with the two things a caller needs
// that the Provider interface deliberately does not expose: where it fetches
// from, for a snapshot manifest, and how to give it a recording HTTP client.
type refSource struct {
	provider  refrate.Provider
	baseURL   string
	setClient func(*http.Client)
}

func newExchangeRateAPI() refSource {
	p := &refrate.ExchangeRateAPI{}
	return refSource{
		provider:  p,
		baseURL:   refrate.DefaultExchangeRateAPI,
		setClient: func(c *http.Client) { p.Client = c },
	}
}

func newCurrencyAPI() refSource {
	p := &refrate.CurrencyAPI{}
	return refSource{
		provider:  p,
		baseURL:   refrate.DefaultCurrencyAPI,
		setClient: func(c *http.Client) { p.Client = c },
	}
}

// refConstructors registers the constructor for each supported reference provider.
// Valid provider names in the flag help and unknown-provider errors are generated
// directly from this list via Provider.Name().
var refConstructors = []func() refSource{
	newExchangeRateAPI,
	newCurrencyAPI,
}

// refProviderNames returns the valid provider names derived from the provider constructors.
func refProviderNames() []string {
	names := make([]string, 0, len(refConstructors))
	for _, c := range refConstructors {
		names = append(names, c().provider.Name())
	}
	return names
}

// refHelp returns the help text for the -ref flag, listing all available providers.
func refHelp() string {
	return fmt.Sprintf("reference rate provider: %s", strings.Join(refProviderNames(), ", "))
}

// referenceProvider resolves the -ref flag.
//
// An unknown name is an error rather than a fallback to the default. Silently
// substituting a different benchmark than the one asked for would mislabel
// every figure the run produces.
func referenceProvider(name string) (refSource, error) {
	clean := strings.ToLower(strings.TrimSpace(name))
	if clean == "" {
		clean = defaultRefProvider
	}
	for _, constructor := range refConstructors {
		src := constructor()
		if strings.EqualFold(src.provider.Name(), clean) {
			return src, nil
		}
	}
	return refSource{}, fmt.Errorf(
		"unknown reference provider %q; known: %s", name, strings.Join(refProviderNames(), ", "))
}

// sizeStrings renders ladder sizes for a snapshot manifest. They are decimal
// strings there for the same reason they are everywhere else: a JSON number
// invites a reader to parse them back through a float64.
func sizeStrings(sizes []decimal.Decimal) []string {
	out := make([]string, len(sizes))
	for i, s := range sizes {
		out[i] = s.String()
	}
	return out
}

// gitRevision records which build captured a snapshot, so a fixture that
// disagrees with the current parser can be traced to the code that wrote it.
// An unavailable revision is recorded as absent rather than guessed.
func gitRevision() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// dirtyFiles lists files whose working-tree state differs from HEAD:
// modified tracked files and untracked files alike.
//
// An untracked file is included because it is part of the working tree the
// recorded bytes were produced from even though HEAD does not contain it, so
// git_revision would not pin the code that generated them. Porcelain lines
// are returned verbatim, status code and all (` M f.go`, `?? scratch.txt`).
//
// An empty result with a nil error means a clean tree; a nil result with an
// error means git could not answer, which is not the same thing and must not
// be reported as clean.
func dirtyFiles(dir string) ([]string, error) {
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		return nil, fmt.Errorf("could not determine whether the tree is clean: %w", err)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// requireCleanTree refuses to record a snapshot from a modified working tree.
//
// git_revision is the only link between a committed fixture and the code that
// produced it. Recorded from a dirty tree it names a revision that did not
// generate the bytes — which is worse than recording nothing, because the
// field looks like provenance and is consulted as though it were.
//
// Dirty means anything `git status` reports: a modified tracked file or an
// untracked file. Either way HEAD does not describe the working tree the
// bytes came from.
//
// This repository already contains the failure: the GHSC and KESC snapshots
// recorded revision e2be414, a commit predating the snapshot package that
// wrote them. Both were re-captured once this check existed.
//
// -allow-dirty exists for local experiments and marks the manifest, so a
// snapshot whose provenance is approximate says so in the artifact rather
// than only in the shell that made it.
func requireCleanTree(dir string, allowDirty bool) (dirty bool, err error) {
	files, err := dirtyFiles(dir)
	if err != nil {
		// Not in a git checkout, or git is unavailable. Recording is still
		// possible; the revision is simply absent, which the manifest
		// represents honestly.
		return false, nil
	}
	if len(files) == 0 {
		return false, nil
	}
	if allowDirty {
		fmt.Fprintf(os.Stderr,
			"warning: recording from a modified tree; manifest will be marked dirty:\n  %s\n",
			strings.Join(files, "\n  "))
		return true, nil
	}
	return false, fmt.Errorf(
		"refusing to record a snapshot from a modified working tree.\n"+
			"git_revision would name a tree that did not produce these bytes, and a\n"+
			"fixture with approximate provenance is a fixture whose provenance is\n"+
			"decorative. Commit or stash first, or pass -allow-dirty to record anyway\n"+
			"and mark the manifest.\n\nModified:\n  %s",
		strings.Join(files, "\n  "))
}

// printJSON writes the shared wire shape and nothing else to stdout, so
// `go run ./cmd/ladder -to GHSC -json | jq` works.
func printJSON(result *route.LadderResult, pair string, findings *checks.Findings) {
	if err := encodeCorridorJSON(os.Stdout, result, pair, findings); err != nil {
		// Encoding a well-formed struct to stdout should not fail; if it
		// does, say so on stderr rather than emitting partial JSON.
		fmt.Fprintf(os.Stderr, "encoding result: %v\n", err)
		os.Exit(1)
	}
}

// encodeCorridorJSON is the entire body of -json mode: it renders the shared
// wire shape and encodes exactly what the shared composition point returns,
// nothing more.
//
// Split out from printJSON so a test can capture the bytes without a pipe on
// os.Stdout. That is also the point of the split: this function has no room
// left in it to grow a second, independently-maintained JSON shape, which is
// the drift TestLadderJSONMatchesToCorridorJSON exists to catch — see
// docs/backlog.md #5 / GitHub issue #113. Findings are attached the same way
// the server attaches them, through route.WithFindings — the one composition
// point — so a corridor measured by either binary carries the same facts.
func encodeCorridorJSON(w io.Writer, result *route.LadderResult, pair string, findings *checks.Findings) error {
	out := route.ToCorridorJSON(result, pair)
	out = route.WithFindings(out, findings)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// printTable renders the human-readable text table. This is the default
// output and its format is unchanged from before -json existed, apart from
// one line: when checks ran, a summary of their findings is printed after
// the reference mid, so the default-on checks are visible in the output they
// paid for the latency of. The reference provider is passed in rather than
// constructed here: a second construction is a second HTTP client, which
// would bypass a recorder and leave the snapshot missing the very rate the
// table prints.
func printTable(ctx context.Context, result *route.LadderResult, ref refrate.Provider, findings *checks.Findings) {
	sendCode := result.Request.SendAsset.Code
	recvCode := result.Request.ReceiveAsset.Code
	pair := result.Request.ReferenceBase + "/" + result.Request.ReferenceQuote
	fmt.Printf("corridor %s -> %s, benchmarked against %s\n", sendCode, recvCode, pair)
	fmt.Printf("run at %s\n\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Printf("%-8s %14s %12s %9s %-10s %-11s %s\n",
		"SEND", "RECEIVE", "RATE", "LOSS%", "VERDICT", "INTEGRITY", "PATH")

	priced := 0
	for _, r := range result.Rungs {
		s := r.SendAmount.String()

		if r.Err != nil {
			fmt.Printf("%-8s  ERROR: %v\n", s, r.Err)
			continue
		}
		if r.Result == nil || len(r.Result.Quotes) == 0 {
			integrity, notes := "-", ""
			if r.Result != nil {
				integrity = r.Result.Integrity.String()
				notes = strings.Join(r.Result.Notes, "; ")
			}
			fmt.Printf("%-8s %14s %12s %9s %-10s %-11s %s\n",
				s, "-", "-", "-", "-", integrity, notes)
			continue
		}

		priced++
		q := r.Result.Quotes[0]
		fmt.Printf("%-8s %14s %12s %9s %-10s %-11s %s\n",
			s,
			q.ReceiveAmount.StringFixed(2),
			q.EffectiveRate.StringFixed(2),
			q.LossPct.StringFixed(2),
			q.Verdict.String(),
			r.Result.Integrity.String(),
			q.Description,
		)
		for _, w := range q.Warnings {
			fmt.Printf("%-8s   warn: %s\n", "", w)
		}
		if r.Result.Recommended == nil {
			fmt.Printf("%-8s   (engine recommends nothing at this size)\n", "")
		}
	}

	if r, err := ref.Rate(ctx, result.Request.ReferenceBase, result.Request.ReferenceQuote); err == nil {
		fmt.Printf("\nreference mid: %s %s via %s, as of %s\n",
			r.Mid.StringFixed(4), pair, r.Source, r.AsOf.UTC().Format(time.RFC3339))
	}
	if findings != nil {
		passed, failed, undetermined := findings.Counts()
		fmt.Printf("checks: %d passed, %d failed, %d undetermined",
			passed, failed, undetermined)
		if worst, any := findings.Worst(); any {
			fmt.Printf(" (worst: %s)", worst)
		}
		fmt.Println(" — pass -checks=false to skip")

		if len(findings.Metrics) > 0 {
			measured := 0
			for _, m := range findings.Metrics {
				if m.Determined {
					measured++
				}
			}
			fmt.Printf("metrics: %d measured, %d undetermined\n", measured, len(findings.Metrics)-measured)
			for _, m := range findings.Metrics {
				if m.Determined {
					fmt.Printf("  %-25s %s %s\n", m.ID, m.Value.String(), m.Unit)
				} else {
					reason := m.Reason
					if reason == "" {
						reason = m.Summary
					}
					fmt.Printf("  %-25s undetermined (%s)\n", m.ID, reason)
				}
			}
		}
	}
	if priced == 0 {
		fmt.Printf("no size could be priced for %s -> %s\n", sendCode, recvCode)
	}
}

// direction is one corridor resolved to the assets it actually measures:
// which asset is sent, which is received, and which reference pair benchmarks
// it. A forward corridor runs USDC -> token against USD/peg; a reverse
// corridor runs token -> USDC against peg/USD, with its own integrity state
// (issue #596).
type direction struct {
	send     asset.Asset
	recv     asset.Asset
	refBase  string
	refQuote string
	reverse  bool
}

// pair renders the reference pair for display and snapshot manifests.
func (d direction) pair() string { return d.refBase + "/" + d.refQuote }

// fiatToken is the corridor's token side — the counterparty the checks
// describe — regardless of which way the corridor is measured.
func (d direction) fiatToken() asset.Asset {
	if d.send.Code == "USDC" {
		return d.recv
	}
	return d.send
}

// defaultSizesFlag renders the default ladder for this direction.
func (d direction) defaultSizesFlag() string {
	sizes := route.DefaultSizes
	if d.reverse {
		sizes = reverseSizes()
	}
	parts := make([]string, 0, len(sizes))
	for _, s := range sizes {
		parts = append(parts, s.String())
	}
	return strings.Join(parts, ",")
}

// resolveDirection maps -from/-to onto the corridor to measure.
//
// With -from USDC (the default) this is the historical forward corridor, and
// -to selects a registered destination token. With -from naming a fiat token,
// this is a reverse corridor — the exit leg — and -to must be USDC: measuring
// "NGNC -> GHSC" is a different corridor nobody asked for, and inventing one
// by taking the argument literally would publish a number under the wrong
// name.
func resolveDirection(from, to string) (direction, error) {
	fromClean := strings.ToUpper(strings.TrimSpace(from))
	toClean := strings.ToUpper(strings.TrimSpace(to))

	if fromClean == "" || fromClean == "USDC" {
		c, ok := corridors[toClean]
		if !ok {
			return direction{}, fmt.Errorf("unknown destination %q; known: %s",
				to, strings.Join(corridorCodes(), ", "))
		}
		return direction{
			send:     asset.USDC(),
			recv:     c.dest,
			refBase:  "USD",
			refQuote: c.refPair,
		}, nil
	}

	send, ok := asset.Lookup(fromClean)
	if !ok {
		return direction{}, fmt.Errorf("unknown -from %q; known: USDC, %s",
			from, strings.Join(asset.KnownCodes(), ", "))
	}
	peg, isFiat := asset.FiatPeg(send)
	if !isFiat {
		return direction{}, fmt.Errorf(
			"-from %q is not a registered fiat-pegged token; a reverse corridor starts "+
				"from a token with a published peg", from)
	}
	if toClean != "USDC" {
		return direction{}, fmt.Errorf(
			"a reverse corridor measures %s -> USDC; got -from %s -to %s",
			send.Code, send.Code, to)
	}
	return direction{
		send:     send,
		recv:     asset.USDC(),
		refBase:  peg,
		refQuote: "USD",
		reverse:  true,
	}, nil
}

// reverseScale is the NGN-per-USD factor the reverse ladder is scaled by.
//
// No FX mid is consulted: the constant only chooses rung sizes, and the
// measurement's own benchmark comes from the reference provider. 1500 is the
// rough late-2026 mid (the recorded forward ladder measured 1516.39 NGN/USD on
// 2026-10-22), so the reverse ladder covers the same notional range as the
// forward one while staying a fixed input rather than furniture that moves
// between runs.
var reverseScale = decimal.NewFromInt(1500)

// reverseSizes scales the shared USD ladder into the send asset of a reverse
// corridor. Scaling rather than inventing rungs preserves the shared ladder's
// design: the smallest rung still isolates the structural floor, the largest
// still exposes exhaustion.
func reverseSizes() []decimal.Decimal {
	out := make([]decimal.Decimal, 0, len(route.DefaultSizes))
	for _, s := range route.DefaultSizes {
		out = append(out, s.Mul(reverseScale).Truncate(0))
	}
	return out
}

// fromHelp describes -from for the flag usage text.
func fromHelp() string {
	return "settlement asset the sender starts from (USDC, the default), or a registered " +
		"fiat token to measure back out of (e.g. NGNC); a reverse corridor always measures <from> -> USDC"
}
