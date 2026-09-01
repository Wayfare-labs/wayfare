// Package checks observes facts about the counterparties a corridor depends on.
//
// The engine already answers two questions well: whether a corridor can execute
// (integrity) and what it costs against fair value (verdict). Neither says
// anything about what the corridor depends on — whether the issuer can freeze a
// balance, whether declared endpoints answer, whether a published document says
// what it claims. Those are facts about third parties, and they change what a
// number means without changing the number.
//
// # The three-valued result
//
// A check is determined-and-passed, determined-and-failed, or not determined.
// The third is not a failure. An anchor that publishes no SEP-10 endpoint is a
// different fact from one publishing an endpoint that does not answer, and both
// differ from one that works. Collapsing the first into "fail" is the same
// category error as reporting NO-MARKET as a bad price.
//
// So Determined is a separate field from Passed, and Passed is meaningless
// unless Determined is true. There is no way to express unknown as a zero, a
// default, or a false.
//
// # Checks never move the headline
//
// Integrity and Verdict stay authoritative. No result, at any severity, may
// change either — see Findings. Letting observations about third parties
// rewrite states derived arithmetically would make the headline unfalsifiable:
// a reader could no longer tell whether a corridor was downgraded because its
// liquidity moved or because somebody added a check.
//
// The full contract is docs/checks.md.
package checks

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/anchor"
	"github.com/Wayfare-labs/wayfare/asset"
)

// Scope says what a check examines.
type Scope int

const (
	ScopeAnchor Scope = iota
	ScopeAsset
	ScopeCorridor
)

func (s Scope) String() string {
	switch s {
	case ScopeAsset:
		return "asset"
	case ScopeCorridor:
		return "corridor"
	default:
		return "anchor"
	}
}

// Cost declares how expensive a check is, so a scheduler can run cheap checks
// often and expensive ones rarely without knowing what any of them do.
type Cost int

const (
	// CostFree is derivable from data already fetched. No I/O.
	CostFree Cost = iota
	// CostOneRequest is a single network round trip.
	CostOneRequest
	// CostExpensive is several round trips.
	CostExpensive
)

func (c Cost) String() string {
	switch c {
	case CostOneRequest:
		return "one-request"
	case CostExpensive:
		return "expensive"
	default:
		return "free"
	}
}

// Severity orders what a reader sees first. It carries no arithmetic weight
// and never feeds back into a verdict.
type Severity int

const (
	// SeverityInfo is context, not a problem.
	SeverityInfo Severity = iota
	// SeverityNotice marks a discrepancy worth knowing — declared behaviour
	// differing from observed behaviour, typically.
	SeverityNotice
	// SeverityWarning marks a failure that makes a route less reliable.
	SeverityWarning
	// SeverityCritical marks a failure that can cost a user their funds:
	// clawback enabled, authorization revocable.
	SeverityCritical
)

func (s Severity) String() string {
	switch s {
	case SeverityCritical:
		return "critical"
	case SeverityWarning:
		return "warning"
	case SeverityNotice:
		return "notice"
	default:
		return "info"
	}
}

// Unit names what a metric measured.
type Unit string

const (
	UnitPercent Unit = "percent"
	UnitRatio   Unit = "ratio"
	UnitCount   Unit = "count"
	UnitAmount  Unit = "amount"
	UnitSeconds Unit = "seconds"
)

// Venue names the liquidity source a corridor metric observes.
//
// Two metrics reporting the same corridor from different venues describe
// different markets, and cannot be reconciled by arithmetic — Horizon's
// /order_book endpoint reports only offers, while /paths/strict-send prices
// through both offers and AMM liquidity pools. A reader comparing a book
// spread against a pathfinding-based figure without knowing which venue each
// came from will draw a wrong inference; the CannotDetermine prose was where
// this used to live, and prose is not something a consumer can act on.
//
// See docs/liquidity-venues.md for the reconciliation rule this field makes
// enforceable.
type Venue string

const (
	// VenueOrderBook is Horizon's /order_book endpoint: offers only, no AMM.
	VenueOrderBook Venue = "order-book"

	// VenuePathfinding is Horizon's /paths/strict-send endpoint: offers plus
	// AMM liquidity pools, the same engine that would settle the payment.
	VenuePathfinding Venue = "pathfinding"
)

