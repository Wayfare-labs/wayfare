package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// renderedIndexHTML returns the bytes a Server with no Store writes for GET /
// — the embedded index.html rendered through uiTemplate with a nil Summary.
// Round-trip tests use this so they compare against what the handler actually
// serves, not the raw embedded file (which carries template directives).
// renderedIndexHTML renders the page exactly as the handler serves it to a
// store-less server at srvURL: the Open Graph block names the request host.
func renderedIndexHTML(t *testing.T, srvURL string) []byte {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, srvURL+"/", nil)
	var buf bytes.Buffer
	if err := uiTemplate.Execute(&buf, uiPageData{SocialMeta: template.HTML(socialMetadata(req, nil))}); err != nil {
		t.Fatalf("rendering template: %v", err)
	}
	return buf.Bytes()
}

// clientWithoutDecompression returns an http.Client that does not perform
// transparent gzip decompression, allowing tests to inspect the raw wire bytes
// and headers (Content-Encoding, Content-Length).
func clientWithoutDecompression() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DisableCompression: true,
		},
	}
}

// TestGzipAccepted_IndexHTML guards issue #610: GET / with Accept-Encoding: gzip
// must serve index.html compressed, with Vary: Accept-Encoding set.
func TestGzipAccepted_IndexHTML(t *testing.T) {
	s := &Server{}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	client := clientWithoutDecompression()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if vary := resp.Header.Get("Vary"); !strings.Contains(vary, "Accept-Encoding") {
		t.Errorf("Vary = %q, want it to contain Accept-Encoding", vary)
	}

	compressed, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading compressed body: %v", err)
	}

	gzReader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gzReader.Close()

	decompressed, err := io.ReadAll(gzReader)
	if err != nil {
		t.Fatalf("decompressing body: %v", err)
	}

	rawExpected := renderedIndexHTML(t, srv.URL)

	if !bytes.Equal(decompressed, rawExpected) {
		t.Errorf("decompressed body length %d != expected %d", len(decompressed), len(rawExpected))
	}

	// Verify meaningful compression: ~100 KB HTML should compress to < 40 KB.
	if len(compressed) >= len(rawExpected)/2 {
		t.Errorf("compressed length %d is not substantially smaller than %d", len(compressed), len(rawExpected))
	}
}

// TestGzipNotAccepted_IndexHTML guards issue #610: GET / without Accept-Encoding: gzip
// must serve index.html uncompressed, while still setting Vary: Accept-Encoding.
func TestGzipNotAccepted_IndexHTML(t *testing.T) {
	s := &Server{}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	client := clientWithoutDecompression()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	// No Accept-Encoding header set.

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
	if vary := resp.Header.Get("Vary"); !strings.Contains(vary, "Accept-Encoding") {
		t.Errorf("Vary = %q, want it to contain Accept-Encoding", vary)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	rawExpected := renderedIndexHTML(t, srv.URL)

	if !bytes.Equal(body, rawExpected) {
		t.Errorf("uncompressed body length %d != expected %d", len(body), len(rawExpected))
	}
}

// TestGzipAccepted_JSON guards issue #610: JSON responses exceeding minGzipSize
// are compressed when the client accepts gzip.
func TestGzipAccepted_JSON(t *testing.T) {
	payload := map[string]any{
		"corridor": "USDC-NGNC",
		"rungs":    strings.Repeat("a large json block of corridor pricing data ", 30),
	}
	expectedJSON, _ := json.Marshal(payload)
	if len(expectedJSON) < minGzipSize {
		t.Fatalf("test payload length %d must exceed minGzipSize %d", len(expectedJSON), minGzipSize)
	}

	handler := withGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := clientWithoutDecompression()
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if vary := resp.Header.Get("Vary"); !strings.Contains(vary, "Accept-Encoding") {
		t.Errorf("Vary = %q, want it to contain Accept-Encoding", vary)
	}

	gzReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gzReader.Close()

	var gotDoc map[string]any
	if err := json.NewDecoder(gzReader).Decode(&gotDoc); err != nil {
		t.Fatalf("decoding decompressed JSON: %v", err)
	}
	if gotDoc["corridor"] != "USDC-NGNC" {
		t.Errorf("corridor = %v, want USDC-NGNC", gotDoc["corridor"])
	}
}

// TestGzipNotAccepted_JSON guards issue #610: JSON responses exceeding minGzipSize
// are NOT compressed when the client does not send Accept-Encoding: gzip.
func TestGzipNotAccepted_JSON(t *testing.T) {
	payload := map[string]any{
		"corridor": "USDC-NGNC",
		"rungs":    strings.Repeat("a large json block of corridor pricing data ", 30),
	}

	handler := withGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := clientWithoutDecompression()
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
	if vary := resp.Header.Get("Vary"); !strings.Contains(vary, "Accept-Encoding") {
		t.Errorf("Vary = %q, want it to contain Accept-Encoding", vary)
	}

	var gotDoc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&gotDoc); err != nil {
		t.Fatalf("decoding uncompressed JSON: %v", err)
	}
	if gotDoc["corridor"] != "USDC-NGNC" {
		t.Errorf("corridor = %v, want USDC-NGNC", gotDoc["corridor"])
	}
}

