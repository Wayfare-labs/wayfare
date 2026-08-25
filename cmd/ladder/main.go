// Command ladder prices a corridor at a range of sizes and reports the
// effective rate, loss against the reference mid, and verdict at each.
//
// It exists to answer one question: is the loss on this corridor
// size-dependent, or is it structural? The answer decides what the product is.
//
//	go run ./cmd/ladder                  # USDC -> NGNC
//	go run ./cmd/ladder -to GHSC         # USDC -> GHSC, benchmarked against GHS
//	go run ./cmd/ladder -to GHSC -json   # same, as JSON on stdout
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

var corridors = map[string]corridor{
	"NGNC": {asset.NGNC(), "NGN"},
	"GHSC": {asset.GHSC(), "GHS"},
	"KESC": {asset.KESC(), "KES"},
}

func main() {
	var (
		to        = flag.String("to", "NGNC", "destination asset code (NGNC, GHSC, KESC)")
		sizesFlag = flag.String("sizes", "0.1,1,5,10,25,50,100,250,500,1000,2500,5000",
			"comma-separated send amounts in USDC")
		jsonOut = flag.Bool("json", false, "emit JSON on stdout instead of the text table")
		record  = flag.String("record", "",
			"record upstream responses as a snapshot in a new directory under this parent")
		refName = flag.String("ref", "exchangerate-api",
			"reference rate provider: exchangerate-api or currency-api")
		allowDirty = flag.Bool("allow-dirty", false,
			"record even though the working tree is modified, marking the manifest dirty")
	)
	flag.Parse()

	c, ok := corridors[strings.ToUpper(*to)]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown destination %q; known: NGNC, GHSC, KESC\n", *to)
		os.Exit(2)
	}

	sizes, err := parseSizes(*sizesFlag)
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
		dirty, err := requireCleanTree(*allowDirty)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		recorder = &snapshot.Recorder{
			Dirty: dirty,
			Corridor: snapshot.Corridor{
				Send:          snapshot.AssetRef{Code: asset.USDC().Code, Issuer: asset.USDC().Issuer},
				Receive:       snapshot.AssetRef{Code: c.dest.Code, Issuer: c.dest.Issuer},
				ReferencePair: "USD/" + c.refPair,
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := eng.Ladder(ctx, route.LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   c.dest,
		Sizes:          sizes,
		ReferenceBase:  "USD",
		ReferenceQuote: c.refPair,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "measuring corridor: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut {
		printJSON(result, "USD/"+c.refPair)
	} else {
		printTable(ctx, result, c, refProvider)
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

// refSource bundles a reference provider with the two things a caller needs
// that the Provider interface deliberately does not expose: where it fetches
// from, for a snapshot manifest, and how to give it a recording HTTP client.
type refSource struct {
	provider  refrate.Provider
	baseURL   string
	setClient func(*http.Client)
}

// referenceProvider resolves the -ref flag.
//
// An unknown name is an error rather than a fallback to the default. Silently
// substituting a different benchmark than the one asked for would mislabel
// every figure the run produces.
func referenceProvider(name string) (refSource, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "exchangerate-api", "":
		p := &refrate.ExchangeRateAPI{}
		return refSource{p, refrate.DefaultExchangeRateAPI, func(c *http.Client) { p.Client = c }}, nil
	case "currency-api":
		p := &refrate.CurrencyAPI{}
		return refSource{p, refrate.DefaultCurrencyAPI, func(c *http.Client) { p.Client = c }}, nil
	}
	return refSource{}, fmt.Errorf(
		"unknown reference provider %q; known: exchangerate-api, currency-api", name)
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

// dirtyFiles lists tracked files modified relative to HEAD.
//
// An empty result with a nil error means a clean tree; a nil result with an
// error means git could not answer, which is not the same thing and must not
// be reported as clean.
func dirtyFiles() ([]string, error) {
	out, err := exec.Command("git", "status", "--porcelain", "--untracked-files=no").Output()
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
// This repository already contains the failure: the GHSC and KESC snapshots
// recorded revision e2be414, a commit predating the snapshot package that
// wrote them. Both were re-captured once this check existed.
//
// -allow-dirty exists for local experiments and marks the manifest, so a
// snapshot whose provenance is approximate says so in the artifact rather
// than only in the shell that made it.
func requireCleanTree(allowDirty bool) (dirty bool, err error) {
	files, err := dirtyFiles()
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
func printJSON(result *route.LadderResult, pair string) {
	if err := encodeCorridorJSON(os.Stdout, result, pair); err != nil {
		// Encoding a well-formed struct to stdout should not fail; if it
		// does, say so on stderr rather than emitting partial JSON.
		fmt.Fprintf(os.Stderr, "encoding result: %v\n", err)
		os.Exit(1)
	}
}

// encodeCorridorJSON is the entire body of -json mode: it delegates to
// route.ToCorridorJSON and encodes exactly what that returns, nothing more.
//
// Split out from printJSON so a test can capture the bytes without a pipe on
// os.Stdout. That is also the point of the split: this function has no room
// left in it to grow a second, independently-maintained JSON shape, which is
// the drift TestLadderJSONMatchesToCorridorJSON exists to catch — see
// docs/backlog.md #5 / GitHub issue #113.
func encodeCorridorJSON(w io.Writer, result *route.LadderResult, pair string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(route.ToCorridorJSON(result, pair))
}

// printTable renders the human-readable text table. This is the default
// output and its format is unchanged from before -json existed.
// The reference provider is passed in rather than constructed here: a second
// construction is a second HTTP client, which would bypass a recorder and
// leave the snapshot missing the very rate the table prints.
func printTable(ctx context.Context, result *route.LadderResult, c corridor, ref refrate.Provider) {
	fmt.Printf("corridor USDC -> %s, benchmarked against USD/%s\n", c.dest.Code, c.refPair)
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

	if r, err := ref.Rate(ctx, "USD", c.refPair); err == nil {
		fmt.Printf("\nreference mid: %s USD/%s via %s, as of %s\n",
			r.Mid.StringFixed(4), c.refPair, r.Source, r.AsOf.UTC().Format(time.RFC3339))
	}
	if priced == 0 {
		fmt.Printf("no size could be priced for USDC -> %s\n", c.dest.Code)
	}
}
