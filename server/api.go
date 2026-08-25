// Package api serves the corridor monitor over HTTP.
//
// The wire shape for a measured corridor lives in package route
// (route.CorridorJSON / route.ToCorridorJSON) so that this HTTP handler and
// cmd/ladder's -json mode emit identical JSON from the same conversion code.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// Server serves the monitor API and its UI.
type Server struct {
	Engine *route.Engine

	// Store is the measurement history. Optional: with none, a failed live
	// measurement is an error, exactly as it was before history existed.
	Store runstore.Store

	// Checks runs counterparty checks alongside a measurement. Nil disables
	// them, and the response then carries no findings block at all — absent
	// and empty must not look the same, since one means "not checked" and
	// the other means "checked, nothing found".
	Checks *checks.Runner

	// HistoryFirst serves the most recent stored run instead of measuring,
	// unless the caller asks for a live reading with ?live=1.
	//
	// This is for deployments that cannot hold a request open long enough to
	// price a twelve-rung ladder — a serverless function, typically. The
	// honesty properties are unchanged either way: a stored reading is
	// labelled live:false and carries its age, and with no stored run the
	// request errors rather than inventing one. What changes is only which
	// is tried first.
	HistoryFirst bool

	// Timeout bounds a single corridor measurement. A full ladder is a
	// dozen round trips to Horizon, so this is generous by HTTP standards.
	Timeout time.Duration

	// Logger is the structured logger for request and upstream logging.
	// Nil means slog.Default().
	Logger *slog.Logger
}

func (s *Server) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *Server) timeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return 90 * time.Second
}

// Handler returns the routed handler for the whole service.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/corridor", s.handleCorridor)
	mux.HandleFunc("/api/assets", s.handleAssets)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.Handle("/", uiHandler())
	return mux
}

// handlers -------------------------------------------------------------------

