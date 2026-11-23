package server

import (
	"context"
	"net/http"
)

// httpGet is http.Get with an explicit context. The noctx linter rejects the
// context-free helpers so that no production request can outlive its caller;
// the tests route through this one function rather than suppressing it.
func httpGet(url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}
