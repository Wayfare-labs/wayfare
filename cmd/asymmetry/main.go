// Command asymmetry cross-checks Horizon's two pathfinding directions on one
// corridor at matched notional (#595).
//
// Strict-send fixes what the sender parts with; strict-receive fixes what the
// recipient is promised. On a thin book the two directions can price the same
// corridor differently, and a strict-send-only ladder then describes only
// half of it. This tool measures both and reports the gap:
//
//	go run ./cmd/asymmetry -to NGNC -record testdata/snapshots/asymmetry
//	go run ./cmd/asymmetry -to NGNT -json
//
// For each ladder size S (in USDC):
//
//  1. /paths/strict-send prices S and reports the best delivered amount D.
//  2. /paths/strict-receive is asked what it costs to deliver exactly D, and
//     reports the cheapest source amount R.
//  3. The asymmetry is (R − S) / S as a percentage. Zero means the two
//     directions agree at that size; positive means delivering what S bought
//     would cost more than S — the corridor prices higher when the recipient
//     amount is the fixed side.
//
// Recording is opt-in and writes the verbatim responses of both directions
// into one snapshot directory, so every figure in the write-up can be reparsed
// offline (docs/snapshot-record-replay.md). A size that failed to reach an
// upstream makes the run unfit to record: a snapshot with a hole would replay
// as a corridor partly unpriced when the market was fine and the network was
// not.
//
// This is research tooling, not a pricing input: nothing in route or server
// calls it, and the write-up (docs/strict-receive-asymmetry-research.md)
// lands before any wire change is proposed.
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
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// row is one ladder size measured in both directions.
type row struct {
	Size      decimal.Decimal `json:"send_usdc"`
	Delivered decimal.Decimal `json:"delivered"`
	Required  decimal.Decimal `json:"required_usdc"`

	// AsymPct is (Required − Size) / Size as a percentage. It is zero for
	// unpriced rows, where State carries the reason instead.
	AsymPct decimal.Decimal `json:"asymmetry_pct"`

	SendPath string `json:"send_path,omitempty"`
	RecvPath string `json:"receive_path,omitempty"`
	State    string `json:"state"` // "priced", "no-market", or "error"
	Detail   string `json:"detail,omitempty"`
	NoMarket bool   `json:"-"`
	Err      error  `json:"-"`
}

// measured reports the row produced a usable comparison.
func (r row) priced() bool { return r.Err == nil && !r.NoMarket }

func main() {
	var (
		to        = flag.String("to", "NGNC", "destination token code (any registered corridor token)")
		sizesFlag = flag.String("sizes", defaultSizesFlag(),
			"comma-separated strict-send amounts in USDC")
		record = flag.String("record", "",
			"record upstream responses of both directions as a snapshot in a new directory under this parent")
		allowDirty = flag.Bool("allow-dirty", false,
			"record even though the working tree is modified, marking the manifest dirty")
		jsonOut = flag.Bool("json", false, "emit JSON on stdout instead of the text table")
	)
	flag.Parse()

	token, peg, err := resolveCorridor(*to)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	sizes, err := parseSizes(*sizesFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	var (
		httpClient *http.Client
		recorder   *snapshot.Recorder
	)
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
				Send:          snapshot.AssetRef{Code: asset.USDC().Code, Issuer: asset.USDC().Issuer},
				Receive:       snapshot.AssetRef{Code: token.Code, Issuer: token.Issuer},
				ReferencePair: "USD/" + peg,
			},
			Sizes: sizeStrings(sizes),
			Sources: snapshot.Sources{
				Horizon: snapshot.SourceRef{BaseURL: dex.DefaultHorizonURL},
				// No reference-rate leg exists in this cross-check: both
				// directions are Horizon's own answer to Horizon's own
				// question, compared against each other rather than against
				// an external mid. Recorded rather than left silently blank.
				Reference: snapshot.SourceRef{Provider: "none (direction cross-check; no external mid is consulted)"},
			},
			GitRevision: gitRevision(),
			Notes: []string{
				"strict-send vs strict-receive asymmetry cross-check (#595); " +
					"sizes are the strict-send send amounts, each paired with the " +
					"strict-receive query priced against that size's delivered amount",
			},
		}
		httpClient = &http.Client{Transport: recorder, Timeout: 30 * time.Second}
		dexClient.HTTPClient = httpClient
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	rows := measure(ctx, dexClient, sizes, token)

	if failed := errored(rows); len(failed) > 0 {
		if recorder != nil {
			fmt.Fprintf(os.Stderr,
				"\nnot recording a snapshot: %d of %d sizes failed to reach an upstream (%s).\n"+
					"A snapshot with a gap would replay as a corridor that is partly unpriced. Re-run.\n",
				len(failed), len(rows), strings.Join(failed, ", "))
			os.Exit(1)
		}
		for _, s := range failed {
			fmt.Fprintf(os.Stderr, "error at %s USDC\n", s)
		}
		os.Exit(1)
	}

	if *jsonOut {
		if err := encodeJSON(os.Stdout, token, peg, rows); err != nil {
			fmt.Fprintf(os.Stderr, "encoding result: %v\n", err)
			os.Exit(1)
		}
	} else {
		printTable(token, peg, rows)
	}

	if recorder != nil {
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
}

