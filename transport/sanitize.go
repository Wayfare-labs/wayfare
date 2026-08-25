// Package transport provides helpers for safe HTTP transport operations.
package transport

import (
	"errors"
	"net/url"
)

// SanitizeTransportError strips credentials and query parameters from
// transport-level errors before they are logged.
//
// http.Client.Do wraps request URLs in *url.Error when a transport-level
// failure occurs (DNS, TLS, connection refused, etc.). If the URL contains
// query-parameter credentials — API keys, bearer tokens, or similar — the
// error message leaks them into logs. This helper parses the URL, removes
// the query string, and reconstructs a clean error that preserves the
// operation and the underlying cause without the sensitive data.
//
// Non-transport errors pass through unchanged.
func SanitizeTransportError(err error) error {
	if err == nil {
		return nil
	}

	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}

	parsed, parseErr := url.Parse(urlErr.URL)
	if parseErr != nil {
		return &url.Error{
			Op:  urlErr.Op,
			URL: "<redacted>",
			Err: urlErr.Err,
		}
	}

	clean := &url.URL{
		Scheme: parsed.Scheme,
		Host:   parsed.Host,
		Path:   parsed.Path,
	}

	return &url.Error{
		Op:  urlErr.Op,
		URL: clean.String(),
		Err: urlErr.Err,
	}
}
