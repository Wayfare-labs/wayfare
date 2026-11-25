package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wayfare-labs/wayfare/runstore"
)

// feedEntry represents a transition entry for the Atom feed.
type feedEntry struct {
	Corridor   string
	Transition *runstore.IntegrityTransition
	RecordHash string // current record's hash for stable entry ID
}

// handleFeed serves /feed.atom listing integrity-state transitions across corridors.
func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, r, http.StatusMethodNotAllowed, codeMethodNotAllowed, "only GET and HEAD are supported")
		return
	}
	if err := checkParams(r, "pretty"); err != nil {
		writeError(w, r, http.StatusBadRequest, codeInvalidQuery, err.Error())
		return
	}

	if s.Store == nil {
		writeError(w, r, http.StatusNotFound, codeNoHistory, "no history store configured")
		return
	}

	ctx := r.Context()
	corridors, err := s.Store.Corridors(ctx)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, codeStoreRead, "listing stored corridors: "+err.Error())
		return
	}

	// Collect all transitions across all corridors
	var entries []feedEntry
	for _, corridor := range corridors {
		transitions, err := runstore.DetectTransitions(ctx, s.Store, corridor)
		if err != nil {
			// Log but continue - don't fail the whole feed for one corridor
			continue
		}
		for _, t := range transitions {
			// Get the current record's hash for stable entry ID
			rec, err := s.Store.Latest(ctx, corridor)
			if err != nil || rec == nil {
				continue
			}
			entries = append(entries, feedEntry{
				Corridor:   corridor,
				Transition: t,
				RecordHash: rec.Hash,
			})
		}
	}

	// Sort by current run time, newest first
	// (transitions are already in chronological order from DetectTransitions)
	// We want newest first for Atom feed
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].Transition.CurrentRunAt.Before(entries[j].Transition.CurrentRunAt) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Limit to 100 most recent transitions
	const maxFeedEntries = 100
	if len(entries) > maxFeedEntries {
		entries = entries[:maxFeedEntries]
	}

	// Generate Atom feed
	feed := generateAtomFeed(entries, s.clock())

	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(feed))
}

// generateAtomFeed creates an Atom 1.0 feed from transitions.
func generateAtomFeed(entries []feedEntry, now time.Time) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<feed xmlns=\"http://www.w3.org/2005/Atom\">\n  <title>Wayfare Corridor Integrity Transitions</title>\n  <link href=\"https://wayfare.example.com/feed.atom\" rel=\"self\"/>\n  <link href=\"https://wayfare.example.com/\"/>\n  <updated>" + now.UTC().Format(time.RFC3339) + "</updated>\n  <id>urn:uuid:wayfare-corridor-transitions</id>\n  <generator uri=\"https://github.com/Wayfare-labs/wayfare\" version=\"1.0\">Wayfare</generator>\n")

	for _, e := range entries {
		t := e.Transition
		// Stable entry ID from record hash and transition info
		entryID := fmt.Sprintf("urn:uuid:%s-%s-%s", e.RecordHash[:16], t.Corridor, t.CurrentRunAt.UTC().Format("20060102T150405Z"))

		title := fmt.Sprintf("%s: %s \u2192 %s", t.Corridor, t.PreviousIntegrity, t.CurrentIntegrity)
		if t.TransitionType == runstore.TransitionDependsOnChanged {
			title = fmt.Sprintf("%s: depends_on changed (%s)", t.Corridor, strings.Join(t.CurrentDependsOn, ", "))
		}

		content := fmt.Sprintf(
			"Corridor %s transitioned from %s to %s at %s. Previous run: %s, Current run: %s. Transition type: %s.",
			t.Corridor, t.PreviousIntegrity, t.CurrentIntegrity,
			t.CurrentRunAt.UTC().Format(time.RFC3339),
			t.PreviousRunAt.UTC().Format(time.RFC3339),
			t.CurrentRunAt.UTC().Format(time.RFC3339),
			t.TransitionType.String(),
		)

		fmt.Fprintf(&b, `  <entry>
    <title>%s</title>
    <link href="https://wayfare.example.com/api/v1/corridor?from=%s&to=%s"/>
    <id>%s</id>
    <updated>%s</updated>
    <summary>%s</summary>
    <content type="text">%s</content>
  </entry>
`,
			escapeXML(title),
			escapeXML(corridorSend(t.Corridor)),
			escapeXML(corridorReceive(t.Corridor)),
			escapeXML(entryID),
			escapeXML(t.CurrentRunAt.UTC().Format(time.RFC3339)),
			escapeXML(content),
			escapeXML(content),
		)
	}

	b.WriteString("</feed>\n")
	return b.String()
}

// escapeXML escapes special XML characters.
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&"+"amp;")
	s = strings.ReplaceAll(s, "<", "&"+"lt;")
	s = strings.ReplaceAll(s, ">", "&"+"gt;")
	s = strings.ReplaceAll(s, "\"", "&"+"quot;")
	s = strings.ReplaceAll(s, "'", "&"+"apos;")
	return s
}

// corridorSend extracts the send asset from a corridor key like "USDC-NGNC".
func corridorSend(corridor string) string {
	parts := strings.SplitN(corridor, "-", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// corridorReceive extracts the receive asset from a corridor key like "USDC-NGNC".
func corridorReceive(corridor string) string {
	parts := strings.SplitN(corridor, "-", 2)
	if len(parts) > 1 {
		return parts[1]
	}
	return ""
}