// Subject is what a check examines.
//
// Typed rather than a string: a corridor check needs a send and a receive
// asset, and a string form would force every implementation to re-parse and
// invent its own handling for a malformed one.
//
// Not every field is populated for every scope. A check must state what it
// needs in its descriptor and return an undetermined result — never a failure —
// when the field it needs is absent. Missing input is not evidence of a
// problem with the subject.
type Subject struct {
	// Domain is the anchor's home domain, e.g. "ngnc.online".
	Domain string

	// Asset is the asset under examination, for ScopeAsset.
	Asset asset.Asset

	// Send and Receive describe a corridor, for ScopeCorridor.
	Send    asset.Asset
	Receive asset.Asset

	// Profile is an already-resolved stellar.toml, when one is available.
	// A CostFree check reads this rather than fetching anything.
	//
	// Profile is shared context, not per-check ownership: the sweep resolves
	// the anchor once and hands the same document to every check, and it is
	// not deep-copied per check. A check must treat it as read-only — the
	// other Subject fields are value-isolated, but a mutation through this
	// pointer reaches the rest of the sweep.
	Profile *anchor.Profile

	// Integrity is the corridor's routing integrity classification —
	// "DIRECT", "DERIVATIVE" or "NO-MARKET" — when the caller has one to
	// give. It is a string rather than route.Integrity because route
	// imports this package for Findings; importing route back would be a
	// cycle. Empty means unknown, and a book metric then falls back to
	// treating an empty order book purely as a market fact, as it always
	// has.
	//
	// This exists because an order book response cannot distinguish "no
	// market exists by construction" from "a market exists and is
	// currently idle" — both are an empty book. Only pathfinding across
	// the whole ladder can tell the two apart, and a metric that only ever
	// fetches /order_book has no way to learn it without being told.
	Integrity string

	// Underlying is the direct pair a DERIVATIVE corridor's paths actually
	// traverse, when the caller has one to offer — e.g. GHSC has no direct
	// market and every path runs through NGNC, so a caller may set this to
	// NGNC. A book metric that measures Underlying instead of Receive must
	// say so explicitly in its evidence; the substitution is never silent.
	Underlying asset.Asset
}

// Label renders the subject for a reader.
func (s Subject) Label() string {
	switch {
	case s.Send.Code != "" && s.Receive.Code != "":
		return s.Send.Code + " -> " + s.Receive.Code
	case s.Asset.Code != "":
		if s.Asset.Issuer != "" && len(s.Asset.Issuer) >= 4 {
			return s.Asset.Code + " (" + s.Asset.Issuer[:4] + "…)"
		}
		return s.Asset.Code
	case s.Domain != "":
		return s.Domain
	default:
		return "unknown subject"
	}
}

// Evidence names what was observed and where.
//
// A verdict without evidence is an assertion. This is the standard the snapshot
// format applies to measurements: a reader must be able to go and look.
type Evidence struct {
	// Source is where the observation came from — a URL, an account ID, or
	// a TOML field path.
	Source string

	// Observed is the value seen, verbatim where practical.
	Observed string

	ObservedAt time.Time
}

// Observation is what every check and every metric records.
type Observation struct {
	ID      string
	Scope   Scope
	Subject string
	At      time.Time

	// Determined is false when the check could not establish the fact
	// either way. It is not a failure, and Passed is meaningless when it is
	// false.
	Determined bool

	// Reason explains an undetermined result, and is required for one.
	// "Could not determine" with no explanation is an assertion.
	Reason string

	Evidence []Evidence
}

// CheckResult is a boolean fact about a subject.
type CheckResult struct {
	Observation

	// Passed is meaningful only when Determined is true.
	Passed bool

	Severity Severity

	// Summary is one line for a reader.
	Summary string
}

// Failed reports a determined failure. Distinct from !Passed, which is also
// true for an undetermined result — the distinction this package exists for.
func (r CheckResult) Failed() bool { return r.Determined && !r.Passed }

// MetricResult is a measured quantity.
//
// Value is meaningful only when Determined is true. An unmeasurable metric
// carries Determined false, never Value zero: a spread of nothing and a spread
// that could not be read are different facts, and zero is a plausible-looking
// number for the second.
type MetricResult struct {
	Observation

	Value decimal.Decimal
	Unit  Unit

	// Venue is the liquidity source the metric observed. Copied from the
	// descriptor by MetricValue and MetricUndetermined so a caller reading
	// results without going back to Describe still sees which market the
	// figure refers to. Empty on non-corridor metrics.
	Venue Venue

	Summary string
}

