package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// BadgeJSON is the response shape for a badge request.
type BadgeJSON struct {
	Corridor     string `json:"corridor"`
	Integrity    string `json:"integrity"`
	FloorLossPct string `json:"floor_loss_pct"`
	AgeHuman     string `json:"age_human"`
	RecordedAt   string `json:"recorded_at"`
}

// handleBadge serves /badge/{corridor}.svg showing integrity and floor loss.
func (s *Server) handleBadge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, r, http.StatusMethodNotAllowed, codeMethodNotAllowed, "only GET and HEAD are supported")
		return
	}
	if err := checkParams(r, "pretty"); err != nil {
		writeError(w, r, http.StatusBadRequest, codeInvalidQuery, err.Error())
		return
	}

	// Extract corridor from path: /badge/USDC-NGNC.svg
	path := strings.TrimPrefix(r.URL.Path, "/badge/")
	path = strings.TrimSuffix(path, ".svg")
	if path == "" {
		writeError(w, r, http.StatusBadRequest, codeInvalidQuery, "missing corridor in path")
		return
	}

	corridor := strings.ToUpper(path)
	if s.Store == nil {
		writeError(w, r, http.StatusNotFound, codeNoHistory, "no history store configured")
		return
	}

	ctx := r.Context()
	rec, err := s.Store.Latest(ctx, corridor)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, codeStoreRead, "reading stored corridor: "+err.Error())
		return
	}
	if rec == nil {
		writeError(w, r, http.StatusNotFound, codeNoHistory, fmt.Sprintf("no stored measurement for corridor %s", corridor))
		return
	}

	// Never trigger a live measurement - only serve from stored data
	now := time.Now().UTC()
	age := now.Sub(rec.RecordedAt.UTC())
	if age < 0 {
		age = 0
	}

	// Determine badge color based on integrity
	var color, label string
	switch rec.Integrity {
	case "DIRECT":
		color = "#2e7d32" // green
		label = "DIRECT"
	case "DERIVATIVE":
		color = "#f57c00" // orange
		label = "DERIVATIVE"
	case "NO-MARKET":
		color = "#c62828" // red
		label = "NO-MARKET"
	case "UNKNOWN":
		color = "#757575" // grey
		label = "UNKNOWN"
	default:
		color = "#757575"
		label = rec.Integrity
	}

	// Floor loss text
	floorText := rec.FloorLossPct + "%"
	if rec.FloorLossPct == "" || rec.FloorLossPct == "0.00" {
		floorText = "no data"
	}

	// Age text
	ageHuman := humanAge(age)

	// Generate SVG
	svg := generateBadgeSVG(corridor, label, floorText, ageHuman, color)

	// Cache for 5 minutes (300 seconds) - badge is served from recorded data only
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(svg))
}

// generateBadgeSVG creates a deterministic SVG badge.
// Format: shields.io style flat badge.
func generateBadgeSVG(corridor, integrity, floorLoss, age, color string) string {
	// shields.io style: left label (corridor), right value (integrity + floor loss + age)
	leftText := "wayfare: " + corridor
	rightText := integrity + " • " + floorLoss + " • " + age

	// Calculate widths (approximate character widths for font-family: system-ui, sans-serif)
	leftWidth := len(leftText)*7 + 10 // padding
	rightWidth := len(rightText)*7 + 10
	totalWidth := leftWidth + rightWidth

	// SVG template
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s">
  <linearGradient id="b" x2="0" y2="100%%">
    <stop offset="0" stop-color="#bbb" stop-opacity=".1"/>
    <stop offset="1" stop-opacity=".1"/>
  </linearGradient>
  <mask id="a">
    <rect width="%d" height="20" rx="3" fill="#fff"/>
  </mask>
  <g mask="url(#a)">
    <rect width="%d" height="20" fill="#555"/>
    <rect x="%d" width="%d" height="20" fill="%s"/>
    <rect width="%d" height="20" fill="url(#b)"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="system-ui, sans-serif" font-size="11" font-weight="600">
    <text x="%d" y="14" fill="#010101" fill-opacity=".3">%s</text>
    <text x="%d" y="13">%s</text>
    <text x="%d" y="14" fill="#010101" fill-opacity=".3">%s</text>
    <text x="%d" y="13">%s</text>
  </g>
</svg>`,
		totalWidth,
		leftText+": "+rightText,
		totalWidth,
		totalWidth,
		leftWidth,
		rightWidth,
		color,
		totalWidth,
		leftWidth/2,
		leftText,
		leftWidth/2,
		leftText,
		leftWidth+rightWidth/2,
		rightText,
		leftWidth+rightWidth/2,
		rightText,
	)
}
