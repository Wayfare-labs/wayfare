package dex

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

func FuzzStrictSendPaths(f *testing.F) {
	var seeds []string
	err := filepath.WalkDir("../testdata/snapshots", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.Contains(entry.Name(), "paths-strict-send-") ||
			!strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		seeds = append(seeds, string(body))
		return nil
	})
	if err != nil {
		f.Fatalf("reading strict-send snapshot seeds: %v", err)
	}
	if len(seeds) == 0 {
		f.Fatal("no paths-strict-send-*.json snapshots found under ../testdata/snapshots")
	}
	for _, body := range seeds {
		f.Add(body)
	}

	f.Add(`{"_embedded":{"records":[]}}`)
	f.Add(`{"_embedded":{"records":[{"source_amount":"1","destination_amount":"1","path":[]}]}}`)
	f.Add(`{"_embedded":{"records":[{"source_amount":"1","destination_amount":"0","path":[]}]}}`)
	f.Add(`{"_embedded":{"records":[{"source_amount":"1","destination_amount":"1.12345678","path":[]}]}}`)
	f.Add(`{"_embedded":{"records":[null]}}`)
	f.Add(`null`)

	f.Fuzz(func(t *testing.T, body string) {
		client := &Client{
			HorizonURL: "http://strict-send.fuzz",
			HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/hal+json"}},
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			})},
		}

		paths, err := client.StrictSendPaths(
			context.Background(), asset.USDC(), decimal.NewFromInt(1), asset.NGNC())
		if err != nil {
			return
		}
		for i, path := range paths {
			if !path.DestAmount.IsPositive() {
				t.Fatalf("accepted path %d has non-positive destination amount %s", i, path.DestAmount)
			}
			if path.DestAmount.Exponent() < -7 {
				t.Fatalf("accepted path %d has more than 7 decimal places: %s", i, path.DestAmount)
			}
		}
	})
}
