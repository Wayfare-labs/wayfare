package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"
)

// uiFS holds the single-page UI. It is embedded so the binary is the whole
// deployment: no build step, no asset pipeline, no separately-served static
// directory that can drift out of sync with the API it talks to.
//
//go:embed index.html
var uiFS embed.FS

// defaultCorridorKey is the corridor the page opens on (the API's default
// from/to pair). The <noscript> summary is rendered for it, so text browsers
// and crawlers see the same history-first record the API would serve.
const defaultCorridorKey = "USDC-NGNC"

// uiSummary is the data the <noscript> block renders (issue #636). Every
// value is already in the record's stored form — nothing here derives,
// rounds, or fills an absent figure; an empty struct renders the explicit
// no-history message instead.
type uiSummary struct {
	Corridor     string
	Integrity    string
	FloorLossPct string
	WorstLossPct string
	RecordedAt   string
	Age          string
}

// uiPageData is the template data for the served page.
type uiPageData struct {
	Summary *uiSummary
}

var uiTemplate = template.Must(template.ParseFS(uiFS, "index.html"))

// summaryFor renders the noscript summary from the latest stored record for
// the default corridor, or nil when there is no store or no history yet.
func (s *Server) summaryFor(ctx context.Context) *uiSummary {
	if s.Store == nil {
		return nil
	}
	rec, err := s.Store.Latest(ctx, defaultCorridorKey)
	if err != nil || rec == nil {
		return nil
	}
	age := time.Since(rec.RecordedAt)
	if age < 0 {
		age = 0
	}
	return &uiSummary{
		Corridor:     defaultCorridorKey,
		Integrity:    rec.Integrity,
		FloorLossPct: rec.FloorLossPct,
		WorstLossPct: rec.WorstLossPct,
		RecordedAt:   rec.RecordedAt.UTC().Format(time.RFC3339),
		Age:          uiHumanAge(age),
	}
}

// uiHumanAge renders a duration the way the stored reading is described in
// prose: one unit, coarse enough to read at a glance.
func uiHumanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		return uiPlural(m, "minute")
	case d < 48*time.Hour:
		h := int(d.Hours())
		return uiPlural(h, "hour")
	default:
		days := int(d.Hours() / 24)
		return uiPlural(days, "day")
	}
}

func uiPlural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s ago", unit)
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}

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

// uiHandler serves the single-page UI, rendering the <noscript> summary from
// the history-first record when one exists. A template failure falls back to
// the raw embedded page: the UI must never 500 because a summary field
// could not be rendered. Security headers from issue #611 (CSP, X-Frame,
// nosniff, no-referrer) are applied to every success path.
func (s *Server) uiHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, r, http.StatusMethodNotAllowed, codeMethodNotAllowed, "only GET and HEAD are supported")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", uiCSP())

		// Render the page with the server-side summary (noscript fallback
		// and head metadata, #635/#636). If rendering fails, serve the raw
		// page rather than an error: it is still a working UI.
		data := uiPageData{Summary: s.summaryFor(r.Context())}
		var buf bytes.Buffer
		if err := uiTemplate.Execute(&buf, data); err != nil {
			body, err := uiFS.ReadFile("index.html")
			if err != nil {
				http.Error(w, "ui unavailable", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(body)
			return
		}
		_, _ = w.Write(buf.Bytes())
	})
}

// noscriptSummary extracts the rendered <noscript> block from a page body.
// Used by tests to assert on what a reader without JavaScript sees.
func noscriptSummary(page string) string {
	start := strings.Index(page, "<noscript>")
	end := strings.Index(page, "</noscript>")
	if start < 0 || end < 0 || end < start {
		return ""
	}
	return page[start : end+len("</noscript>")]
}
