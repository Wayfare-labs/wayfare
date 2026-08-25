package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// Wire shape for a corridor's stored history.
//
// These are explicit wire types for the same reason route.CorridorJSON
// exists: runstore.Record is a storage format — versioned, hash-sealed, with
// a field order that is part of the hash preimage — and marshalling it
// directly would make the API a function of how records happen to be stored
// on disk. The conversion below is the only place the two shapes meet.
//
// Runs come back oldest first: a timeline reads left to right. The store's
// own Recent() returns newest first because its callers want the latest
// record, so the handler reverses before converting.

const (
	// defaultTrendLimit is roughly twenty-five days of 6-hourly runs.
	defaultTrendLimit = 100
	// maxTrendLimit bounds one request's read, not the history itself.
	maxTrendLimit = 500
)

// TrendJSON is a corridor's stored history.
type TrendJSON struct {
	Corridor      string          `json:"corridor"`
	SendAsset     route.AssetJSON `json:"send_asset"`
	ReceiveAsset  route.AssetJSON `json:"receive_asset"`
	ReferencePair string          `json:"reference_pair"`

	// Count is len(Runs), carried separately so a reader can tell an empty
	// history from a failed request without inspecting the array.
	Count int `json:"count"`

	// Runs is oldest first. An array, never null, so a client can iterate
	// it without a nil check when the history is empty.
	Runs []TrendRunJSON `json:"runs"`
}

// TrendRunJSON is one stored measurement, reduced to what a trend needs:
// the state (integrity, depends_on), the headline figures, the reference the
// figures were scored against, and enough of each rung to plot loss by size.
// Hash linkage and full rung detail stay in storage.
type TrendRunJSON struct {
	Seq        int64  `json:"seq"`
	RecordedAt string `json:"recorded_at"`

	Integrity string   `json:"integrity"`
	DependsOn []string `json:"depends_on"`

	Reference TrendRefJSON `json:"reference"`

	FloorLossPct string `json:"floor_loss_pct"`
	FloorSize    string `json:"floor_size"`
	WorstLossPct string `json:"worst_loss_pct"`
	WorstSize    string `json:"worst_size"`

	// RecommendedSize is empty when no size produced an acceptable route,
	// which is the normal shape of a broken corridor and must survive into
	// the history rather than being promoted away.
	RecommendedSize string `json:"recommended_size,omitempty"`

	Finding string          `json:"finding"`
	Rungs   []TrendRungJSON `json:"rungs"`
}

// TrendRefJSON is the reference rate a run was scored against.
//
// Both mids are carried when both providers answered, exactly as storage
// does: a trend whose benchmark changed mid-series is a different question
// from a trend whose corridor moved, and the fields that tell them apart
// are source, secondary_source, divergence_pct and scored_against.
type TrendRefJSON struct {
	Mid    string `json:"mid"`
	Source string `json:"source"`
	AsOf   string `json:"as_of,omitempty"`

	SecondaryMid    string `json:"secondary_mid,omitempty"`
	SecondarySource string `json:"secondary_source,omitempty"`
	DivergencePct   string `json:"divergence_pct,omitempty"`

	// ScoredAgainst names which source produced the run's verdicts, so a
	// reader looking at the loss series can see when it was scored against
	// a different provider than the primary.
	ScoredAgainst string `json:"scored_against,omitempty"`
}

// TrendRungJSON is one size's stored result, reduced to what a trend plots.
type TrendRungJSON struct {
	SendAmount string `json:"send_amount"`
	Priced     bool   `json:"priced"`
	LossPct    string `json:"loss_pct,omitempty"`
	Verdict    string `json:"verdict,omitempty"`
}

// toTrendJSON renders stored records in the wire shape, preserving their
// stored (oldest first) order.
func toTrendJSON(recs []*runstore.Record, key, pair string, send, recv asset.Asset) TrendJSON {
	out := TrendJSON{
		Corridor:      key,
		SendAsset:     route.ToAssetJSON(send),
		ReceiveAsset:  route.ToAssetJSON(recv),
		ReferencePair: pair,
		Count:         len(recs),
		Runs:          make([]TrendRunJSON, 0, len(recs)),
	}
	for _, rec := range recs {
		out.Runs = append(out.Runs, toTrendRunJSON(rec))
	}
	return out
}

