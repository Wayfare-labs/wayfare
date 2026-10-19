package server

import (
	"embed"
	"net/http"
)

// uiFS holds the UI: the page, its stylesheet and its script. It is embedded
// so the binary is the whole deployment: no build step, no asset pipeline, no
// separately-served static directory that can drift out of sync with the API
// it talks to.
//
// The UI is three files rather than one because they change for different
// reasons and are reviewed by different people. What was never the point is
// the single-file count itself; the invariants are no build step and one
// embedded binary, and both hold either way. Source-text tests read the same
// files through uiFS, so they target the split files directly.
//
//go:embed index.html app.css app.js
var uiFS embed.FS

// uiAssets maps a URL path to the embedded file that answers it and the
// content type that file must be served with. Only these paths are served;
// anything else 404s, as before.
var uiAssets = map[string]struct {
	file        string
	contentType string
}{
	"/":        {"index.html", "text/html; charset=utf-8"},
	"/app.css": {"app.css", "text/css; charset=utf-8"},
	"/app.js":  {"app.js", "text/javascript; charset=utf-8"},
}

func uiHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asset, ok := uiAssets[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		body, err := uiFS.ReadFile(asset.file)
		if err != nil {
			http.Error(w, "ui unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", asset.contentType)
		_, _ = w.Write(body)
	})
}