// resolveCorridor maps -to to the registered token and its peg.
func resolveCorridor(to string) (asset.Asset, string, error) {
	code := strings.ToUpper(strings.TrimSpace(to))
	a, ok := asset.Lookup(code)
	if !ok {
		return asset.Asset{}, "", fmt.Errorf("unknown destination %q; known: %s",
			to, strings.Join(asset.KnownCodes(), ", "))
	}
	peg, ok := asset.FiatPeg(a)
	if !ok {
		return asset.Asset{}, "", fmt.Errorf(
			"destination %q is not a registered fiat-pegged token; an asymmetry cross-check needs a token with a published peg", code)
	}
	return a, peg, nil
}

// measure prices every size in both directions, sequentially: Horizon is a
// shared public service, and a research sweep is in no hurry.
func measure(ctx context.Context, d *dex.Client, sizes []decimal.Decimal, token asset.Asset) []*row {
	rows := make([]*row, 0, len(sizes))
	for _, size := range sizes {
		r := &row{Size: size}
		rows = append(rows, r)

		paths, err := d.StrictSendPaths(ctx, asset.USDC(), size, token)
		if err != nil {
			r.State, r.Err = "error", err
			r.Detail = "strict-send: " + err.Error()
			continue
		}
		if len(paths) == 0 {
			r.State, r.NoMarket = "no-market", true
			r.Detail = "strict-send: no path for " + size.String() + " USDC"
			continue
		}
		// Horizon generally returns its best path first, but that is not a
		// documented guarantee; pick the maximum, as BestPath does.
		best := paths[0]
		for _, p := range paths[1:] {
			if p.DestAmount.GreaterThan(best.DestAmount) {
				best = p
			}
		}
		r.Delivered = best.DestAmount
		r.SendPath = best.Describe()

		recv, err := d.StrictReceivePaths(ctx, asset.USDC(), best.DestAmount, token)
		if err != nil {
			r.State, r.Err = "error", err
			r.Detail = "strict-receive: " + err.Error()
			continue
		}
		if len(recv) == 0 {
			r.State, r.NoMarket = "no-market", true
			r.Detail = "strict-receive: no path delivers " + best.DestAmount.String() + " " + token.Code
			continue
		}
		// Cheapest source amount wins: that is the cost of delivering the
		// same amount from the recipient's side.
		cheapest := recv[0]
		for _, p := range recv[1:] {
			if p.SourceAmount.LessThan(cheapest.SourceAmount) {
				cheapest = p
			}
		}
		r.Required = cheapest.SourceAmount
		r.RecvPath = cheapest.Describe()
		r.State = "priced"
		if size.IsPositive() {
			r.AsymPct = r.Required.Sub(size).Div(size).Mul(decimal.NewFromInt(100))
		}
	}
	return rows
}

