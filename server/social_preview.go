package server

import (
	"fmt"
	"html"
	"net/http"
	"time"
)

func (s *Server) handleSocialPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, codeMethodNotAllowed, "only GET is supported")
		return
	}
	if err := checkParams(r, "from", "to"); err != nil {
		writeError(w, r, http.StatusBadRequest, codeInvalidQuery, err.Error())
		return
	}

	from, to, key, ok := previewCorridor(r)
	if !ok {
		writeError(w, r, http.StatusBadRequest, codeInvalidQuery, "unknown corridor asset")
		return
	}
	if s.Store == nil {
		http.NotFound(w, r)
		return
	}
	record, err := s.Store.Latest(r.Context(), key)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, codeStoreRead, "reading stored history: "+err.Error())
		return
	}
	if record == nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630" viewBox="0 0 1200 630" role="img" aria-labelledby="title description">
  <title id="title">%s corridor integrity snapshot</title>
  <desc id="description">Recorded %s integrity, floor loss %s percent at %s %s, on %s.</desc>
  <rect width="1200" height="630" fill="#f8fafc"/>
  <path d="M0 0h1200v18H0z" fill="#0f766e"/>
  <text x="72" y="82" fill="#475569" font-family="sans-serif" font-size="24" font-weight="700" letter-spacing="2">WAYFARE / CORRIDOR SNAPSHOT</text>
  <text x="72" y="178" fill="#0f172a" font-family="sans-serif" font-size="58" font-weight="700">%s → %s</text>
  <text x="72" y="280" fill="#475569" font-family="sans-serif" font-size="22" font-weight="700" letter-spacing="1.5">INTEGRITY</text>
  <text x="72" y="350" fill="#0f172a" font-family="sans-serif" font-size="50" font-weight="700">%s</text>
  <text x="625" y="280" fill="#475569" font-family="sans-serif" font-size="22" font-weight="700" letter-spacing="1.5">FLOOR LOSS</text>
  <text x="625" y="350" fill="#0f172a" font-family="sans-serif" font-size="50" font-weight="700">%s%%</text>
  <text x="625" y="404" fill="#475569" font-family="sans-serif" font-size="24">at %s %s</text>
  <path d="M72 466h1056" stroke="#cbd5e1" stroke-width="2"/>
  <text x="72" y="520" fill="#475569" font-family="sans-serif" font-size="20" font-weight="700" letter-spacing="1.5">RECORDED AT</text>
  <text x="72" y="568" fill="#0f172a" font-family="monospace" font-size="26">%s</text>
</svg>`,
		html.EscapeString(key), html.EscapeString(record.Integrity), html.EscapeString(record.FloorLossPct),
		html.EscapeString(record.FloorSize), html.EscapeString(from), html.EscapeString(record.RecordedAt.UTC().Format(time.RFC3339Nano)),
		html.EscapeString(from), html.EscapeString(to), html.EscapeString(record.Integrity),
		html.EscapeString(record.FloorLossPct), html.EscapeString(record.FloorSize), html.EscapeString(from),
		html.EscapeString(record.RecordedAt.UTC().Format(time.RFC3339Nano)))
}