// Descriptor is what a check declares about itself.
type Descriptor struct {
	ID       string
	Scope    Scope
	Cost     Cost
	Severity Severity

	// Venue names the liquidity source a corridor metric observes. Required
	// for corridor-scoped metrics and empty otherwise: an anchor or asset
	// check has no venue, and a check with one would suggest a market
	// figure the check does not produce.
	//
	// Making the venue a first-class descriptor field, rather than a phrase
	// buried inside CannotDetermine, is what lets a downstream consumer
	// refuse to reconcile a book figure with a route figure by machine.
	Venue Venue

	// Title is one line naming what is checked.
	Title string

	// CanDetermine and CannotDetermine are both required.
	//
	// The likeliest way this system misleads is not a wrong result — it is
	// a correct result read as answering more than it does. The auth-flags
	// check can prove a clawback flag is set; it cannot prove the issuer
	// will ever use it, and a reader deserves to be told which.
	CanDetermine    string
	CannotDetermine string
}

// Validate reports a descriptor that would produce unreadable results.
func (d Descriptor) Validate() error {
	var missing []string
	if strings.TrimSpace(d.ID) == "" {
		missing = append(missing, "ID")
	}
	if strings.TrimSpace(d.Title) == "" {
		missing = append(missing, "Title")
	}
	if strings.TrimSpace(d.CanDetermine) == "" {
		missing = append(missing, "CanDetermine")
	}
	if strings.TrimSpace(d.CannotDetermine) == "" {
		missing = append(missing, "CannotDetermine")
	}
	if len(missing) > 0 {
		return fmt.Errorf("checks: descriptor %q is missing required field(s): %s",
			d.ID, strings.Join(missing, ", "))
	}
	return nil
}

// ValidateAsMetric adds the metric-only requirements on top of Validate: a
// corridor-scoped metric must declare a Venue, and no metric may declare a
// venue outside the known set.
//
// A metric without a venue reports a market figure without saying which
// market — the specific misuse issue #104 exists to fix, and precisely the
// shape a downstream consumer cannot recover from without going back to the
// prose descriptor. Anchor and asset metrics have no venue: liquidity is a
// corridor property, and forcing a venue on a non-corridor metric would
// invent one.
func (d Descriptor) ValidateAsMetric() error {
	if err := d.Validate(); err != nil {
		return err
	}
	switch d.Venue {
	case "", VenueOrderBook, VenuePathfinding:
	default:
		return fmt.Errorf("checks: metric %q declares unknown venue %q; known: %q, %q",
			d.ID, d.Venue, VenueOrderBook, VenuePathfinding)
	}
	if d.Scope == ScopeCorridor && d.Venue == "" {
		return fmt.Errorf("checks: corridor metric %q must declare a Venue", d.ID)
	}
	if d.Scope != ScopeCorridor && d.Venue != "" {
		return fmt.Errorf("checks: non-corridor metric %q must not declare a Venue (got %q)",
			d.ID, d.Venue)
	}
	return nil
}

// Check observes one fact about a subject.
//
// Implementations must never panic, never block indefinitely, and never return
// a determined result they cannot evidence. Run receives a context and is
// expected to honour it.
type Check interface {
	Describe() Descriptor
	Run(ctx context.Context, s Subject) CheckResult
}

// result constructors ---------------------------------------------------------

// Pass builds a determined, passing result.
func Pass(d Descriptor, s Subject, summary string, ev ...Evidence) CheckResult {
	return CheckResult{
		Observation: Observation{
			ID: d.ID, Scope: d.Scope, Subject: s.Label(),
			At: time.Now().UTC(), Determined: true, Evidence: ev,
		},
		Passed:   true,
		Severity: d.Severity,
		Summary:  summary,
	}
}

// Fail builds a determined, failing result.
func Fail(d Descriptor, s Subject, summary string, ev ...Evidence) CheckResult {
	return CheckResult{
		Observation: Observation{
			ID: d.ID, Scope: d.Scope, Subject: s.Label(),
			At: time.Now().UTC(), Determined: true, Evidence: ev,
		},
		Passed:   false,
		Severity: d.Severity,
		Summary:  summary,
	}
}

