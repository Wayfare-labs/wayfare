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
	"github.com/Wayfare-labs/wayfare/refrate"
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
}

// pkgLogger is the package-level logger for request and upstream logging.
// Set via SetLogger; nil means slog.Default().
var pkgLogger *slog.Logger

// SetLogger configures the package-level logger for the server package.
func SetLogger(l *slog.Logger) { pkgLogger = l }

func log() *slog.Logger {
	if pkgLogger != nil {
		return pkgLogger
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
	mux.HandleFunc("/api/corridor/trend", s.handleTrend)
	mux.HandleFunc("/api/assets", s.handleAssets)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.Handle("/", uiHandler())
	return withCORS(mux)
}

// withCORS makes the API callable from any origin, and records the policy
// rather than leaving it implicit (backlog #38 / issue #139).
//
// The decision is Access-Control-Allow-Origin: * because there is nothing
// here to protect: the API is public, keyless and read-only, and the
// corridor figures it serves are data this project exists to publish. A
// wildcard origin is only unsafe when a response can carry credentials,
// and this surface carries none — there is deliberately no
// Access-Control-Allow-Credentials header, so a browser cannot attach
// cookies or stored auth to a cross-origin request even if one existed.
// If the API ever grows a write path or credentials, this middleware is
// the single place that policy must change.
//
// Preflight (OPTIONS) is answered here so a client that adds custom
// headers can still call the API; the methods list is exactly what the
// mux supports.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			if reqHeaders := r.Header.Get("Access-Control-Request-Headers"); reqHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", reqHeaders)
			}
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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
			log().Info("corridor measured",
				"method", r.Method,
				"path", r.URL.Path,
				"from", from,
				"to", to,
				"sizes", r.URL.Query().Get("sizes"),
				"live", false,
				"integrity", stale.Integrity,
				"duration", time.Since(started).Round(time.Millisecond).String())
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
			log().Info("corridor measured",
				"method", r.Method,
				"path", r.URL.Path,
				"from", from,
				"to", to,
				"sizes", r.URL.Query().Get("sizes"),
				"live", false,
				"integrity", stale.Integrity,
				"duration", time.Since(started).Round(time.Millisecond).String())
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
	// actualLive tracks whether this response came from a live measurement
	// rather than a stale/history response. It is set after the measurement
	// succeeds, not derived from the query parameter.
	actualLive := true
	log().Info("corridor measured",
		"method", r.Method,
		"path", r.URL.Path,
		"from", from,
		"to", to,
		"sizes", r.URL.Query().Get("sizes"),
		"live", actualLive,
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
		// ReferenceAgreement and Scored are reconstructed from the stored
		// record rather than dropped. The record's reference block is the same
		// state refrate.reconcile produced, persisted: a secondary implies
		// two providers answered, and the recorded divergence and as-of
		// moments decide which agreement band they fell into. ScoredAgainst
		// records that verdicts were actually derived, so scored is faithful
		// to measurement time instead of defaulting to false. See
		// staleAgreement and staleScored.
		ReferenceAgreement: staleAgreement(&rec.Reference),
		Scored:             staleScored(&rec.Reference),
		// ReferenceFetchedAt is carried from the record so a reader can tell
		// how old the benchmark was when the reading was taken — recorded
		// from the live path since Version 3, and absent on an older record
		// that predates it, which is the honest "unknown" rather than a guess.
		ReferenceFetchedAt: rec.Reference.FetchedAt,
		Floor:              rec.FloorLossPct,
		FloorSize:          rec.FloorSize,
		WorstLoss:          rec.WorstLossPct,
		WorstSize:          rec.WorstSize,
		RecommendedSize:    rec.RecommendedSize,
		Finding:            rec.Finding,
		Rungs:              make([]route.RungJSON, 0, len(rec.Rungs)),
		MeasuredAt:         rec.RecordedAt.UTC().Format(time.RFC3339),

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

	// Findings are served back from storage, so a history-served corridor
	// shows the same counterparty facts the live one did. Absent when the
	// record carried none — a Version 1 record, or a live measurement taken
	// without a checks runner — which is the honest "not checked".
	if f := storedFindingsJSON(rec); f != nil {
		out.Findings = f
	}
	return out
}

// staleScored reconstructs the wire's scored bool from a stored record.
//
// A corridor is scored exactly when verdicts were derived against one of the
// reference mids, which the record captures as ScoredAgainst (empty when the
// rate was unscorable — see FromCorridorJSON's scoredAgainst). So faithfulness
// is checking whether a scored-against source was recorded, not reproducing the
// refrate band calculation: recording it is what made it scorable.
func staleScored(ref *runstore.Reference) bool {
	return ref.ScoredAgainst != ""
}

