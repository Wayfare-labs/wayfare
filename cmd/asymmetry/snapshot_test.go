package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// loadAsymmetrySnapshot finds the recorded cross-check for one corridor.
func loadAsymmetrySnapshot(t *testing.T, prefix string) *snapshot.Manifest {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("..", "..", "testdata", "snapshots", "asymmetry", prefix+"-*"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no %s asymmetry snapshot (err=%v); capture one with: "+
			"go run ./cmd/asymmetry -to %s -record testdata/snapshots/asymmetry",
			prefix, err, prefix)
	}
	m, err := snapshot.Load(matches[0])
	if err != nil {
		t.Fatalf("loading %s: %v", matches[0], err)
	}
	return m
}

// TestAsymmetrySnapshotPinsFinding replays the two recorded naira cross-checks
// and holds them to the figures in docs/strict-receive-asymmetry-research.md.
//
// The measurement found the two directions exactly inverse at every matched
// size: asking Horizon what it costs to deliver the amount strict-send said a
// size buys returns that very size. That is the finding — the strict-send
// ladder does not describe half the corridor on these tokens, it describes all
// of it — and an implementation that started reporting a spread here would
// silently contradict the write-up.
func TestAsymmetrySnapshotPinsFinding(t *testing.T) {
	cases := []struct {
		prefix    string
		token     asset.Asset
		delivered []string // to two decimals, as published
	}{
		{
			prefix: "usdc-ngnc",
			token:  asset.NGNC(),
			delivered: []string{
				"147.47", "1462.81", "7111.71", "13749.18", "31262.38", "54336.32",
				"86116.49", "132676.20", "161843.64", "181830.37", "196382.79", "201768.49",
			},
		},
		{
			prefix: "usdc-ngnt",
			token:  asset.NGNT(),
			delivered: []string{
				"924.91", "9143.73", "43506.86", "82057.42", "175254.20", "282059.99",
				"424304.93", "711775.06", "1010425.82", "1379925.42", "2181313.42", "3032207.60",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.prefix, func(t *testing.T) {
			m := loadAsymmetrySnapshot(t, tc.prefix)
			client := &dex.Client{HorizonURL: dex.DefaultHorizonURL, HTTPClient: m.HTTPClient()}

			rows := measure(context.Background(), client, route.DefaultSizes, tc.token)
			if len(rows) != len(route.DefaultSizes) {
				t.Fatalf("measured %d rows, want %d", len(rows), len(route.DefaultSizes))
			}
			if len(tc.delivered) != len(rows) {
				t.Fatalf("pinned %d figures for %d rows", len(tc.delivered), len(rows))
			}

			for i, r := range rows {
				if r.Err != nil {
					t.Fatalf("%s USDC errored: %v", r.Size, r.Err)
				}
				if r.NoMarket || r.State != "priced" {
					t.Fatalf("%s USDC state = %q, want priced in both directions", r.Size, r.State)
				}
				// The delivered amount is what the write-up publishes, to
				// two decimals.
				if got := r.Delivered.StringFixed(2); got != tc.delivered[i] {
					t.Errorf("%s USDC delivered = %s, want %s", r.Size, got, tc.delivered[i])
				}
				// The matched strict-receive quote is exactly inverse.
				if !r.Required.Equal(r.Size) {
					t.Errorf("%s USDC required = %s; want the same amount",
						r.Size, r.Required)
				}
				if !r.AsymPct.IsZero() {
					t.Errorf("%s USDC asymmetry = %s%%, want 0.0000%%",
						r.Size, r.AsymPct.StringFixed(4))
				}
			}
		})
	}
}

// TestAsymmetrySnapshotHasBothDirections proves the recorded snapshot actually
// contains the second direction's bytes. A snapshot holding only strict-send
// would replay as zero asymmetry for the wrong reason: every strict-receive
// request would fail with ErrNotRecorded, and a test that only checked the
// summary would call that agreement.
func TestAsymmetrySnapshotHasBothDirections(t *testing.T) {
	m := loadAsymmetrySnapshot(t, "usdc-ngnc")
	var sends, receives int
	for _, key := range m.Keys() {
		switch {
		case strings.Contains(key, "/paths/strict-send?"):
			sends++
		case strings.Contains(key, "/paths/strict-receive?"):
			receives++
		}
	}
	if sends != len(route.DefaultSizes) {
		t.Errorf("recorded %d strict-send bodies, want one per size (%d)", sends, len(route.DefaultSizes))
	}
	if receives != len(route.DefaultSizes) {
		t.Errorf("recorded %d strict-receive bodies, want one per size (%d)", receives, len(route.DefaultSizes))
	}
}
