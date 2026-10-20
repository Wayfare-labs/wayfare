package server_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/server"
)

func TestRequestIDGeneratedWhenAbsent(t *testing.T) {
	srv := httptest.NewServer(testServerHandler(t))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/healthz", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	reqID := resp.Header.Get("X-Request-Id")
	if reqID == "" {
		t.Fatal("response header X-Request-Id is missing")
	}
	if len(reqID) != 32 { // 16 bytes hex-encoded
		t.Errorf("X-Request-Id = %q, want 32 hex chars", reqID)
	}
	if expose := resp.Header.Get("Access-Control-Expose-Headers"); !strings.Contains(expose, "X-Request-Id") {
		t.Errorf("Access-Control-Expose-Headers = %q, want it to contain X-Request-Id", expose)
	}
}

func TestRequestIDPropagatedWhenPresent(t *testing.T) {
	srv := httptest.NewServer(testServerHandler(t))
	defer srv.Close()

	customID := "client-trace-123456"
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/healthz", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Request-Id", customID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	reqID := resp.Header.Get("X-Request-Id")
	if reqID != customID {
		t.Errorf("X-Request-Id = %q, want propagated %q", reqID, customID)
	}
}

func TestRequestIDInErrorBody(t *testing.T) {
	srv := httptest.NewServer(testServerHandler(t))
	defer srv.Close()

	customID := "err-trace-999"
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/corridor?from=INVALID", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Request-Id", customID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/corridor: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if resp.Header.Get("X-Request-Id") != customID {
		t.Errorf("header X-Request-Id = %q, want %q", resp.Header.Get("X-Request-Id"), customID)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if body["request_id"] != customID {
		t.Errorf("error body request_id = %q, want %q", body["request_id"], customID)
	}
}

func TestRequestIDLoggedOnRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	server.SetLogger(logger)
	defer server.SetLogger(nil)

	srv := httptest.NewServer(testServerHandler(t))
	defer srv.Close()

	customID := "log-trace-777"
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/corridor?from=USDC&to=NGNC", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Request-Id", customID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/corridor: %v", err)
	}
	defer resp.Body.Close()

	logOutput := buf.String()
	if !strings.Contains(logOutput, customID) {
		t.Errorf("log output does not contain request_id %q: %s", customID, logOutput)
	}
}

func testServerHandler(t *testing.T) http.Handler {
	t.Helper()
	fakeHorizon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		amount := r.URL.Query().Get("source_amount")
		_, _ = w.Write([]byte(`{"_embedded":{"records":[{
			"source_asset_type":"credit_alphanum4","source_asset_code":"USDC",
			"source_amount":"` + amount + `",
			"destination_asset_type":"credit_alphanum4","destination_asset_code":"NGNC",
			"destination_amount":"130000.0000000","path":[]}]}}`))
	}))
	t.Cleanup(fakeHorizon.Close)

	eng := &route.Engine{
		DEX: &dex.Client{HorizonURL: fakeHorizon.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/NGN": decimal.RequireFromString("1350"),
		}),
	}
	s := &server.Server{
		Engine: eng,
	}
	return s.Handler()
}
