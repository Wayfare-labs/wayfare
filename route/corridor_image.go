// Package route renders a corridor's measurement into a static social-preview
// SVG. The image is generated entirely from the measured document — there is
// no live lookup, no template rendering on the browser, and no dependency on a
// graphic binary. It is meant to be served by the monitor as a corridor's
// Open Graph image so a shared link shows the corridor's name, integrity
// state, floor/worst figures and the recorded time the numbers were taken.
package route

import (
	"fmt"
	"strings"
	"time"
)

// CorridorImage carries the fields the renderer needs from a measured document.
type CorridorImage struct {
	// Title is the corridor label shown at the top of the image.
	Title string

	// Integrity is the structural state badge text.
	Integrity string

	// DependsOn is rendered as a small dependency line when non-empty.
	DependsOn []string

	// Floor is the floor loss percentage, rendered as a fixed-point string.
	Floor string

	// FloorSize is the size the floor was measured at.
	FloorSize string

	// WorstLoss is the worst-case loss percentage.
	WorstLoss string

	// WorstSize is the size the worst loss was measured at.
	WorstSize string

	// ReferenceMid is the benchmark mid used for the measurement.
	ReferenceMid string

	// ReferenceSource is the benchmark source name.
	ReferenceSource string

	// RecordedAt is the UTC timestamp the reading was taken / recorded.
	RecordedAt string
}

// SVG renders the corridor snapshot as an inline SVG document sized for a
// social preview. It is plain UTF-8 SVG with no external references.
func (c CorridorImage) SVG() string {
	parts := []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630" viewBox="0 0 1200 630">`,
		`<defs>`,
		`<linearGradient id="bg" x1="0" y1="0" x2="1" y2="1">`,
		`<stop offset="0" stop-color="#0F172A"/>`,
		`<stop offset="1" stop-color="#1E293B"/>`,
		`</linearGradient>`,
		`</defs>`,
		`<rect width="1200" height="630" fill="url(#bg)"/>`,

		label(60, 96, "WAYFARE — CORRIDOR SNAPSHOT", "#F8FAFC", 30, "700", 1),
		label(60, 138, esc(c.Title), "#94A3B8", 22, "", 1), big(60, 192, esc(c.Integrity), "#10B981", 88),
		badge(60, 218, 320, 54, esc(c.Integrity), "#10B981", 30),

		label(60, 312, "FLOOR LOSS", "#F59E0B", 22, "", 1),
		big(60, 348, esc(c.Floor)+"%", "#F59E0B", 44),
		label(470, 348, "at "+esc(c.FloorSize)+" send", "#CBD5E1", 24, "", 0),

		label(60, 408, "WORST LOSS", "#F97316", 22, "", 1),
		big(60, 444, esc(c.WorstLoss)+"%", "#F97316", 44),
		label(470, 444, "at "+esc(c.WorstSize)+" send", "#CBD5E1", 24, "", 0),

		label(60, 502, "REFERENCE", "#F8FAFC", 22, "", 1),
		big(60, 538, esc(c.ReferenceMid), "#F8FAFC", 30),
		label(60, 572, esc(c.ReferenceSource), "#CBD5E1", 24, "", 0),
	}
	if len(c.DependsOn) > 0 {
		parts = append(parts, label(60, 610, "depends on "+strings.Join(c.DependsOn, ", "), "#94A3B8", 22, "", 1))
	}
	parts = append(parts, footer(60, 610, recordedOrUnavailable(c.RecordedAt), "#94A3B8", 22))
	return strings.Join(parts, "\n") + "\n</svg>"
}

func label(x, y int, text, color string, size float64, weight string, ls float64) string {
	return fmt.Sprintf(
		`<text x="%d" y="%d" font-family="ui-monospace, Menlo, Consolas, monospace" font-size="%g" font-weight="%s" letter-spacing="%g" fill="%s">%s</text>`,
		x, y, size, weight, ls, color, text)
}

func big(x, y int, text, color string, size float64) string {
	return label(x, y, text, color, size, "700", 1)
}

func badge(x, y, w, h int, text, color string, size float64) string {
	cy := y + h/2 + int((size*0.35)+2)
	return strings.Join([]string{
		fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" rx="10" fill="%s" fill-opacity="0.14"/>`, x, y, w, h, color),
		label(x+24, cy, text, color, size, "700", 1),
	}, "\n")
}

func footer(x, y int, text, color string, size float64) string {
	return label(x, y, text, color, size, "", 1)
}

func recordedOrUnavailable(s string) string {
	if s == "" {
		return "recorded time unavailable"
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		month := t.Month().String()[:3]
		return fmt.Sprintf("recorded %s %02d %02d:%02d UTC", month, t.Day(), t.Hour(), t.Minute())
	}
	return s
}

func esc(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	).Replace(s)
}

// NewCorridorImage builds an image descriptor from a measured corridor
// document and the UTC time the snapshot was generated for the image.
func NewCorridorImage(d CorridorJSON, _ time.Time) CorridorImage {
	depends := make([]string, 0, len(d.DependsOn))
	for _, a := range d.DependsOn {
		if a.Code != "" {
			depends = append(depends, a.Code)
		}
	}

	title := ""
	if d.SendAsset.Code != "" && d.ReceiveAsset.Code != "" {
		title = d.SendAsset.Code + " → " + d.ReceiveAsset.Code
		if peg := d.ReceiveAsset.Peg; peg != "" {
			title += " (" + peg + ")"
		}
	}

	return CorridorImage{
		Title:           title,
		Integrity:       d.Integrity,
		DependsOn:       depends,
		Floor:           d.Floor,
		FloorSize:       d.FloorSize,
		WorstLoss:       d.WorstLoss,
		WorstSize:       d.WorstSize,
		ReferenceMid:    d.ReferenceMid,
		ReferenceSource: d.ReferenceSource,
		RecordedAt:      recordedTime(d),
	}
}

// recordedTime chooses the recorded timestamp to show in the image. Exact
// provenance is the point of the image, so a stale envelope is preferred, and a
// wall-clock generated_at is used only as an honest fallback when the document
// carries no recording time at all.
func recordedTime(d CorridorJSON) string {
	if d.Stale != nil && d.Stale.RecordedAt != "" {
		return d.Stale.RecordedAt
	}
	if d.MeasuredAt != "" {
		return d.MeasuredAt
	}
	return ""
}
