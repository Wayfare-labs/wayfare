// Command hop-analysis replays the recorded corridor snapshots and reports
// how often the best path traverses native XLM, alongside the best non-XLM
// alternative.
//
// It is the reproducibility half of issue #101 — docs/native-xlm-routing.md
// states the finding, this tool derives that finding from the same
// testdata/snapshots directory the docs cite, so a reader can re-run it and
// see the numbers do not depend on remembering.
//
//	go run ./cmd/hop-analysis                        # text table, all corridors
//	go run ./cmd/hop-analysis -json                  # JSON on stdout
//	go run ./cmd/hop-analysis -snapshots ./testdata/snapshots
//
// The tool never touches the network. Every figure it prints comes from
// bytes in the snapshots directory, verified against their recorded hashes
// by snapshot.Load.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// PathSummary is one path's contribution to the analysis, in the form the
// finding needs: whether it uses XLM, the classes of every hop, and the
// destination amount for comparison.
type PathSummary struct {
	Description  string          `json:"description"`
	HopClasses   []asset.Class   `json:"-"`
	HopClassStrs []string        `json:"hop_classes"`
	UsesXLM      bool            `json:"uses_xlm"`
	DestAmount   decimal.Decimal `json:"-"`
	DestAmountS  string          `json:"dest_amount"`
	SourceAmount decimal.Decimal `json:"-"`
	Rate         decimal.Decimal `json:"-"`
	RateS        string          `json:"rate"`
	_            struct{}        // keep struct extensible without breaking positional JSON
}

// SizeBreakdown holds every path Horizon returned for one send amount on one
// corridor, plus which is best overall and which is best without XLM.
type SizeBreakdown struct {
	SendAmount     string        `json:"send_amount"`
	NumPaths       int           `json:"num_paths"`
	Best           *PathSummary  `json:"best,omitempty"`
	BestNonXLM     *PathSummary  `json:"best_non_xlm,omitempty"`
	XLMAdvantagePc string        `json:"xlm_advantage_pct,omitempty"`
	Paths          []PathSummary `json:"paths"`
}

// CorridorReport is the per-corridor rollup: hop-composition counts across
// every size, and one SizeBreakdown per size for reproducibility.
type CorridorReport struct {
	Snapshot         string          `json:"snapshot"`
	SendCode         string          `json:"send"`
	ReceiveCode      string          `json:"receive"`
	SizesMeasured    int             `json:"sizes_measured"`
	SizesWithAnyPath int             `json:"sizes_with_any_path"`
	SizesBestUsesXLM int             `json:"sizes_best_uses_xlm"`
	SizesWithNonXLM  int             `json:"sizes_with_non_xlm_alt"`
	SummaryLine      string          `json:"summary"`
	Sizes            []SizeBreakdown `json:"sizes"`
}

// Report is the whole document — one entry per corridor snapshot.
type Report struct {
	Corridors []CorridorReport `json:"corridors"`
	Note      string           `json:"note"`
}

func main() {
	var (
		snapshotsDir = flag.String("snapshots", "testdata/snapshots",
			"parent directory holding recorded corridor snapshots")
		jsonOut = flag.Bool("json", false, "emit JSON on stdout instead of a text table")
	)
	flag.Parse()

	report, err := Analyse(*snapshotsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "encoding report: %v\n", err)
			os.Exit(1)
		}
		return
	}
	printText(os.Stdout, report)
}

// Analyse walks a snapshots parent directory and produces one CorridorReport
// per snapshot it can load. A directory that is not a valid snapshot is
// skipped with a stderr line rather than aborting the whole run — this tool
// exists to summarise what is on disk, and a broken sibling should not hide
// a good corridor.
func Analyse(snapshotsDir string) (*Report, error) {
	entries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		return nil, fmt.Errorf("reading snapshots dir %s: %w", snapshotsDir, err)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	out := &Report{
		Note: "Every figure derives from recorded Horizon responses in the named " +
			"snapshot; no network calls are made. See docs/native-xlm-routing.md.",
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		full := filepath.Join(snapshotsDir, e.Name())
		m, err := snapshot.Load(full)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", e.Name(), err)
			continue
		}
		cr, err := analyseSnapshot(m)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", e.Name(), err)
			continue
		}
		out.Corridors = append(out.Corridors, cr)
	}
	return out, nil
}

