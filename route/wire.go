package route

import (
	"time"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/checks"
)

// Wire types for a measured corridor. These are the single JSON contract
// shared by the HTTP API (server.Server) and cmd/ladder's -json mode. A second,
// independently-maintained shape for the same measurement is exactly the
// kind of drift this project exists to catch, so there is only one.
//
// Money crosses the wire as decimal strings. Serialising a rate as a JSON
// number invites a client to parse it into a float64, which is the same
// rounding bug this project refuses internally.

type QuoteJSON struct {
	// Kind is "dex" or "anchor-sep38" — see route.Kind. On-chain DEX
	// liquidity and an anchor's own RFQ rails are two different markets
	// that can price the same pair differently, and Source alone (a free
	// -form domain or "stellar-dex") makes a client re-derive that
	// distinction from a string it was never guaranteed to parse. Every
	// quote in a response is "dex" today — see route.KindAnchorSEP38 — but
	// the field is on the wire from the start so a caller never has to
	// guess which rail a figure came from, and the two are never silently
	// conflated into one number.
	Kind        string `json:"kind"`
	Description string `json:"description"`

	// Hops is the same path as structured data: one entry per asset, in
	// execution order, both endpoints included — [send, …intermediates,
	// receive]. Each entry carries that asset's identity as established:
	// code, issuer where known, peg where known (see AssetJSON).
	//
	// Description stays exactly as it was — display text, and the field
	// existing clients already read. Hops exists because that string
	// cannot be consumed: it names codes only, so a reader cannot tell
	// which project's NGNC a path touched, and asking a client to parse
	// prose (or guess issuers from codes) to answer that is the identity
	// error this project refuses everywhere else. Whether a route crosses
	// a particular issuer is the exact question the DERIVATIVE integrity
	// state answers and the route explorer exists to show; this is the
	// data that answers it without a parse (backlog #84, issue #100).
	//
	// An asset with no code to publish is skipped rather than emitted as
	// an empty placeholder — a hop must identify something to be a hop.
	// A hop whose issuing account could not be established carries
	// issuer_unknown rather than a silently missing field.
	//
	// Null only on a stale replay: runstore.Rung records the description
	// string and has no home for the hops (widening the record is a
	// schema migration with its own review bar), and rebuilding them from
	// Description would be exactly the prose parse this field exists to
	// make unnecessary. Null means "not recorded", never "no
	// intermediate assets".
	Hops []AssetJSON `json:"hops"`

	Source        string   `json:"source"`
	ReceiveAmount string   `json:"receive_amount"`
	EffectiveRate string   `json:"effective_rate"`
	LossPct       string   `json:"loss_pct"`
	LossAmount    string   `json:"loss_amount,omitempty"`
	Verdict       string   `json:"verdict"`
	PathCount     int      `json:"path_count,omitempty"`
	Warnings      []string `json:"warnings"`
}

// CostPartJSON is one component of a rung's effective transfer cost.
//
// It follows the project's unknown discipline, the same rule MetricJSON
// enforces for an unmeasured metric: an undetermined component carries no
// number at all. Amount and Pct are omitted entirely rather than emitted as
// zero, because an unmeasured component has no amount and zero would be the
// default-to-zero failure in a new place. Determined is always present, and
// Reason is required whenever Determined is false.
type CostPartJSON struct {
	Component  string `json:"component"`
	Amount     string `json:"amount,omitempty"`
	Pct        string `json:"pct,omitempty"`
	Determined bool   `json:"determined"`
	Reason     string `json:"reason,omitempty"`
}

// CostBlockJSON is a priced rung's full cost decomposition on the wire:
// the individual components plus the total loss percentage they sum toward.
type CostBlockJSON struct {
	Parts        []CostPartJSON `json:"parts"`
	TotalLossPct string         `json:"total_loss_pct"`
}

type ExecutionRatePointJSON struct {
	Size   string `json:"size"`
	Rate   string `json:"rate,omitempty"`
	Priced bool   `json:"priced"`
	Reason string `json:"reason,omitempty"`
}

type ExecutionRateCurveJSON struct {
	Points           []ExecutionRatePointJSON `json:"points"`
	PricedCount      int                      `json:"priced_count"`
	ObservationCount int                      `json:"observation_count"`
	NonMonotonic     bool                     `json:"non_monotonic"`
}

