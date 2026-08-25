package transport

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestSanitizeTransportError_NilError(t *testing.T) {
	if got := SanitizeTransportError(nil); got != nil {
		t.Errorf("SanitizeTransportError(nil) = %v, want nil", got)
	}
}

func TestSanitizeTransportError_NonTransportError(t *testing.T) {
	orig := errors.New("something went wrong")
	got := SanitizeTransportError(orig)
	if got != orig {
		t.Errorf("non-transport error should pass through unchanged, got %v", got)
	}
}

func TestSanitizeTransportError_StripsQueryParams(t *testing.T) {
	orig := &url.Error{
		Op:  "Get",
		URL: "https://horizon.stellar.org/paths/strict-send?api_key=secret-token&source_amount=100",
		Err: errors.New("dial tcp: connection refused"),
	}

	got := SanitizeTransportError(orig)
	errStr := got.Error()

	if strings.Contains(errStr, "secret-token") {
		t.Errorf("credential leaked in sanitized error: %s", errStr)
	}
	if strings.Contains(errStr, "api_key") {
		t.Errorf("query parameter leaked in sanitized error: %s", errStr)
	}
	if !strings.Contains(errStr, "horizon.stellar.org") {
		t.Errorf("host should be preserved in sanitized error: %s", errStr)
	}
	if !strings.Contains(errStr, "Get") {
		t.Errorf("operation should be preserved in sanitized error: %s", errStr)
	}
}

func TestSanitizeTransportError_PreservesWrappedError(t *testing.T) {
	inner := errors.New("connection timeout")
	orig := &url.Error{
		Op:  "Post",
		URL: "https://api.example.com/v1/rates?key=supersecret",
		Err: inner,
	}

	got := SanitizeTransportError(orig)

	var urlErr *url.Error
	if !errors.As(got, &urlErr) {
		t.Fatalf("expected *url.Error, got %T", got)
	}
	if !errors.Is(urlErr.Err, inner) {
		t.Errorf("wrapped error should be preserved")
	}
}

func TestSanitizeTransportError_CleansUserInfo(t *testing.T) {
	orig := &url.Error{
		Op:  "Get",
		URL: "https://user:password@example.com/api/data?token=abc123",
		Err: errors.New("i/o timeout"),
	}

	got := SanitizeTransportError(orig)
	errStr := got.Error()

	if strings.Contains(errStr, "password") {
		t.Errorf("user info leaked in sanitized error: %s", errStr)
	}
	if strings.Contains(errStr, "abc123") {
		t.Errorf("query token leaked in sanitized error: %s", errStr)
	}
}
