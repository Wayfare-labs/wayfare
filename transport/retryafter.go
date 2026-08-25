package transport

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryAfter reads the Retry-After header, which providers are free not to
// send. A missing or unparseable value reports zero rather than a guess — an
// invented backoff is the kind of plausible-looking number this project
// refuses everywhere else.
//
// Both HTTP-date and delay-seconds forms are accepted, as the spec allows
// either.
func RetryAfter(resp *http.Response) time.Duration {
	v := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
