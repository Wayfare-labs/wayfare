package server

import (
	"net/http"
	"time"
)

// chainHeadJSON is the externally pinnable identity of one corridor's current
// stored chain tip. It intentionally publishes the existing record hash rather
// than adding a new commitment or changing the hash-pinned record format.
type chainHeadJSON struct {
	Corridor   string `json:"corridor"`
	Seq        int64  `json:"seq"`
	RecordedAt string `json:"recorded_at"`
	Hash       string `json:"hash"`
}

type chainHeadsJSON struct {
	Heads []chainHeadJSON `json:"heads"`
}

// handleChainHeads publishes the current hash-chain tip for every stored
// corridor, in stable corridor order, so independent readers can retain a pin.
func (s *Server) handleChainHeads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, codeMethodNotAllowed, "only GET is supported")
		return
	}
	if err := checkParams(r, "pretty"); err != nil {
		writeError(w, r, http.StatusBadRequest, codeInvalidQuery, err.Error())
		return
	}

	heads := chainHeadsJSON{Heads: make([]chainHeadJSON, 0)}
	if s.Store != nil {
		corridors, err := s.Store.Corridors(r.Context())
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "STORE_READ_ERROR", "listing stored corridors: "+err.Error())
			return
		}
		for _, corridor := range corridors {
			rec, err := s.Store.Latest(r.Context(), corridor)
			if err != nil {
				writeError(w, r, http.StatusInternalServerError, "STORE_READ_ERROR", "reading chain head for "+corridor+": "+err.Error())
				return
			}
			if rec == nil {
				writeError(w, r, http.StatusInternalServerError, "STORE_READ_ERROR", "store listed corridor "+corridor+" without a latest record")
				return
			}
			heads.Heads = append(heads.Heads, chainHeadJSON{
				Corridor:   corridor,
				Seq:        rec.Seq,
				RecordedAt: rec.RecordedAt.UTC().Format(time.RFC3339),
				Hash:       rec.Hash,
			})
		}
	}

	writeJSON(w, r, http.StatusOK, heads)
}
