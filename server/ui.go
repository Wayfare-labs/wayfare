package server

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
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
var (
	uiContentSecurityPolicy string
	cspOnce                 sync.Once
)

// computeScriptHashes parses HTML bytes and computes the base64-encoded SHA-256
// hashes of the contents of all inline <script>...</script> blocks.
func computeScriptHashes(html []byte) []string {
	var hashes []string
	const openTag = "<script"
	const closeTag = "</script>"
	idx := 0
	for {
		start := bytes.Index(html[idx:], []byte(openTag))
		if start == -1 {
			break
		}
		start += idx
		tagEnd := bytes.IndexByte(html[start:], '>')
		if tagEnd == -1 {
			break
		}
		bodyStart := start + tagEnd + 1
		bodyEnd := bytes.Index(html[bodyStart:], []byte(closeTag))
		if bodyEnd == -1 {
			break
		}
		content := html[bodyStart : bodyStart+bodyEnd]
		if len(bytes.TrimSpace(content)) > 0 {
			h := sha256.Sum256(content)
			hashes = append(hashes, fmt.Sprintf("'sha256-%s'", base64.StdEncoding.EncodeToString(h[:])))
		}
		idx = bodyStart + bodyEnd + len(closeTag)
	}
	return hashes
}

// uiCSP returns the Content-Security-Policy header for the UI.
//
// Allows only 'self', with SHA-256 hashes for the inline scripts in index.html
// (theme initialization and application code) and frame-ancestors 'none' to
// prevent clickjacking (issue #611).
func uiCSP() string {
	cspOnce.Do(func() {
		body, err := uiFS.ReadFile("index.html")
		if err != nil {
			uiContentSecurityPolicy = "default-src 'self'; frame-ancestors 'none'"
			return
		}
		hashes := computeScriptHashes(body)
		scriptSrc := "'self'"
		if len(hashes) > 0 {
			scriptSrc += " " + strings.Join(hashes, " ")
		}
		uiContentSecurityPolicy = fmt.Sprintf(
			"default-src 'self'; script-src %s; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
			scriptSrc,
		)
	})
	return uiContentSecurityPolicy
}

func uiHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asset, ok := uiAssets[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		body, err := uiFS.ReadFile(asset.file)
		if r.URL.Path != "/" {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, r, http.StatusMethodNotAllowed, codeMethodNotAllowed, "only GET and HEAD are supported")
			return
		}
		body, err := uiFS.ReadFile("index.html")
		if err != nil {
			http.Error(w, "ui unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", asset.contentType)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", uiCSP())
		_, _ = w.Write(body)
	})
}