// analyseSnapshot builds a CorridorReport by asking the recorded snapshot
// itself for every strict-send path it holds. It intentionally avoids
// route.Engine — that would score each path and pull in a reference rate,
// and the finding here is about hop composition, not about scoring.
func analyseSnapshot(m *snapshot.Manifest) (CorridorReport, error) {
	send, recv, err := sendRecv(m)
	if err != nil {
		return CorridorReport{}, err
	}

	c := &dex.Client{
		HorizonURL: "https://horizon.stellar.org",
		HTTPClient: m.HTTPClient(),
	}
	ctx := context.Background()

	// Enumerate every strict-send probe the snapshot recorded, in ascending
	// numerical order. The size is embedded in each request key; extracting
	// it from there rather than re-declaring a size list means a snapshot
	// captured with a non-default -sizes list still analyses correctly.
	sizes := probedSizes(m)

	cr := CorridorReport{
		Snapshot:      m.Name(),
		SendCode:      send.Code,
		ReceiveCode:   recv.Code,
		SizesMeasured: len(sizes),
	}

	for _, size := range sizes {
		paths, err := c.StrictSendPaths(ctx, send, size, recv)
		if err != nil {
			// A missing key means the snapshot did not record this probe —
			// unusual but not fatal, and worth surfacing.
			fmt.Fprintf(os.Stderr, "%s: no recorded response for size %s: %v\n",
				m.Name(), size, err)
			continue
		}

		sb := SizeBreakdown{SendAmount: size.String(), NumPaths: len(paths)}
		if len(paths) > 0 {
			cr.SizesWithAnyPath++
		}

		var best, bestNonXLM *PathSummary
		for _, p := range paths {
			ps := summarisePath(p)
			sb.Paths = append(sb.Paths, ps)
			if best == nil || ps.DestAmount.GreaterThan(best.DestAmount) {
				b := ps
				best = &b
			}
			if !ps.UsesXLM {
				if bestNonXLM == nil || ps.DestAmount.GreaterThan(bestNonXLM.DestAmount) {
					b := ps
					bestNonXLM = &b
				}
			}
		}
		sb.Best = best
		sb.BestNonXLM = bestNonXLM
		if best != nil && best.UsesXLM {
			cr.SizesBestUsesXLM++
		}
		if bestNonXLM != nil {
			cr.SizesWithNonXLM++
		}
		if best != nil && bestNonXLM != nil && !bestNonXLM.DestAmount.IsZero() {
			adv := best.DestAmount.Sub(bestNonXLM.DestAmount).
				Div(bestNonXLM.DestAmount).
				Mul(decimal.NewFromInt(100))
			sb.XLMAdvantagePc = adv.StringFixed(2)
		}
		cr.Sizes = append(cr.Sizes, sb)
	}

	cr.SummaryLine = summariseCorridor(cr)
	return cr, nil
}

// summarisePath collapses a dex.Path into just the fields the finding needs.
func summarisePath(p dex.Path) PathSummary {
	usesXLM := false
	classes := make([]asset.Class, 0, len(p.Hops)+2)
	strs := make([]string, 0, len(p.Hops)+2)
	classes = append(classes, asset.Classify(p.SourceAsset))
	strs = append(strs, classes[0].String())
	for _, h := range p.Hops {
		c := asset.Classify(h)
		classes = append(classes, c)
		strs = append(strs, c.String())
		if c == asset.ClassNative {
			usesXLM = true
		}
	}
	classes = append(classes, asset.Classify(p.DestAsset))
	strs = append(strs, classes[len(classes)-1].String())

	return PathSummary{
		Description:  p.Describe(),
		HopClasses:   classes,
		HopClassStrs: strs,
		UsesXLM:      usesXLM,
		DestAmount:   p.DestAmount,
		DestAmountS:  p.DestAmount.String(),
		SourceAmount: p.SourceAmount,
		Rate:         p.Rate(),
		RateS:        p.Rate().StringFixed(6),
	}
}

func summariseCorridor(cr CorridorReport) string {
	if cr.SizesWithAnyPath == 0 {
		return fmt.Sprintf(
			"%s -> %s: no paths at any of %d sizes (NO-MARKET). "+
				"Native XLM routing is inapplicable: nothing routes at all.",
			cr.SendCode, cr.ReceiveCode, cr.SizesMeasured)
	}
	return fmt.Sprintf(
		"%s -> %s: %d/%d sizes priced; best path traverses XLM at %d/%d, and "+
			"a non-XLM alternative exists at %d/%d.",
		cr.SendCode, cr.ReceiveCode,
		cr.SizesWithAnyPath, cr.SizesMeasured,
		cr.SizesBestUsesXLM, cr.SizesWithAnyPath,
		cr.SizesWithNonXLM, cr.SizesWithAnyPath)
}

// sendRecv reads the corridor's send/receive asset from the manifest — the
// only place a snapshot records them.
func sendRecv(m *snapshot.Manifest) (send, recv asset.Asset, err error) {
	c := m.Corridor
	if c.Send.Code == "" || c.Receive.Code == "" {
		return asset.Asset{}, asset.Asset{},
			fmt.Errorf("snapshot %s: manifest has no corridor asset codes", m.Name())
	}
	send = asset.Stellar(c.Send.Code, c.Send.Issuer)
	recv = asset.Stellar(c.Receive.Code, c.Receive.Issuer)
	return send, recv, nil
}

// probedSizes lifts the send amounts from the snapshot's manifest field.
// The recorder writes them there in the order the user asked; sorting them
// ascending here gives a stable analysis.
func probedSizes(m *snapshot.Manifest) []decimal.Decimal {
	out := make([]decimal.Decimal, 0, len(m.Sizes))
	for _, s := range m.Sizes {
		d, err := decimal.NewFromString(s)
		if err != nil {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LessThan(out[j]) })
	return out
}

// printText renders the report as a human-readable table for terminals.
func printText(w *os.File, r *Report) {
	fmt.Fprintln(w, "Hop-composition analysis (issue #101, native XLM routing)")
	fmt.Fprintln(w, "Source: recorded snapshots, replay only — no network calls.")
	fmt.Fprintln(w, strings.Repeat("-", 78))

	for _, cr := range r.Corridors {
		fmt.Fprintf(w, "\n%s  [%s]\n", cr.SummaryLine, cr.Snapshot)
		if cr.SizesWithAnyPath == 0 {
			continue
		}
		fmt.Fprintf(w, "%-8s %6s  %-10s  %-30s  %10s\n",
			"SIZE", "PATHS", "BEST USES", "BEST PATH", "XLM ADV%")
		for _, s := range cr.Sizes {
			best := "-"
			bestUses := "-"
			if s.Best != nil {
				best = s.Best.Description
				if s.Best.UsesXLM {
					bestUses = "XLM"
				} else {
					bestUses = "no-XLM"
				}
			}
			fmt.Fprintf(w, "%-8s %6d  %-10s  %-30s  %10s\n",
				s.SendAmount, s.NumPaths, bestUses, best, s.XLMAdvantagePc)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, r.Note)
}
