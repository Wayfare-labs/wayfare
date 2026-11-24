package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Wayfare-labs/wayfare/analysis"
	"github.com/Wayfare-labs/wayfare/runstore"
)

func run(args []string, out io.Writer, errOut io.Writer) int {
	flags := flag.NewFlagSet("analyse", flag.ContinueOnError)
	flags.SetOutput(errOut)

	dataDir := flags.String("data", "./data", "path to runstore data directory")
	corridor := flags.String("corridor", "USDC-NGNC", "corridor key to analyse, e.g. USDC-NGNC")

	if err := flags.Parse(args); err != nil {
		return 2
	}

	if *dataDir == "" || *corridor == "" {
		fmt.Fprintf(errOut, "usage: analyse -data <dir> -corridor <corridor>\n")
		return 2
	}

	ctx := context.Background()
	store, err := runstore.Open(*dataDir)
	if err != nil {
		fmt.Fprintf(errOut, "refusing to analyse unverified chain: opening runstore at %s: %v\n", *dataDir, err)
		return 1
	}

	// Refuse to analyse an unverified chain.
	if err := store.Verify(ctx, *corridor); err != nil {
		fmt.Fprintf(errOut, "refusing to analyse unverified chain for %s: %v\n", *corridor, err)
		return 1
	}

	records, err := store.All(ctx, *corridor)
	if err != nil {
		fmt.Fprintf(errOut, "error reading records for %s: %v\n", *corridor, err)
		return 1
	}

	total := len(records)
	if total == 0 {
		fmt.Fprintf(out, "Corridor: %s\nStatus: verified\nTotal records: 0/0\nResult: no records to analyse\n", *corridor)
		return 0
	}

	fmt.Fprintf(out, "Corridor: %s (chain verified intact)\n", *corridor)
	fmt.Fprintf(out, "Total observations: %d/%d (100.0%%)\n\n", total, total)

	divStats, err := analysis.DivergenceHistory(records)
	if err != nil {
		fmt.Fprintf(errOut, "divergence analysis error: %v\n", err)
		return 1
	}

	if divStats != nil {
		fmt.Fprintln(out, "--- Reference Divergence Analysis ---")
		fmt.Fprintf(out, "Sample size: %d/%d observations (denominator: %d total records)\n", divStats.ObservationCount, total, total)
		if !divStats.Undetermined && divStats.Mean != nil {
			fmt.Fprintf(out, "Mean divergence: %s%% (n=%d/%d)\n", divStats.Mean.StringFixed(4), divStats.ObservationCount, total)
			if divStats.StdDev != nil {
				fmt.Fprintf(out, "StdDev divergence: %s%% (n=%d/%d)\n", divStats.StdDev.StringFixed(4), divStats.ObservationCount, total)
			}
			fmt.Fprintf(out, "Regime: %s (n=%d/%d)\n", divStats.Regime, divStats.ObservationCount, total)
			if divStats.Trend != nil {
				fmt.Fprintf(out, "Trend: %s (slope=%s%%/obs, n=%d/%d)\n", divStats.Trend.Direction, divStats.Trend.Slope.StringFixed(4), divStats.ObservationCount, total)
			}
		} else {
			fmt.Fprintf(out, "Status: UNDETERMINED (n=%d/%d < minimum sample threshold %d)\n", divStats.ObservationCount, total, analysis.MinSampleSizeForMeanStdDev)
		}
	}

	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
