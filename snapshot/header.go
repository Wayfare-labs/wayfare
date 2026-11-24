package snapshot

import (
	"net/http"
	"strings"
)

// allowedHeaderPrefixes defines prefixes of headers that are permitted in recorded snapshots.
var allowedHeaderPrefixes = []string{
	"content-type",
	"content-length",
	"cache-control",
	"etag",
	"stellar-",
	"x-ratelimit-",
}

// allowedExactHeaders defines exact header names that are permitted in recorded snapshots.
var allowedExactHeaders = map[string]bool{
	"date":         true,
	"retry-after":  true,
	"x-request-id": true,
}

// IsAllowedHeader reports whether a given header name is permitted to be stored in snapshots.
func IsAllowedHeader(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if allowedExactHeaders[lower] {
		return true
	}
	for _, prefix := range allowedHeaderPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// CleanHeaders filters an http.Header according to the allowed headers list.
func CleanHeaders(h http.Header) http.Header {
	if h == nil {
		return nil
	}
	cleaned := make(http.Header)
	for k, vals := range h {
		if IsAllowedHeader(k) {
			for _, v := range vals {
				cleaned.Add(k, v)
			}
		}
	}
	return cleaned
}