// TestGzipSkipTinyBody guards issue #610: bodies smaller than minGzipSize
// are sent uncompressed even when the client accepts gzip.
func TestGzipSkipTinyBody(t *testing.T) {
	s := &Server{}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	client := clientWithoutDecompression()
	// An invalid query triggers writeError which produces a tiny error JSON payload (< 100 bytes).
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/corridor?unknown_param=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty for tiny body", got)
	}
	if vary := resp.Header.Get("Vary"); !strings.Contains(vary, "Accept-Encoding") {
		t.Errorf("Vary = %q, want it to contain Accept-Encoding", vary)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) >= minGzipSize {
		t.Fatalf("error body length %d is unexpectedly >= minGzipSize %d", len(raw), minGzipSize)
	}

	var errResp map[string]string
	if err := json.Unmarshal(raw, &errResp); err != nil {
		t.Fatalf("error response is not valid JSON: %v\n%s", err, raw)
	}
	if errResp["code"] != codeInvalidQuery {
		t.Errorf("code = %q, want %q", errResp["code"], codeInvalidQuery)
	}
}

// TestGzipAcceptEncodingParsing tests the various shapes of Accept-Encoding.
func TestGzipAcceptEncodingParsing(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"", false},
		{"identity", false},
		{"gzip", true},
		{"GZIP", true},
		{"gzip, deflate", true},
		{"deflate, gzip", true},
		{"deflate, gzip;q=0.9", true},
		{"gzip;q=1.0, *;q=0.5", true},
		{"gzip;q=0", false},
		{"gzip; q=0.0", false},
		{"gzip;q=0.000", false},
		{"*;q=0", false},
		{"*", true},
		{"br;q=1.0, gzip;q=0.8", true},
		{"br, zstd", false},
	}

	for _, tc := range cases {
		t.Run(tc.header, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				req.Header.Set("Accept-Encoding", tc.header)
			}
			if got := acceptsGzip(req); got != tc.want {
				t.Errorf("acceptsGzip(%q) = %v, want %v", tc.header, got, tc.want)
			}
		})
	}
}

// TestGzipChunkedWritesCrossingThreshold tests that multiple small writes that
// cumulatively exceed minGzipSize correctly trigger compression.
func TestGzipChunkedWritesCrossingThreshold(t *testing.T) {
	handler := withGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		chunk := bytes.Repeat([]byte("x"), 300)
		_, _ = w.Write(chunk) // 300 bytes (buffered, < 512)
		_, _ = w.Write(chunk) // 300 bytes (cumulatively 600 bytes >= 512, triggers gzip)
	}))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := clientWithoutDecompression()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", resp.Header.Get("Content-Encoding"))
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gz.Close()

	decompressed, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	if len(decompressed) != 600 {
		t.Errorf("decompressed length = %d, want 600", len(decompressed))
	}
}

// TestGzipPreEncodedContentBypassesCompression verifies that if Content-Encoding
// is already set by a handler, withGzip does not double-compress.
func TestGzipPreEncodedContentBypassesCompression(t *testing.T) {
	handler := withGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "deflate")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("already compressed"), 50))
	}))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := clientWithoutDecompression()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Encoding"); got != "deflate" {
		t.Errorf("Content-Encoding = %q, want deflate", got)
	}
}

// TestGzipNoContentStatus verifies 204 No Content sends no body and no gzip encoding.
func TestGzipNoContentStatus(t *testing.T) {
	handler := withGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := clientWithoutDecompression()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" {
		t.Errorf("Content-Encoding = %q, want empty for 204", enc)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Errorf("body length = %d, want 0", len(body))
	}
}

// TestGzipPreservesExistingVary verifies that existing Vary headers are preserved.
func TestGzipPreservesExistingVary(t *testing.T) {
	handler := withGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "Origin")
		_, _ = w.Write([]byte("hello"))
	}))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := clientWithoutDecompression()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	varyVals := resp.Header.Values("Vary")
	joined := strings.Join(varyVals, ", ")
	if !strings.Contains(joined, "Origin") {
		t.Errorf("Vary %q missing Origin", joined)
	}
	if !strings.Contains(joined, "Accept-Encoding") {
		t.Errorf("Vary %q missing Accept-Encoding", joined)
	}
}