func (s *Server) handleCorridor(w http.ResponseWriter, r *http.Request) {
	started := time.Now()

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

	pegQuote, ok := asset.FiatPeg(recvAsset)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"no verified fiat peg for %s, so there is no independent rate to score it against",
			recvAsset.Code))
		return
	}
	pegBase, ok := asset.FiatPeg(sendAsset)
	if !ok {
		pegBase = "USD"
	}

	sizes, err := parseSizes(r.URL.Query().Get("sizes"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// History-first: answer from the chain unless a live reading was asked
	// for. The stored document is the same shape, so a client needs no
	// special casing — see staleJSON.
	if s.HistoryFirst && r.URL.Query().Get("live") == "" {
		if stale, ok := s.staleFor(r.Context(), sendAsset.Code, recvAsset.Code,
			pegBase+"/"+pegQuote); ok {
			writeJSON(w, http.StatusOK, stale)
			return
		}
		// No history yet. Fall through and measure rather than erroring:
		// a first deploy with an empty chain should still answer.
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout())
	defer cancel()

	res, err := s.Engine.Ladder(ctx, route.LadderRequest{
		SendAsset:      sendAsset,
		ReceiveAsset:   recvAsset,
		Sizes:          sizes,
		ReferenceBase:  pegBase,
		ReferenceQuote: pegQuote,
	})
	// A ladder whose every rung errored is not a measurement, even though
	// Ladder itself returns no error: the figures would all be zero and the
	// body would look exactly like a corridor that priced at nothing. Treat
	// it as the failure it is.
	if err == nil && res.Failed() {
		err = fmt.Errorf("no size could be measured; every request failed to reach an upstream")
	}
	if err != nil {
		// A live measurement failed. If history exists, serve the most
		// recent stored run with an explicit stale envelope; if it does
		// not, error. Nothing is ever synthesised to fill the gap — this
		// is the one place a continuous monitor can quietly betray the
		// project, by returning a plausible number instead of admitting
		// it does not currently know.
		if stale, ok := s.staleFor(ctx, sendAsset.Code, recvAsset.Code, pegBase+"/"+pegQuote); ok {
			writeJSON(w, http.StatusOK, stale)
			return
		}
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		writeError(w, status, "measuring corridor: "+err.Error())
		return
	}

	out := route.ToCorridorJSON(res, pegBase+"/"+pegQuote)

	// Checks run after the measurement and cannot alter it. route.WithFindings
	// is the only composition point and branches on nothing — see the
	// composition rule in docs/checks.md.
	if s.Checks != nil {
		out = route.WithFindings(out, s.Checks.ForAsset(ctx, recvAsset))
	}

	writeJSON(w, http.StatusOK, out)

	// Request log: corridor, sizes, duration, and status. Upstream attribution
	// happens inside the engine; this boundary log attributes the overall
	// measurement to the HTTP request that asked for it.
	requestedLive := r.URL.Query().Get("live") != ""
	s.log().Info("corridor measured",
		"method", r.Method,
		"path", r.URL.Path,
		"from", from,
		"to", to,
		"sizes", r.URL.Query().Get("sizes"),
		"requested_live", requestedLive,
		"integrity", out.Integrity,
		"duration", time.Since(started).Round(time.Millisecond).String())
}

func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		route.AssetJSON
		Corridor bool `json:"can_be_destination"`
	}
	out := make([]entry, 0)
	for _, code := range asset.KnownCodes() {
		a, _ := asset.Lookup(code)
		_, hasPeg := asset.FiatPeg(a)
		out = append(out, entry{AssetJSON: route.ToAssetJSON(a), Corridor: hasPeg})
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": out})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// helpers --------------------------------------------------------------------

const maxSizes = 24

func param(r *http.Request, key, fallback string) string {
	if v := strings.TrimSpace(r.URL.Query().Get(key)); v != "" {
		return v
	}
	return fallback
}

func parseSizes(raw string) ([]decimal.Decimal, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > maxSizes {
		return nil, fmt.Errorf("too many sizes: %d requested, limit is %d", len(parts), maxSizes)
	}
	out := make([]decimal.Decimal, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		d, err := decimal.NewFromString(p)
		if err != nil {
			return nil, fmt.Errorf("bad size %q: not a number", p)
		}
		if !d.IsPositive() {
			return nil, fmt.Errorf("bad size %q: must be positive", p)
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// staleFor returns the most recent stored run for a corridor, labelled as
// stale, or false when there is no history to serve.
//
// The returned document is deliberately the same shape as a live one, with two
// differences a client cannot miss: live is false, and stale carries the age.
// Serving a different shape would mean every consumer needed two parsers, and
// the one that forgot would be the one that rendered a six-hour-old reading as
// current.
func (s *Server) staleFor(ctx context.Context, send, recv, pair string) (route.CorridorJSON, bool) {
	if s.Store == nil {
		return route.CorridorJSON{}, false
	}
	rec, err := s.Store.Latest(ctx, runstore.CorridorKey(send, recv))
	if err != nil || rec == nil {
		return route.CorridorJSON{}, false
	}
	return staleJSON(rec, pair, time.Now().UTC()), true
}

// staleJSON renders a stored record in the wire shape, marked not live.
func staleJSON(rec *runstore.Record, pair string, now time.Time) route.CorridorJSON {
	age := now.Sub(rec.RecordedAt.UTC())
	if age < 0 {
		age = 0
	}

	out := route.CorridorJSON{
		SendAsset:                assetFromCode(rec.Corridor, 0),
		ReceiveAsset:             assetFromCode(rec.Corridor, 1),
		Integrity:                rec.Integrity,
		DependsOn:                []route.AssetJSON{},
		ReferenceMid:             rec.Reference.Mid,
		ReferenceSource:          rec.Reference.Source,
		ReferencePair:            pair,
		ReferenceSecondaryMid:    rec.Reference.SecondaryMid,
		ReferenceSecondarySource: rec.Reference.SecondarySource,
		ReferenceDivergencePct:   rec.Reference.DivergencePct,
		Floor:                    rec.FloorLossPct,
		FloorSize:                rec.FloorSize,
		WorstLoss:                rec.WorstLossPct,
		WorstSize:                rec.WorstSize,
		RecommendedSize:          rec.RecommendedSize,
		Finding:                  rec.Finding,
		Rungs:                    make([]route.RungJSON, 0, len(rec.Rungs)),
		MeasuredAt:               rec.RecordedAt.UTC().Format(time.RFC3339),

		Live: false,
		Stale: &route.StaleJSON{
			RecordedAt: rec.RecordedAt.UTC().Format(time.RFC3339),
			AgeSeconds: int64(age.Seconds()),
			AgeHuman:   humanAge(age),
		},
	}

	for _, code := range rec.DependsOn {
		out.DependsOn = append(out.DependsOn, route.AssetJSON{Code: code})
	}
	for _, r := range rec.Rungs {
		rj := route.RungJSON{
			SendAmount: r.SendAmount,
			Priced:     r.Priced,
			Integrity:  r.Integrity,
			Notes:      []string{},
		}
		if r.Priced {
			rj.Quote = &route.QuoteJSON{
				Description:   r.Path,
				Source:        "stellar-dex",
				ReceiveAmount: r.ReceiveAmount,
				EffectiveRate: r.EffectiveRate,
				LossPct:       r.LossPct,
				Verdict:       r.Verdict,
				Warnings:      []string{},
			}
		}
		out.Rungs = append(out.Rungs, rj)
	}

	// A stored run's recommendation is carried exactly as recorded — null
	// stays null. Promoting a stored quote here would recreate, on the
	// stale path, the recommendation the monitor refused to make live.
	if rec.Recommended != nil {
		out.Recommended = &route.QuoteJSON{
			Description:   rec.Recommended.Path,
			Source:        "stellar-dex",
			ReceiveAmount: rec.Recommended.ReceiveAmount,
			EffectiveRate: rec.Recommended.EffectiveRate,
			LossPct:       rec.Recommended.LossPct,
			Verdict:       rec.Recommended.Verdict,
			Warnings:      []string{},
		}
	}
	return out
}

// assetFromCode splits a stored corridor key like "USDC-NGNC".
func assetFromCode(corridor string, idx int) route.AssetJSON {
	parts := strings.SplitN(corridor, "-", 2)
	if idx >= len(parts) {
		return route.AssetJSON{}
	}
	if a, ok := asset.Lookup(parts[idx]); ok {
		return route.ToAssetJSON(a)
	}
	return route.AssetJSON{Code: parts[idx]}
}

// humanAge renders a duration the way a reader thinks about staleness.
func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