// Undetermined builds a result for a fact that could not be established.
//
// The reason is mandatory at the call site because it is mandatory in the
// contract: a result that says only "unknown" tells a reader nothing about
// whether to look further.
func Undetermined(d Descriptor, s Subject, reason string, ev ...Evidence) CheckResult {
	if strings.TrimSpace(reason) == "" {
		reason = "no reason given — this is a bug in check " + d.ID
	}
	return CheckResult{
		Observation: Observation{
			ID: d.ID, Scope: d.Scope, Subject: s.Label(),
			At: time.Now().UTC(), Determined: false, Reason: reason, Evidence: ev,
		},
		Severity: d.Severity,
		Summary:  "could not determine: " + reason,
	}
}

// running ---------------------------------------------------------------------

// Run executes one check, converting a panic into an undetermined result.
//
// A contributed check that panics must not take down a sweep. The other
// corridors are still worth measuring, and a check that crashed is exactly the
// kind of thing a reader should be told about rather than shielded from.
func Run(ctx context.Context, c Check, s Subject) (res CheckResult) {
	d := c.Describe()

	defer func() {
		if r := recover(); r != nil {
			res = Undetermined(d, s, fmt.Sprintf("check panicked: %v", r))
		}
	}()

	if err := d.Validate(); err != nil {
		return Undetermined(d, s, err.Error())
	}
	if err := ctx.Err(); err != nil {
		return Undetermined(d, s, "context cancelled before the check ran: "+err.Error())
	}

	res = c.Run(ctx, s)

	// A check that forgot to fill in its identity would produce results no
	// reader could attribute. Repair rather than discard: the observation
	// still happened.
	if res.ID == "" {
		res.ID = d.ID
	}
	if res.Subject == "" {
		res.Subject = s.Label()
	}
	if res.At.IsZero() {
		res.At = time.Now().UTC()
	}
	if !res.Determined && strings.TrimSpace(res.Reason) == "" {
		res.Reason = "no reason given — this is a bug in check " + d.ID
	}
	return res
}

// RunAll executes every check against one subject, in declaration order.
//
// Sequential rather than concurrent: the set is small, several checks share an
// upstream, and a burst of parallel requests at a third party is worse
// behaviour than a few extra milliseconds.
func RunAll(ctx context.Context, cs []Check, s Subject) []CheckResult {
	out := make([]CheckResult, 0, len(cs))
	for _, c := range cs {
		out = append(out, Run(ctx, c, s))
	}
	return out
}

// Findings is a corridor's check results, ready to report.
//
// It deliberately exposes no way to influence integrity or a verdict. The
// composition rule — checks qualify the headline, they never move it — is
// enforced by this type having no path back into the engine, not by a comment
// asking callers to behave.
type Findings struct {
	Checks  []CheckResult
	Metrics []MetricResult
}

// Add appends a result.
func (f *Findings) Add(r CheckResult) { f.Checks = append(f.Checks, r) }

// AddMetric appends a measured quantity.
func (f *Findings) AddMetric(m MetricResult) { f.Metrics = append(f.Metrics, m) }

// Counts summarises results by state, for a reader deciding where to look.
func (f *Findings) Counts() (passed, failed, undetermined int) {
	for _, r := range f.Checks {
		switch {
		case !r.Determined:
			undetermined++
		case r.Passed:
			passed++
		default:
			failed++
		}
	}
	return
}

// Worst returns the highest severity among failed checks, and whether any
// failed at all.
//
// Undetermined results are excluded on purpose. Not knowing something is not a
// finding against the subject, and letting it raise a severity would recreate
// the fail/unknown collapse this package exists to prevent.
func (f *Findings) Worst() (Severity, bool) {
	worst, any := SeverityInfo, false
	for _, r := range f.Checks {
		if r.Failed() && (!any || r.Severity > worst) {
			worst, any = r.Severity, true
		}
	}
	return worst, any
}

// Sorted returns results ordered for display: failures first by descending
// severity, then undetermined, then passes. Ties break on ID so output is
// stable across runs.
func (f *Findings) Sorted() []CheckResult {
	out := make([]CheckResult, len(f.Checks))
	copy(out, f.Checks)

	rank := func(r CheckResult) int {
		switch {
		case r.Failed():
			return 0
		case !r.Determined:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		if ri == 0 && out[i].Severity != out[j].Severity {
			return out[i].Severity > out[j].Severity
		}
		return out[i].ID < out[j].ID
	})
	return out
}
