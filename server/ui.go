package server

import (
	"embed"
	"net/http"
)

// uiFS holds the single-page UI. It is embedded so the binary is the whole
// deployment: no build step, no asset pipeline, no separately-served static
// directory that can drift out of sync with the API it talks to.
//
//go:embed index.html
var uiFS embed.FS

func uiHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		body, err := uiFS.ReadFile("index.html")
		if err != nil {
			http.Error(w, "ui unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	})
}