// staleAgreement reconstructs the wire's reference_agreement from a stored
// record the way refrate.reconcile originally classified it, rather than
// dropping it.
//
// The record's reference block is the state reconcile produced, persisted:
//   - No secondary implies only one provider answered: SINGLE.
//   - Otherwise the recorded DivergencePct and as-of moments reproduce the
//     band reconcile assigned at measurement time: beyond the malfunction
//     threshold is MALFUNCTION, as-of moments far apart in time is STALE, and
//     below the agree ceiling is AGREE, with DISAGREE between the two. This
//     is the same classification order refrate.reconcile used, so a corridor
//     whose providers agreed does not read back as scored:false, and one that
//     malformed reads back as MALFUNCTION rather than as a plausible band.
//   - Where the stored record genuinely cannot answer — a divergence that was
//     never recorded, or one that is not a number — the honest output is the
//     explicit UNKNOWN below rather than "" or a guessed band.
func staleAgreement(ref *runstore.Reference) string {
	if ref.SecondaryMid == "" && ref.SecondarySource == "" {
		// Only one provider answered: uncorroborated by construction.
		return refrate.AgreementSingle.String()
	}

	d, err := decimal.NewFromString(ref.DivergencePct)
	if err != nil {
		// Divergence was never recorded, or is not a number on this build.
		// Older records predate the cross-check, or the field is corrupted —
		// the stored record genuinely cannot answer, so an explicit UNKNOWN
		// is the honest output rather than "" or a guessed band.
		return "UNKNOWN"
	}

	// A divergence past the malfunction threshold is a broken feed, exactly
	// as reconcile judged it at measurement time.
	if d.GreaterThan(refrate.DivergenceMalfunction) {
		return refrate.AgreementMalfunction.String()
	}
	// Two providers answering quotes far enough apart in time that their
	// difference measured lag rather than disagreement is STALE, again
	// reconstructed from the stored as-of moments the way reconcile did.
	// Below the malfunction threshold and not stale, the band is decided by
	// whether the recorded divergence crosses the agreement ceiling.
	if asOfGap(ref) > refrate.StaleGap {
		return refrate.AgreementStale.String()
	}
	if d.GreaterThan(refrate.DivergenceAgree) {
		return refrate.AgreementDisagree.String()
	}
	return refrate.AgreementAgree.String()
}

// asOfGap returns how far apart two providers' as-of moments were, or zero
// when either stamp is absent or unreadable. A missing stamp is treated as no
// gap (not stale): the record that lacks the times cannot claim STALE on
// reconstruction, and the bordering divergences (AGREE/DISAGREE) still answer
// faithfully from the recorded numbers alone.
func asOfGap(ref *runstore.Reference) time.Duration {
	a, errA := time.Parse(time.RFC3339, ref.AsOf)
	b, errB := time.Parse(time.RFC3339, ref.SecondaryAsOf)
	if errA != nil || errB != nil {
		return 0
	}
	gap := a.Sub(b)
	if gap < 0 {
		gap = -gap
	}
	return gap
}

// storedFindingsJSON rebuilds the wire findings block from a stored record,
// or returns nil when the record carried no checks or metrics.
//
// The record stores the per-item CheckJSON/MetricJSON arrays; the summary
// counts and worst severity that appear on a live findings block are derived
// from those and must be recomputed here so a stale response matches the live
// shape field-for-field. Absence (no stored findings at all) stays absent —
// "not checked" must not read back as "checked, nothing found".
func storedFindingsJSON(rec *runstore.Record) *checks.FindingsJSON {
	if len(rec.Checks) == 0 && len(rec.Metrics) == 0 {
		return nil
	}
	f := &checks.FindingsJSON{
		Checks:  rec.Checks,
		Metrics: rec.Metrics,
	}
	worstRank, haveWorst := -1, false
	for _, c := range rec.Checks {
		switch {
		case !c.Determined:
			f.Undetermined++
		case c.Passed:
			f.Passed++
		default:
			f.Failed++
		}
		if c.Determined && !c.Passed {
			if rank := severityRank(c.Severity); !haveWorst || rank > worstRank {
				worstRank, haveWorst = rank, true
			}
		}
	}
	if haveWorst {
		f.WorstSeverity = severityName(worstRank)
	}
	return f
}

// severityRank maps a severity string to its ordering, lowest first.
// Unknown severities sort below everything and never become WorstSeverity
// unless nothing higher is present.
func severityRank(s string) int {
	switch s {
	case "critical":
		return 4
	case "warning":
		return 3
	case "notice":
		return 2
	case "info":
		return 1
	default:
		return -1
	}
}

func severityName(rank int) string {
	switch rank {
	case 4:
		return "critical"
	case 3:
		return "warning"
	case 2:
		return "notice"
	case 1:
		return "info"
	default:
		return ""
	}
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