func toTrendRunJSON(rec *runstore.Record) TrendRunJSON {
	out := TrendRunJSON{
		Seq:        rec.Seq,
		RecordedAt: rec.RecordedAt.UTC().Format(time.RFC3339),
		Integrity:  rec.Integrity,
		DependsOn:  make([]string, 0, len(rec.DependsOn)),
		Reference: TrendRefJSON{
			Mid:             rec.Reference.Mid,
			Source:          rec.Reference.Source,
			AsOf:            rec.Reference.AsOf,
			SecondaryMid:    rec.Reference.SecondaryMid,
			SecondarySource: rec.Reference.SecondarySource,
			DivergencePct:   rec.Reference.DivergencePct,
			ScoredAgainst:   rec.Reference.ScoredAgainst,
		},
		FloorLossPct:    rec.FloorLossPct,
		FloorSize:       rec.FloorSize,
		WorstLossPct:    rec.WorstLossPct,
		WorstSize:       rec.WorstSize,
		RecommendedSize: rec.RecommendedSize,
		Finding:         rec.Finding,
		Rungs:           make([]TrendRungJSON, 0, len(rec.Rungs)),
	}
	out.DependsOn = append(out.DependsOn, rec.DependsOn...)
	for _, r := range rec.Rungs {
		out.Rungs = append(out.Rungs, TrendRungJSON{
			SendAmount: r.SendAmount,
			Priced:     r.Priced,
			LossPct:    r.LossPct,
			Verdict:    r.Verdict,
		})
	}
	return out
}

// handleTrend serves a corridor's stored history, oldest first.
//
// It is a read endpoint: it never measures and answers from storage alone.
// An empty history is a 200 with zero runs, not an error — a missing
// history is not a failure of the request, it is the answer, and a client
// that treated it as one would have to special-case the first day of
// deployment.
func (s *Server) handleTrend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "only GET is supported")
		return
	}

	from := param(r, "from", "USDC")
	to := param(r, "to", "NGNC")

	sendAsset, ok := asset.Lookup(from)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"unknown send asset %q; verified assets are %s",
			from, strings.Join(asset.KnownCodes(), ", ")))
		return
	}
	recvAsset, ok := asset.Lookup(to)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"unknown receive asset %q; verified assets are %s",
			to, strings.Join(asset.KnownCodes(), ", ")))
		return
	}
	if _, ok := asset.FiatPeg(recvAsset); !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"no verified fiat peg for %s, so there is no independent rate to score it against",
			recvAsset.Code))
		return
	}

	limit, err := parseTrendLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// The pair was fixed by the assets' pegs at measurement time, so the
	// stored runs and this document describe the same benchmark question.
	pegQuote, _ := asset.FiatPeg(recvAsset)
	pegBase, ok := asset.FiatPeg(sendAsset)
	if !ok {
		pegBase = "USD"
	}
	pair := pegBase + "/" + pegQuote
	key := runstore.CorridorKey(from, to)

	var recs []*runstore.Record
	if s.Store != nil {
		recent, err := s.Store.Recent(r.Context(), key, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "reading stored history: "+err.Error())
			return
		}
		// Recent is newest first because its callers want the latest
		// record; a timeline reads oldest first, so reverse.
		recs = make([]*runstore.Record, 0, len(recent))
		for i := len(recent) - 1; i >= 0; i-- {
			recs = append(recs, recent[i])
		}
	}

	writeJSON(w, http.StatusOK, toTrendJSON(recs, key, pair, sendAsset, recvAsset))
}

// parseTrendLimit bounds how much history one request may read.
//
// The value is clamped rather than rejected: asking for more history is not
// a client mistake worth a 400, and a history shorter than the limit simply
// comes back whole.
func parseTrendLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultTrendLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("bad limit %q: must be a whole number", raw)
	}
	if n <= 0 {
		return 0, fmt.Errorf("bad limit %q: must be positive", raw)
	}
	if n > maxTrendLimit {
		n = maxTrendLimit
	}
	return n, nil
}