// errored names the sizes whose upstream call failed — distinct from a size
// that returned no path, which is a finding rather than a hole.
func errored(rows []*row) []string {
	var out []string
	for _, r := range rows {
		if r.Err != nil {
			out = append(out, r.Size.String())
		}
	}
	return out
}

// encodeJSON writes the machine-readable document, nothing else.
func encodeJSON(w io.Writer, token asset.Asset, peg string, rows []*row) error {
	doc := struct {
		Corridor      string `json:"corridor"`
		ReferencePair string `json:"reference_pair"`
		RunAt         string `json:"run_at"`
		Rows          []*row `json:"rows"`
	}{
		Corridor:      "USDC -> " + token.Code,
		ReferencePair: "USD/" + peg,
		RunAt:         time.Now().UTC().Format(time.RFC3339),
		Rows:          rows,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// printTable renders the human-readable comparison.
func printTable(token asset.Asset, peg string, rows []*row) {
	fmt.Printf("corridor USDC -> %s, strict-send vs strict-receive at matched notional\n", token.Code)
	fmt.Printf("run at %s (benchmark pair USD/%s not consulted; both sides are Horizon)\n\n",
		time.Now().UTC().Format(time.RFC3339), peg)
	fmt.Printf("%-10s %18s %14s %10s  %s\n",
		"SEND", "DELIVERED("+token.Code+")", "REQUIRED", "ASYM%", "PATH")

	priced := 0
	for _, r := range rows {
		switch {
		case r.Err != nil:
			fmt.Printf("%-10s  ERROR: %v\n", r.Size, r.Err)
		case r.NoMarket:
			fmt.Printf("%-10s  NO-MARKET: %s\n", r.Size, r.Detail)
		default:
			priced++
			fmt.Printf("%-10s %18s %14s %10s  %s\n",
				r.Size,
				r.Delivered.StringFixed(2),
				r.Required.StringFixed(2),
				r.AsymPct.StringFixed(4),
				r.SendPath)
		}
	}

	if priced == 0 {
		fmt.Println("\nno size could be priced in both directions")
		return
	}
	min, max := asymExtremes(rows)
	fmt.Printf("\n%d of %d sizes priced in both directions; asymmetry ranges from %s%% to %s%%\n",
		priced, len(rows), min.StringFixed(4), max.StringFixed(4))
}

// asymExtremes returns the smallest and largest asymmetry among priced rows.
func asymExtremes(rows []*row) (decimal.Decimal, decimal.Decimal) {
	var min, max decimal.Decimal
	first := true
	for _, r := range rows {
		if !r.priced() {
			continue
		}
		switch {
		case first:
			min, max, first = r.AsymPct, r.AsymPct, false
		case r.AsymPct.LessThan(min):
			min = r.AsymPct
		case r.AsymPct.GreaterThan(max):
			max = r.AsymPct
		}
	}
	return min, max
}

// parseSizes reads the -sizes flag, skipping malformed entries the way
// cmd/ladder does: a research sweep measures what it can.
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

// defaultSizesFlag renders route.DefaultSizes so the cross-check and the
// ladder measure the same notional ladder (#390's rule, applied here).
func defaultSizesFlag() string {
	parts := make([]string, 0, len(route.DefaultSizes))
	for _, s := range route.DefaultSizes {
		parts = append(parts, s.String())
	}
	return strings.Join(parts, ",")
}

func sizeStrings(sizes []decimal.Decimal) []string {
	out := make([]string, 0, len(sizes))
	for _, s := range sizes {
		out = append(out, s.String())
	}
	return out
}

// gitRevision records which build captured a snapshot. Absent rather than
// guessed when git cannot answer.
func gitRevision() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// requireCleanTree refuses to record from a modified working tree — the same
// provenance rule cmd/ladder applies (see its requireCleanTree for the full
// argument). A dirty tree names a revision that did not produce the bytes.
func requireCleanTree(dir string, allowDirty bool) (bool, error) {
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		return false, nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
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
			"git_revision would name a tree that did not produce these bytes.\n"+
			"Commit or stash first, or pass -allow-dirty.\n\nModified:\n  %s",
		strings.Join(files, "\n  "))
}
