package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/runstore"
)

func TestChainHeadsPublishesSortedCurrentTips(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for _, seed := range []struct {
		corridor string
		at       time.Time
	}{
		{corridor: "USDC-NGNC", at: base},
		{corridor: "USDC-GHSC", at: base},
		{corridor: "USDC-NGNC", at: base.Add(6 * time.Hour)},
	} {
		rec := &runstore.Record{Corridor: seed.corridor, RecordedAt: seed.at}
		if err := store.Append(context.Background(), rec); err != nil {
			t.Fatalf("append %s: %v", seed.corridor, err)
		}
	}

	ngnc, err := store.Latest(context.Background(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	ghsc, err := store.Latest(context.Background(), "USDC-GHSC")
	if err != nil {
		t.Fatal(err)
	}

	api := httptest.NewServer((&Server{Store: store}).Handler())
	t.Cleanup(api.Close)
	status, body := getJSON(t, api.URL+"/api/chain-heads")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %v", status, body)
	}
	rawHeads, ok := body["heads"].([]any)
	if !ok || len(rawHeads) != 2 {
		t.Fatalf("heads = %v, want two corridor heads", body["heads"])
	}

	first := rawHeads[0].(map[string]any)
	second := rawHeads[1].(map[string]any)
	if first["corridor"] != "USDC-GHSC" || second["corridor"] != "USDC-NGNC" {
		t.Fatalf("heads are not in stable corridor order: %v", rawHeads)
	}
	assertPublishedHead(t, first, ghsc)
	assertPublishedHead(t, second, ngnc)
}

func assertPublishedHead(t *testing.T, got map[string]any, want *runstore.Record) {
	t.Helper()
	if got["seq"] != float64(want.Seq) {
		t.Errorf("seq = %v, want %d", got["seq"], want.Seq)
	}
	if got["recorded_at"] != want.RecordedAt.UTC().Format(time.RFC3339) {
		t.Errorf("recorded_at = %v, want %s", got["recorded_at"], want.RecordedAt.UTC().Format(time.RFC3339))
	}
	if got["hash"] != want.Hash {
		t.Errorf("hash = %v, want the current full chain hash %s", got["hash"], want.Hash)
	}
}

func TestChainHeadsEmptyStoreReturnsEmptyArray(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer((&Server{Store: store}).Handler())
	t.Cleanup(api.Close)

	status, body := getJSON(t, api.URL+"/api/chain-heads")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %v", status, body)
	}
	heads, ok := body["heads"].([]any)
	if !ok || len(heads) != 0 {
		t.Errorf("heads = %v, want an empty array", body["heads"])
	}
}

func TestChainHeadsRejectsUnsupportedMethodAndUnknownParameters(t *testing.T) {
	api := httptest.NewServer((&Server{}).Handler())
	t.Cleanup(api.Close)

	for _, tc := range []struct {
		method string
		path   string
		want   int
	}{
		{method: http.MethodPost, path: "/api/chain-heads", want: http.StatusMethodNotAllowed},
		{method: http.MethodGet, path: "/api/chain-heads?unknown=1", want: http.StatusBadRequest},
	} {
		req, err := http.NewRequest(tc.method, api.URL+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s %s returned %d, want %d", tc.method, tc.path, resp.StatusCode, tc.want)
		}
	}
}

func TestChainHeadsResponseCanBeDecodedByConsumers(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(context.Background(), &runstore.Record{
		Corridor:   "USDC-NGNC",
		RecordedAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	api := httptest.NewServer((&Server{Store: store}).Handler())
	t.Cleanup(api.Close)
	resp, err := http.Get(api.URL + "/api/chain-heads?pretty=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body chainHeadsJSON
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode chain-head response: %v", err)
	}
	if resp.StatusCode != http.StatusOK || len(body.Heads) != 1 || body.Heads[0].Hash == "" {
		t.Errorf("decoded response = %+v, status %d; want one head with a hash", body, resp.StatusCode)
	}
}