type RungJSON struct {
	SendAmount   string         `json:"send_amount"`
	MarginalCost string         `json:"marginal_cost,omitempty"`
	MarginalFrom string         `json:"marginal_from,omitempty"`
	MarginalTo   string         `json:"marginal_to,omitempty"`
	Priced       bool           `json:"priced"`
	Integrity    string         `json:"integrity"`
	PathCount    int            `json:"path_count,omitempty"`
	Quote        *QuoteJSON     `json:"quote"`
	Cost         *CostBlockJSON `json:"cost,omitempty"`
	Notes        []string       `json:"notes"`
	Error        string         `json:"error,omitempty"`
}

// ReferenceDiagnosticJSON is the third-provider attribution of a DISAGREE.
//
// It is present only when a diagnostic provider was configured and the two
// scored providers disagreed. It is evidence about which primary is the
// outlier, not an input: reference_mid, reference_source and every verdict are
// chosen exactly as they would be without it.
type ReferenceDiagnosticJSON struct {
	Source             string `json:"source"`
	Mid                string `json:"mid,omitempty"`
	AsOf               string `json:"as_of,omitempty"`
	Outlier            string `json:"outlier,omitempty"`
	OutlierDistancePct string `json:"outlier_distance_pct,omitempty"`
	OtherDistancePct   string `json:"other_distance_pct,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

type CorridorJSON struct {
	SendAsset    AssetJSON `json:"send_asset"`
	ReceiveAsset AssetJSON `json:"receive_asset"`

	Integrity string      `json:"integrity"`
	DependsOn []AssetJSON `json:"depends_on"`

	ReferenceMid    string `json:"reference_mid"`
	ReferenceSource string `json:"reference_source"`
	ReferencePair   string `json:"reference_pair"`

	// The cross-check between reference providers.
	//
	// ReferenceAgreement is AGREE, DISAGREE, STALE, MALFUNCTION or SINGLE.
	// Scored is false when the providers disagreed so far apart that no
	// verdict could honestly be derived from either; a client rendering the
	// loss curve without checking it would show zeroes as though they were
	// measurements.
	ReferenceAgreement       string                   `json:"reference_agreement"`
	ReferenceSecondaryMid    string                   `json:"reference_secondary_mid,omitempty"`
	ReferenceSecondarySource string                   `json:"reference_secondary_source,omitempty"`
	ReferenceDivergencePct   string                   `json:"reference_divergence_pct,omitempty"`
	ReferenceNote            string                   `json:"reference_note,omitempty"`
	ReferenceDiagnostic      *ReferenceDiagnosticJSON `json:"reference_diagnostic,omitempty"`
	Scored                   bool                     `json:"scored"`

	// ReferenceAsOf is the upstream provider's rate timestamp. Omitted if
	// the provider provided no timestamp.
	ReferenceAsOf string `json:"reference_as_of,omitempty"`

	// ReferenceSecondaryAsOf is the second provider's rate timestamp.
	// Omitted if absent.
	ReferenceSecondaryAsOf string `json:"reference_secondary_as_of,omitempty"`

	// ReferenceFetchedAt is when the rate was last obtained from the
	// provider, which differs from reference_as_of: as-of is the upstream's
	// own stamp, fetched-at is when we asked. A cached rate has an older
	// fetched-at, and a client showing only one of the two cannot tell a
	// current figure from a reused one.
	ReferenceFetchedAt string `json:"reference_fetched_at,omitempty"`

	// The parallel/street-market reference, reported as a second dimension
	// alongside the official rate and never blended into it. The whole block
	// is absent when no parallel source is configured; present-but-
	// UNABLE-TO-DETERMINE (with parallel_reason) when a source was configured
	// but could not be defended.
	//
	// ParallelStatus is REPORTED or UNABLE-TO-DETERMINE. parallel_gap_pct is
	// how far the parallel mid sits from the official mid, signed, and is
	// meaningful only when the status is REPORTED. The official verdict and
	// loss figures above are computed without reference to any of these.
	ParallelStatus string `json:"parallel_status,omitempty"`
	ParallelMid    string `json:"parallel_mid,omitempty"`
	ParallelSource string `json:"parallel_source,omitempty"`
	ParallelGapPct string `json:"parallel_gap_pct,omitempty"`
	ParallelReason string `json:"parallel_reason,omitempty"`

	Floor     string `json:"floor_loss_pct"`
	FloorSize string `json:"floor_size"`
	WorstLoss string `json:"worst_loss_pct"`
	WorstSize string `json:"worst_size"`

	Recommended     *QuoteJSON `json:"recommended"`
	RecommendedSize string     `json:"recommended_size,omitempty"`

	// Live is true for a measurement taken now, false for one replayed from
	// the run store because a live fetch failed. It is present on every
	// response, never omitted, so a client that ignores the field cannot
	// mistake a stored reading for a fresh one by its absence.
	Live bool `json:"live"`

	// Stale describes the stored reading's age, and is present only when
	// Live is false.
	Stale *StaleJSON `json:"stale,omitempty"`

	// Findings are check results: facts about the counterparties this
	// corridor depends on. They qualify the headline and never move it —
	// Integrity and Verdict above are computed without reference to them,
	// and nothing here feeds back into either.
	Findings *checks.FindingsJSON `json:"findings,omitempty"`

	Finding    string                  `json:"finding"`
	Curve      *ExecutionRateCurveJSON `json:"curve"`
	Rungs      []RungJSON              `json:"rungs"`
	MeasuredAt string                  `json:"measured_at"`
}

// StaleJSON labels a reading served from history rather than measured now.
//
// Nothing is ever fabricated to fill a gap: when a live fetch fails and no
// stored run exists, the request errors rather than returning a plausible
// number. This struct exists so the case where a stored run does exist is
// unmistakable.
type StaleJSON struct {
	RecordedAt string `json:"recorded_at"`
	AgeSeconds int64  `json:"age_seconds"`
	AgeHuman   string `json:"age_human"`
}

// AssetJSON identifies an asset on the wire.
//
// Code and Issuer are kept as separate fields for a reader who wants one or
// the other without parsing a string. Asset carries the same identity in the
// single wire form the README specifies — "stellar:CODE:ISSUER",
// "stellar:native", or "iso4217:CODE" — which is also the SEP-38 asset
// identification format this project already speaks everywhere else, so a
// client comparing an asset here against a SEP-38 quote request or response
// can compare the strings directly rather than reassembling one from parts.
//
// Asset is empty whenever the source asset.Asset is not Identifiable — an
// issued Stellar asset with no issuer, in particular — and on a wire producer
// that has only a bare code to work from at all (see route.AssetJSON{Code:
// code} call sites), because in both cases a wire form built from a guess
// would state an identity nothing verified.
//
// IssuerUnknown is the explicit companion to that absence: it is emitted
// exactly when the asset names a code but the issuing account could not be
// established, so the two ways `issuer` can be empty — "there is no issuer
// by nature" (native XLM, off-chain fiat, identified in full by the wire
// form above) and "an issuer exists but was never established" — are never
// indistinguishable in the JSON. Omitting the field silently would make a
// client guess issuers from codes, which is the identity failure a hops
// array exists to end; a hop that cannot say who issued it says so.
type AssetJSON struct {
	Code          string `json:"code"`
	Issuer        string `json:"issuer,omitempty"`
	IssuerUnknown bool   `json:"issuer_unknown,omitempty"`
	Peg           string `json:"peg,omitempty"`
	Asset         string `json:"asset,omitempty"`
}

func ToAssetJSON(a asset.Asset) AssetJSON {
	j := AssetJSON{Code: a.Code, Issuer: a.Issuer}
	if a.Identifiable() {
		j.Asset = a.SEP38()
	} else if a.Code != "" {
		// A code with no wire form behind it: native and fiat always
		// render above, so this is an issued asset (or an unrecognised
		// kind) naming a code whose identity nothing established. Say so
		// rather than let the empty issuer field read as "this asset has
		// no issuer". An asset with no code at all names nothing and
		// gets no marker — there is no identity question to answer.
		j.IssuerUnknown = true
	}
	if peg, ok := asset.FiatPeg(a); ok {
		j.Peg = peg
	}
	return j
}

func ToQuoteJSON(q *Quote) *QuoteJSON {
	if q == nil {
		return nil
	}
	w := q.Warnings
	if w == nil {
		w = []string{}
	}
	return &QuoteJSON{
		Kind:          string(q.Kind),
		Description:   q.Description,
		Hops:          hopsJSON(q),
		Source:        q.Source,
		ReceiveAmount: q.ReceiveAmount.String(),
		EffectiveRate: q.EffectiveRate.String(),
		LossPct:       q.LossPct.String(),
		LossAmount:    q.LossAmount.StringFixed(2),
		Verdict:       q.Verdict.String(),
		PathCount:     q.PathCount,
		Warnings:      w,
	}
}

// hopsJSON renders the quote's path as structured hops in execution order:
// the send asset, every intermediate, the receive asset — the same sequence
// Description draws its codes from.
//
// An asset with no code is skipped rather than published as an empty hop. A
// placeholder with a blank code identifies nothing at all, so emitting one
// would put a hop on the wire that no pathfinding ever returned; the
// endpoints of any path the engine priced always carry codes, so a hops
// array from a real quote always includes both.
func hopsJSON(q *Quote) []AssetJSON {
	seq := make([]asset.Asset, 0, len(q.Hops)+2)
	seq = append(seq, q.SendAsset)
	seq = append(seq, q.Hops...)
	seq = append(seq, q.ReceiveAsset)

	hops := make([]AssetJSON, 0, len(seq))
	for _, a := range seq {
		if a.Code == "" {
			continue
		}
		hops = append(hops, ToAssetJSON(a))
	}
	return hops
}

// ToCostBlockJSON renders a decomposition for the wire.
//
// Component money values are decimal strings, like every other number that
// crosses the wire, so a client is never invited to round them through a
// float64. Undetermined components carry their reason and nothing else.
func ToCostBlockJSON(d CostDecomposition) *CostBlockJSON {
	if len(d.Parts) == 0 {
		return nil
	}
	out := &CostBlockJSON{
		Parts:        make([]CostPartJSON, 0, len(d.Parts)),
		TotalLossPct: d.TotalLossPct.String(),
	}
	for _, p := range d.Parts {
		cp := CostPartJSON{
			Component:  string(p.Component),
			Determined: p.Determined,
			Reason:     p.Reason,
		}
		if p.Determined {
			cp.Amount = p.Amount.String()
			cp.Pct = p.Pct.String()
		}
		out.Parts = append(out.Parts, cp)
	}
	return out
}

func ToCorridorJSON(l *LadderResult, pair string) CorridorJSON {
	out := CorridorJSON{
		SendAsset:          ToAssetJSON(l.Request.SendAsset),
		ReceiveAsset:       ToAssetJSON(l.Request.ReceiveAsset),
		Integrity:          l.Integrity.String(),
		DependsOn:          []AssetJSON{},
		ReferenceMid:       l.ReferenceMid.String(),
		ReferenceSource:    l.ReferenceSource,
		ReferencePair:      pair,
		ReferenceAgreement: l.Reference.Agreement.String(),
		ReferenceNote:      l.Reference.Note,
		Scored:             l.Reference.Scorable(),
		Floor:              l.Floor.StringFixed(2),
		FloorSize:          l.FloorSize.String(),
		WorstLoss:          l.WorstLoss.StringFixed(2),
		WorstSize:          l.WorstSize.String(),
		Recommended:        ToQuoteJSON(l.Recommended),
		Live:               true,
		Finding:            l.Finding,
		Rungs:              make([]RungJSON, 0, len(l.Rungs)),
		MeasuredAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if !l.Reference.AsOf.IsZero() {
		out.ReferenceAsOf = l.Reference.AsOf.UTC().Format(time.RFC3339)
	}
	if !l.Reference.SecondaryMid.IsZero() {
		out.ReferenceSecondaryMid = l.Reference.SecondaryMid.String()
		out.ReferenceSecondarySource = l.Reference.SecondarySource
	}
	if !l.Reference.SecondaryAsOf.IsZero() {
		out.ReferenceSecondaryAsOf = l.Reference.SecondaryAsOf.UTC().Format(time.RFC3339)
	}
	if !l.Reference.FetchedAt.IsZero() {
		out.ReferenceFetchedAt = l.Reference.FetchedAt.UTC().Format(time.RFC3339)
	}
	if !l.Reference.DivergencePct.IsZero() {
		out.ReferenceDivergencePct = l.Reference.DivergencePct.StringFixed(4)
	}
	if d := l.Reference.Diagnostic; d != nil {
		out.ReferenceDiagnostic = &ReferenceDiagnosticJSON{
			Source: d.Source,
			Reason: d.Reason,
		}
		// A failed diagnostic has no mid; zero is absence, not a quoted
		// figure, so it is omitted rather than published as "0".
		if !d.Mid.IsZero() {
			out.ReferenceDiagnostic.Mid = d.Mid.String()
		}
		if !d.AsOf.IsZero() {
			out.ReferenceDiagnostic.AsOf = d.AsOf.UTC().Format(time.RFC3339)
		}
		out.ReferenceDiagnostic.Outlier = d.Outlier
		if !d.OutlierDistancePct.IsZero() {
			out.ReferenceDiagnostic.OutlierDistancePct = d.OutlierDistancePct.StringFixed(4)
		}
		if !d.OtherDistancePct.IsZero() {
			out.ReferenceDiagnostic.OtherDistancePct = d.OtherDistancePct.StringFixed(4)
		}
	}
	if l.Parallel != nil {
		out.ParallelStatus = l.Parallel.Status.String()
		out.ParallelSource = l.Parallel.Source
		out.ParallelReason = l.Parallel.Reason
		if l.Parallel.Reported() {
			out.ParallelMid = l.Parallel.Mid.String()
			out.ParallelGapPct = l.Parallel.GapPct.StringFixed(4)
		}
	}

	if l.Recommended != nil {
		out.RecommendedSize = l.RecommendedSize.String()
	}
	if l.Curve != nil {
		out.Curve = &ExecutionRateCurveJSON{
			Points:           make([]ExecutionRatePointJSON, 0, len(l.Curve.Points)),
			PricedCount:      l.Curve.PricedCount,
			ObservationCount: l.Curve.ObservationCount,
			NonMonotonic:     l.Curve.NonMonotonic,
		}
		for _, p := range l.Curve.Points {
			point := ExecutionRatePointJSON{Size: p.Size.String(), Priced: p.Priced, Reason: p.Reason}
			if p.Priced {
				point.Rate = p.Rate.String()
			}
			out.Curve.Points = append(out.Curve.Points, point)
		}
	}

	for _, d := range l.DependsOn {
		out.DependsOn = append(out.DependsOn, ToAssetJSON(d))
	}

	for _, r := range l.Rungs {
		out.Rungs = append(out.Rungs, ToRungJSON(r))
	}
	return out
}

// ToRungJSON renders one rung in the wire shape. It is exported so a caller
// that emits rungs as they resolve — the streaming corridor endpoint, which
// sends a rung before the ladder has summarised — renders each one with the
// same conversion the whole-corridor response uses. Two converters would
// drift, and the one that drifted would be the one a streaming client saw
// first.
//
// A rung sent before summarisation carries no marginal cost or decomposition
// yet (both are computed across the finished ladder), so those fields are
// absent here and filled in on the final response. Everything the rung itself
// established — priced, integrity, quote, notes, error — is present and exact.
func ToRungJSON(r Rung) RungJSON {
	rj := RungJSON{
		SendAmount: r.SendAmount.String(),
		Priced:     r.Priced(),
		Integrity:  IntegrityUnknown.String(),
		Notes:      []string{},
	}
	if r.MarginalCost != nil {
		rj.MarginalCost = r.MarginalCost.Cost.String()
		rj.MarginalFrom = r.MarginalCost.From.String()
		rj.MarginalTo = r.MarginalCost.To.String()
	}
	if r.Err != nil {
		rj.Error = r.Err.Error()
	}
	if r.Result != nil {
		rj.Integrity = r.Result.Integrity.String()
		if r.Result.Notes != nil {
			rj.Notes = r.Result.Notes
		}
		if len(r.Result.Quotes) > 0 {
			q := &r.Result.Quotes[0]
			rj.PathCount = q.PathCount
			rj.Quote = ToQuoteJSON(q)
		}
	}
	if len(r.Decomposition.Parts) > 0 {
		rj.Cost = ToCostBlockJSON(r.Decomposition)
	}
	return rj
}

// WithFindings attaches check results to a rendered corridor.
//
// This is the composition point, and it is deliberately the only one: it takes
// findings as input and returns a document whose headline fields are copied
// unchanged from the input. There is no branch here on severity, on failure
// counts, or on any check's identity.
//
// The rule it enforces is that checks qualify the headline and never move it.
// Integrity, the verdict on every rung, the loss figures and the recommendation
// are all computed before this function is reachable, from pathfinding and a
// reference rate. Letting an observation about a third party rewrite any of
// them would make the headline unfalsifiable — a reader could no longer tell
// whether a corridor was downgraded because its liquidity moved or because
// somebody added a check.
//
// TestFindingsDoNotMoveTheHeadline attacks this function specifically.
func WithFindings(c CorridorJSON, f *checks.Findings) CorridorJSON {
	if f == nil || (len(f.Checks) == 0 && len(f.Metrics) == 0) {
		// Absent and empty must not look the same. An empty findings block
		// would read as "checked, nothing found", which is a different
		// claim from "not checked".
		return c
	}
	j := f.ToJSON()
	c.Findings = &j
	return c
}
