package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/runstore"
)

func TestAnalyseRefusesUnverifiedChain(t *testing.T) {
	dir := t.TempDir()
	store, err := runstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	corridor := "USDC-NGNC"
	ctx := context.Background()

	// Append a valid record
	rec := &runstore.Record{
		Version:      1,
		Corridor:     corridor,
		Integrity:    "DIRECT",
		FloorLossPct: "-1.50",
		FloorSize:    "100",
		WorstLossPct: "-3.00",
		WorstSize:    "5000",
		Reference: runstore.Reference{
			Mid:    "1500.00",
			Source: "currency-api",
		},
		RecordedAt: time.Now().UTC(),
	}
	if err := store.Append(ctx, rec); err != nil {
		t.Fatal(err)
	}

	// Tamper with the chain file
	filePath := filepath.Join(dir, corridor+".ndjson")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(content, []byte("-1.50"), []byte("-9.99"), 1)
	if err := os.WriteFile(filePath, tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := run([]string{"-data", dir, "-corridor", corridor}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit code 1 for tampered chain, got %d", code)
	}
	if !strings.Contains(errOut.String(), "refusing to analyse unverified chain") {
		t.Errorf("errOut = %q, want mention of refusing unverified chain", errOut.String())
	}
}

func TestAnalysePrintsDenominatorsForVerifiedChain(t *testing.T) {
	dir := t.TempDir()
	store, err := runstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	corridor := "USDC-NGNC"
	ctx := context.Background()

	// Append 35 records with divergence
	for i := 0; i < 35; i++ {
		rec := &runstore.Record{
			Version:      1,
			Corridor:     corridor,
			Integrity:    "DIRECT",
			FloorLossPct: "-1.50",
			FloorSize:    "100",
			WorstLossPct: "-3.00",
			WorstSize:    "5000",
			Reference: runstore.Reference{
				Mid:           "1500.00",
				Source:        "currency-api",
				DivergencePct: "0.25",
			},
			RecordedAt: time.Now().UTC().Add(time.Duration(i) * time.Hour),
		}
		if err := store.Append(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	var out, errOut bytes.Buffer
	code := run([]string{"-data", dir, "-corridor", corridor}, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; errOut: %s", code, errOut.String())
	}

	stdout := out.String()
	if !strings.Contains(stdout, "Total observations: 35/35") {
		t.Errorf("stdout missing total denominator: %s", stdout)
	}
	if !strings.Contains(stdout, "35/35") {
		t.Errorf("stdout missing sample size denominator: %s", stdout)
	}
	if !strings.Contains(stdout, "n=35/35") {
		t.Errorf("stdout missing figure denominator n=35/35: %s", stdout)
	}
}
