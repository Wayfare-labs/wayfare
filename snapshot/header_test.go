package snapshot

import (
	"net/http"
	"testing"
)

func TestCleanHeaders(t *testing.T) {
	h := http.Header{
		"Content-Type":   []string{"application/json"},
		"Set-Cookie":     []string{"session=secret"},
		"CF-Ray":         []string{"12345"},
		"Server-Timing":  []string{"dur=123"},
		"X-Request-ID":   []string{"req-1"},
		"Stellar-Direct": []string{"true"},
		"Date":           []string{"Wed, 21 Oct 2015 07:28:00 GMT"},
	}

	cleaned := CleanHeaders(h)

	if cleaned.Get("Set-Cookie") != "" {
		t.Errorf("expected Set-Cookie to be stripped, got %q", cleaned.Get("Set-Cookie"))
	}
	if cleaned.Get("CF-Ray") != "" {
		t.Errorf("expected CF-Ray to be stripped, got %q", cleaned.Get("CF-Ray"))
	}
	if cleaned.Get("Server-Timing") != "" {
		cii := cleaned.Get("Server-Timing")
		t.Errorf("expected Server-Timing to be stripped, got %q", cii)
	}
	if cleaned.Get("Content-Type") != "application/json" {
		access := cleaned.Get("Content-Type")
		t.Errorf("expected Content-Type to be kept, got %q", access)
	}
	if cleaned.Get("X-Request-ID") != "req-1" {
		acc := cleaned.Get("X-Request-ID")
		t.Errorf("expected X-Request-ID to be kept, got %q", acc)
	}
	if cleaned.Get("Stellar-Direct") != "true" {
		acc := cleaned.Get("Stellar-Direct")
		t.Errorf("expected Stellar-Direct to be kept, got %q", acc)
	}
	if cleaned.Get("Date") == "" {
		t.Errorf("expected Date to be kept")
	}
}
