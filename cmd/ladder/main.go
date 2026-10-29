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
	"flag"
	"fmt"
	"net/http"
	"os"
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
