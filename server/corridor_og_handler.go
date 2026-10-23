package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// handleCorridorImage serves a corridor-specific social preview SVG generated
// from the most recent stored measurement for the requested corridor. When no
// stored run exists the endpoint returns a 404 rather than a generic logo: the
// request names a specific corridor and the response should represent one.
func (s *Server) handleCorridorImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, codeMethodNotAllowed, "only GET is supported")
		return
	}
	if err := checkParams(r, "from", "to", "pretty"); err != nil {
		writeError(w, r, http.StatusBadRequest, codeInvalidQuery, err.Error())
		return
	}

	from := param(r, "from", "USDC")
	to := param(r, "to", "NGNC")

	sendAsset, ok := asset.Lookup(from)
	if !ok {
		writeError(w, r, http.StatusBadRequest, codeUnknownSendAsset, fmt.Sprintf(
			"unknown send asset %q; verified assets are %s",
			from, assetCodesCommaSeparated()))
		return
	}
	recvAsset, ok := asset.Lookup(to)
	if !ok {
		writeError(w, r, http.StatusBadRequest, codeUnknownReceiveAsset, fmt.Sprintf(
			"unknown receive asset %q; verified assets are %s",
			to, assetCodesCommaSeparated()))
		return
	}

	doc, ok := s.corridorDocumentForImage(r.Context(), sendAsset, recvAsset)
	if !ok {
		writeError(w, r, http.StatusNotFound, "no_corridor_data", fmt.Sprintf(
			"no recorded measurements for %s to %s yet", from, to))
		return
	}

	img := route.NewCorridorImage(doc, time.Now().UTC())
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60, stale-while-revalidate=300")
	_, _ = w.Write([]byte(img.SVG()))
}

// corridorDocumentForImage returns a corridor snapshot document suitable for an
// image, preferring the most recent stored reading for the requested corridor.
// It returns false when the server has no data for that corridor at all.
func (s *Server) corridorDocumentForImage(ctx context.Context, send, recv asset.Asset) (route.CorridorJSON, bool) {
	if s.Store == nil {
		return route.CorridorJSON{}, false
	}
	key := runstore.CorridorKey(send.Code, recv.Code)
	rec, err := s.Store.Latest(ctx, key)
	if err != nil || rec == nil {
		return route.CorridorJSON{}, false
	}
	stale := staleJSON(rec, pegPair(send, recv), time.Now().UTC())
	if stale.MeasuredAt == "" {
		return route.CorridorJSON{}, false
	}
	return stale, true
}

func pegPair(send, recv asset.Asset) string {
	base, _ := asset.FiatPeg(send)
	quote, ok := asset.FiatPeg(recv)
	if !ok {
		quote = "UNKNOWN"
	}
	return base + "/" + quote
}

// assetCodesCommaSeparated renders the verified asset codes as a comma-separated
// list for use in error messages.
func assetCodesCommaSeparated() string {
	return strings.Join(asset.KnownCodes(), ", ")
}
