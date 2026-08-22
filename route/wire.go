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
	Description   string   `json:"description"`
	Source        string   `json:"source"`
	ReceiveAmount string   `json:"receive_amount"`
	EffectiveRate string   `json:"effective_rate"`
	LossPct       string   `json:"loss_pct"`
	LossAmount    string   `json:"loss_amount"`
	Verdict       string   `json:"verdict"`
	Warnings      []string `json:"warnings"`
}

type RungJSON struct {
	SendAmount string     `json:"send_amount"`
	Priced     bool       `json:"priced"`
	Integrity  string     `json:"integrity"`
	Quote      *QuoteJSON `json:"quote"`
	Notes      []string   `json:"notes"`
	Error      string     `json:"error,omitempty"`
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
	ReferenceAgreement       string `json:"reference_agreement"`
	ReferenceSecondaryMid    string `json:"reference_secondary_mid,omitempty"`
	ReferenceSecondarySource string `json:"reference_secondary_source,omitempty"`
	ReferenceDivergencePct   string `json:"reference_divergence_pct,omitempty"`
	ReferenceNote            string `json:"reference_note,omitempty"`
	Scored                   bool   `json:"scored"`

	// ReferenceFetchedAt is when the rate was last obtained from the
	// provider, which differs from reference_as_of: as-of is the upstream's
	// own stamp, fetched-at is when we asked. A cached rate has an older
	// fetched-at, and a client showing only one of the two cannot tell a
	// current figure from a reused one.
	ReferenceFetchedAt string `json:"reference_fetched_at,omitempty"`

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

	Finding    string     `json:"finding"`
	Rungs      []RungJSON `json:"rungs"`
	MeasuredAt string     `json:"measured_at"`
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

type AssetJSON struct {
	Code   string `json:"code"`
	Issuer string `json:"issuer,omitempty"`
	Peg    string `json:"peg,omitempty"`
}

func ToAssetJSON(a asset.Asset) AssetJSON {
	j := AssetJSON{Code: a.Code, Issuer: a.Issuer}
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
		Description:   q.Description,
		Source:        q.Source,
		ReceiveAmount: q.ReceiveAmount.String(),
		EffectiveRate: q.EffectiveRate.String(),
		LossPct:       q.LossPct.StringFixed(2),
		LossAmount:    q.LossAmount.StringFixed(2),
		Verdict:       q.Verdict.String(),
		Warnings:      w,
	}
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
	if !l.Reference.SecondaryMid.IsZero() {
		out.ReferenceSecondaryMid = l.Reference.SecondaryMid.String()
		out.ReferenceSecondarySource = l.Reference.SecondarySource
	}
	if !l.Reference.FetchedAt.IsZero() {
		out.ReferenceFetchedAt = l.Reference.FetchedAt.UTC().Format(time.RFC3339)
	}
	if !l.Reference.DivergencePct.IsZero() {
		out.ReferenceDivergencePct = l.Reference.DivergencePct.StringFixed(4)
	}

	if l.Recommended != nil {
		out.RecommendedSize = l.RecommendedSize.String()
	}
	for _, d := range l.DependsOn {
		out.DependsOn = append(out.DependsOn, ToAssetJSON(d))
	}

	for _, r := range l.Rungs {
		rj := RungJSON{
			SendAmount: r.SendAmount.String(),
			Priced:     r.Priced(),
			Integrity:  IntegrityUnknown.String(),
			Notes:      []string{},
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
				rj.Quote = ToQuoteJSON(&r.Result.Quotes[0])
			}
		}
		out.Rungs = append(out.Rungs, rj)
	}
	return out
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
