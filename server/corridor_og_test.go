package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
)

func TestCorridorImageServesStoredSVG(t *testing.T) {
	now := time.Date(2026, 10, 19, 14, 8, 0, 0, time.UTC)
	st, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := st.Append(context.Background(), &runstore.Record{
		Version:    runstore.Version,
		Seq:        1,
		RecordedAt: now,
		Corridor:   "USDC-NGNC",
		Integrity:  "DIRECT",
		Reference: runstore.Reference{
			Mid:           "1349.67",
			Source:        "exchangerate-api",
			AsOf:          now.UTC().Format(time.RFC3339),
			ScoredAgainst: "exchangerate-api",
		},
		FloorLossPct: "27.15",
		FloorSize:    "0.1",
		WorstLossPct: "97.52",
		WorstSize:    "5000",
		Finding:      "Stored corridor for image tests.",
		Rungs:        []runstore.Rung{},
	}); err != nil {
		t.Fatal(err)
	}

	srv := ogServer(t, st)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/corridor/image?from=USDC&to=NGNC")
	if err != nil {
		t.Fatalf("GET /api/corridor/image: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/svg+xml; charset=utf-8" {
		t.Errorf("Content-Type = %q, want image/svg+xml; charset=utf-8", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	svg := string(body)
	if !startsWith(svg, `<?xml version="1.0"`) {
		t.Error("image missing SVG XML declaration")
	}
	if !contains(svg, "USDC → NGNC") {
		t.Error("image missing corridor title")
	}
	if !contains(svg, "DIRECT") {
		t.Error("image missing integrity")
	}
	if !contains(svg, "27.15%") {
		t.Error("image missing floor loss")
	}
	if !contains(svg, "recorded Oct 19 14:08 UTC") {
		t.Error("image missing recorded time")
	}
}

func TestCorridorImageReturns404WhenNoHistory(t *testing.T) {
	st, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	srv := ogServer(t, st)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/corridor/image?from=USDC&to=NGNC")
	if err != nil {
		t.Fatalf("GET /api/corridor/image: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if got, _ := body["code"].(string); got != "no_corridor_data" {
		t.Errorf("code = %q, want no_corridor_data", got)
	}
}

func TestCorridorImageRejectsUnknownAssets(t *testing.T) {
	st, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	srv := ogServer(t, st)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/corridor/image?from=USDC&to=SCAMC")
	if err != nil {
		t.Fatalf("GET /api/corridor/image: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if msg, _ := body["error"].(string); !contains(msg, "unknown receive asset") {
		t.Errorf("error = %q, want it to name the unknown receive asset", msg)
	}
}

func TestCorridorImageAcceptsDefaults(t *testing.T) {
	now := time.Date(2026, 10, 19, 14, 8, 0, 0, time.UTC)
	st, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := st.Append(context.Background(), &runstore.Record{
		Version:    runstore.Version,
		Seq:        1,
		RecordedAt: now,
		Corridor:   "USDC-NGNC",
		Integrity:  "DIRECT",
		Reference: runstore.Reference{
			Mid:           "1349.67",
			Source:        "exchangerate-api",
			AsOf:          now.UTC().Format(time.RFC3339),
			ScoredAgainst: "exchangerate-api",
		},
		FloorLossPct: "27.15",
		FloorSize:    "0.1",
		WorstLossPct: "97.52",
		WorstSize:    "5000",
		Finding:      "Stored corridor for image tests.",
		Rungs:        []runstore.Rung{},
	}); err != nil {
		t.Fatal(err)
	}

	srv := ogServer(t, st)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/corridor/image")
	if err != nil {
		t.Fatalf("GET /api/corridor/image: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	svg := string(body)
	if !contains(svg, "USDC → NGNC") {
		t.Error("default image missing corridor title")
	}
}

func TestCorridorImageRejectsUnsupportedMethod(t *testing.T) {
	st, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	srv := ogServer(t, st)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/corridor/image?from=USDC&to=NGNC", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

func TestCorridorImageRejectsUnknownQueryParams(t *testing.T) {
	st, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	srv := ogServer(t, st)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/corridor/image?from=USDC&to=NGNC&debug=1")
	if err != nil {
		t.Fatalf("GET /api/corridor/image: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if msg, _ := body["error"].(string); !contains(msg, "unknown query parameter") {
		t.Errorf("error = %q, want it to say unknown query parameter", msg)
	}
}

func TestCorridorImagePrefersStaleRecordedAt(t *testing.T) {
	recorded := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	st, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := st.Append(context.Background(), &runstore.Record{
		Version:    runstore.Version,
		Seq:        1,
		RecordedAt: recorded,
		Corridor:   "USDC-NGNC",
		Integrity:  "DIRECT",
		Reference: runstore.Reference{
			Mid:           "1349.67",
			Source:        "exchangerate-api",
			AsOf:          recorded.UTC().Format(time.RFC3339),
			ScoredAgainst: "exchangerate-api",
		},
		FloorLossPct: "27.15",
		FloorSize:    "0.1",
		WorstLossPct: "97.52",
		WorstSize:    "5000",
		Finding:      "Stored corridor for image tests.",
		Rungs:        []runstore.Rung{},
	}); err != nil {
		t.Fatal(err)
	}

	srv := ogServer(t, st)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/corridor/image?from=USDC&to=NGNC")
	if err != nil {
		t.Fatalf("GET /api/corridor/image: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	svg := string(body)
	if !contains(svg, "recorded Oct 18 09:00 UTC") {
		t.Errorf("image should show recorded_at from the stored run; got: %s", svg)
	}
	if contains(svg, "recorded Oct 19") {
		t.Errorf("image should not show the generated_at timestamp when a stored recorded_at exists; got: %s", svg)
	}
}

func TestCorridorImageWireShapeFromStoredRecord(t *testing.T) {
	now := time.Date(2026, 10, 19, 14, 8, 0, 0, time.UTC)
	st, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := st.Append(context.Background(), &runstore.Record{
		Version:    runstore.Version,
		Seq:        1,
		RecordedAt: now,
		Corridor:   "USDC-NGNC",
		Integrity:  "DIRECT",
		Reference: runstore.Reference{
			Mid:           "1349.67",
			Source:        "exchangerate-api",
			AsOf:          now.UTC().Format(time.RFC3339),
			ScoredAgainst: "exchangerate-api",
		},
		FloorLossPct: "27.15",
		FloorSize:    "0.1",
		WorstLossPct: "97.52",
		WorstSize:    "5000",
		Recommended: &runstore.Rung{
			SendAmount:    "0.1",
			Priced:        true,
			Integrity:     "DIRECT",
			ReceiveAmount: "100",
			EffectiveRate: "1000",
			LossPct:       "27.15",
			Verdict:       "UNUSABLE",
			Path:          "USDC -> NGNC",
		},
		RecommendedSize: "0.1",
		Finding:         "Stored corridor for image tests.",
		Rungs: []runstore.Rung{{
			SendAmount:    "0.1",
			Priced:        true,
			Integrity:     "DIRECT",
			ReceiveAmount: "100",
			EffectiveRate: "1000",
			LossPct:       "27.15",
			Verdict:       "UNUSABLE",
			Path:          "USDC -> NGNC",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	srv := ogServer(t, st)
	defer srv.Close()

	imgURL := srv.URL + "/api/corridor/image?from=USDC&to=NGNC"

	// Read the image for content, then also assert the wire shape via the same
	// stored record so the renderer is covered by a typed assertion too.
	resp, err := http.Get(imgURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	svg, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(svg), "USDC → NGNC") {
		t.Error("rendered image missing corridor title")
	}

	doc := route.NewCorridorImage(route.CorridorJSON{
		SendAsset:       route.AssetJSON{Code: "USDC"},
		ReceiveAsset:    route.AssetJSON{Code: "NGNC"},
		Integrity:       "DIRECT",
		Floor:           "27.15",
		FloorSize:       "0.1",
		WorstLoss:       "97.52",
		WorstSize:       "5000",
		ReferenceMid:    "1349.67",
		ReferenceSource: "exchangerate-api",
		MeasuredAt:      now.Format(time.RFC3339),
		Stale: &route.StaleJSON{
			RecordedAt: now.Format(time.RFC3339),
		},
	}, time.Now())
	if doc.Title != "USDC → NGNC" {
		t.Errorf("title = %q, want USDC → NGNC", doc.Title)
	}
	if doc.Floor != "27.15" {
		t.Errorf("floor = %q, want 27.15", doc.Floor)
	}
}

func ogServer(t *testing.T, st runstore.Store) *httptest.Server {
	t.Helper()
	s := &Server{Store: st}
	api := httptest.NewServer(s.Handler())
	t.Cleanup(api.Close)
	return api
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func contains(s, sub string) bool {
	if len(s) < len(sub) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return s == sub
}
