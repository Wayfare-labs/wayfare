// Package monitor runs corridor measurements on a schedule.
//
// # Why this is separate from the HTTP surface
//
// A monitor that only measures while somebody has a page open is not a
// monitor; it is a calculator with a web front end. The history it produces
// would have holes exactly where nobody was looking, which is precisely when a
// corridor breaking is most worth having recorded.
//
// So this package imports nothing from server. A Scheduler is constructible and
// runnable with no server at all, and wayfared -serve=false does exactly that.
// The dependency runs one way: the server may read what the scheduler wrote,
// and the scheduler never needs the server to exist.
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/Wayfare-labs/wayfare/failover"
	"github.com/Wayfare-labs/wayfare/metrics"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// Corridor is one measurement target.
type Corridor struct {
	Send    asset.Asset
	Receive asset.Asset

	// ReferenceBase and ReferenceQuote name the fiat pair to benchmark
	// against. Nobody publishes a mid-market rate for a token, so this is
	// the peg it claims to track.
	ReferenceBase  string
	ReferenceQuote string
}

// Key is the corridor's identifier in the run store.
func (c Corridor) Key() string {
	return runstore.CorridorKey(c.Send.Code, c.Receive.Code)
}

// DefaultCorridors derives the sweep from the verified asset registry.
// Every registered fiat destination is measured, including pending assets:
// their declared status is part of the finding, not a reason to hide them.
// USD-pegged destinations are deliberately excluded until the USD-to-USD
// measurement semantics are decided in issue #671.
func DefaultCorridors() []Corridor {
	corridors := make([]Corridor, 0)
	for _, entry := range asset.Registry() {
		if entry.Peg == "" || entry.Peg == "USD" {
			continue
		}
		corridors = append(corridors, Corridor{
			Send:           asset.USDC(),
			Receive:        asset.Stellar(entry.Code, entry.Issuer),
			ReferenceBase:  "USD",
			ReferenceQuote: entry.Peg,
		})
	}
	return corridors
}

// DefaultInterval is how often corridors are measured.
//
// Six hours. The corridors measured so far move on the scale of days — the
// 2026-08-04 and 2026-08-08 runs differ by about a percent — so a shorter
// interval would buy resolution nobody needs while multiplying load on a
// shared public service. The deployment guide documents the aggregate
// Horizon request ceiling for the registry-derived sweep.
const DefaultInterval = 6 * time.Hour

// DefaultJitter is the maximum random delay added to each scheduled interval.
const DefaultJitter = 5 * time.Minute

// Scheduler measures corridors on an interval and records the results.
type Scheduler struct {
	Engine    *route.Engine
	Store     runstore.Store
	Corridors []Corridor

	// Checks runs counterparty checks alongside each measurement, exactly
	// as the HTTP server does, so a scheduled history records the same
	// findings a live response serves. Nil disables checks, and stored
	// records then carry none.
	Checks *checks.Runner

	// SnapshotDir enables scheduled capture. SnapshotEngineFactory must
	// configure every upstream client to use the supplied recorder.
	SnapshotDir           string
	SnapshotSources       snapshot.Sources
	SnapshotNotes         []string
	SnapshotEngineFactory func(*snapshot.Recorder) *route.Engine
	SnapshotChecksFactory func(*snapshot.Recorder) *checks.Runner

	// Interval is the gap between sweeps. Zero means DefaultInterval.
	Interval time.Duration

	// Jitter is the maximum random delay added to Interval. Zero disables it.
	Jitter time.Duration

	// JitterSeed makes the interval sequence reproducible when set. Nil uses
	// a time-based seed.
	JitterSeed *int64

	// Logger is where run outcomes go. Nil means slog.Default.
	Logger *slog.Logger

	// Metrics counts each corridor's sweep outcome (issue #705): measured,
	// failed, or measured and then refused by the store. Nil observes nothing.
	//
	// The scheduler reports what it did; the upstream counters come from the
	// instrumented HTTP clients it is handed, so a Scheduler run without them
	// still answers "did the sweep record this corridor" and says nothing
	// about why it did not.
	Metrics *metrics.Collector

	// Now is the clock, for tests. Nil means time.Now.
	Now func() time.Time
}

func (s *Scheduler) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *Scheduler) interval() time.Duration {
	if s.Interval > 0 {
		return s.Interval
	}
	return DefaultInterval
}

func (s *Scheduler) nextInterval(rng *rand.Rand) time.Duration {
	if s.Jitter <= 0 {
		return s.interval()
	}
	return s.interval() + time.Duration(rng.Int63n(int64(s.Jitter)))
}

func (s *Scheduler) randomSource() *rand.Rand {
	seed := time.Now().UnixNano()
	if s.JitterSeed != nil {
		seed = *s.JitterSeed
	}
	return rand.New(rand.NewSource(seed))
}

func (s *Scheduler) corridors() []Corridor {
	if len(s.Corridors) > 0 {
		return s.Corridors
	}
	return DefaultCorridors()
}

func (s *Scheduler) store() runstore.Store {
	if s.Store != nil {
		return s.Store
	}
	return runstore.Nop{}
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Run measures on the interval until ctx is cancelled.
//
// The first sweep happens immediately rather than after one interval, so a
// freshly deployed instance has data to serve within minutes instead of hours.
func (s *Scheduler) Run(ctx context.Context) error {
	if s.Engine == nil {
		return fmt.Errorf("monitor: Scheduler requires an Engine")
	}

	s.log().Info("scheduler starting",
		"interval", s.interval().String(),
		"jitter", s.Jitter.String(),
		"corridors", len(s.corridors()))

	if err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
		// A failed sweep is logged inside RunOnce per corridor; reaching
		// here means something structural. Keep running: a monitor that
		// exits on one bad sweep stops recording precisely when a corridor
		// is misbehaving.
		s.log().Error("initial sweep failed", "error", err)
	}

	rng := s.randomSource()

	for {
		timer := time.NewTimer(s.nextInterval(rng))
		select {
		case <-ctx.Done():
			timer.Stop()
			s.log().Info("scheduler stopping", "reason", ctx.Err())
			return ctx.Err()
		case <-timer.C:
			if err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
				s.log().Error("sweep failed", "error", err)
			}
		}
	}
}

// RunOnce measures every corridor once and records what it finds.
//
// Corridors are measured in sequence rather than concurrently. Each ladder
// already runs four requests in parallel, and three simultaneous ladders would
// be a burst against a shared public service for no benefit at this cadence.
//
// A corridor that fails does not stop the sweep: the others are still worth
// recording, and a corridor being unreachable is itself information.
func (s *Scheduler) RunOnce(ctx context.Context) error {
	if s.Engine == nil {
		return fmt.Errorf("monitor: Scheduler requires an Engine")
	}

	var failures int
	for _, c := range s.corridors() {
		if err := ctx.Err(); err != nil {
			return err
		}
		outcome, err := s.measure(ctx, c)
		if err != nil {
			outcome = metrics.SweepFailed
			failures++
			s.log().Error("corridor measurement failed",
				"corridor", c.Key(), "error", err)
		}
		s.Metrics.Sweep(c.Key(), outcome)
	}

	if failures == len(s.corridors()) && failures > 0 {
		return fmt.Errorf("monitor: all %d corridors failed to measure", failures)
	}
	return nil
}

// measure runs one corridor's ladder and appends the result.
//
// It reports the sweep outcome alongside the error so that RunOnce is the only
// place a corridor is counted: "measured but the store refused it" is visible
// here and nowhere else, and splitting the two observations across functions
// would let a future early return count a sweep twice.
func (s *Scheduler) measure(ctx context.Context, c Corridor) (string, error) {
	started := s.now()
	engine := s.Engine
	var recorder *snapshot.Recorder
	if s.SnapshotDir != "" {
		if s.SnapshotEngineFactory == nil {
			return "", fmt.Errorf("monitor: snapshot directory configured without a snapshot engine factory")
		}
		recorder = &snapshot.Recorder{
			Corridor: snapshot.Corridor{
				Send:          snapshot.AssetRef{Code: c.Send.Code, Issuer: c.Send.Issuer},
				Receive:       snapshot.AssetRef{Code: c.Receive.Code, Issuer: c.Receive.Issuer},
				ReferencePair: c.ReferenceBase + "/" + c.ReferenceQuote,
			},
			Sizes:   make([]string, len(route.DefaultSizes)),
			Sources: s.SnapshotSources,
			Notes:   s.SnapshotNotes,
		}
		for i, size := range route.DefaultSizes {
			recorder.Sizes[i] = size.String()
		}
		engine = s.SnapshotEngineFactory(recorder)
		if engine == nil {
			return "", fmt.Errorf("monitor: snapshot engine factory returned nil for %s", c.Key())
		}
	}

	// One Horizon per ladder (issue #701). The scope covers the measurement and
	// nothing else: a price curve is a comparison across sizes, so a rung priced
	// by a fallback host would be measuring the two providers' differences as
	// well as the corridor's. The counterparty checks that run afterwards
	// deliberately stay outside it — they describe an anchor, not a curve.
	ladderCtx, pin := failover.WithPin(ctx)

	result, err := engine.Ladder(ladderCtx, route.LadderRequest{
		SendAsset:      c.Send,
		ReceiveAsset:   c.Receive,
		Sizes:          route.DefaultSizes,
		ReferenceBase:  c.ReferenceBase,
		ReferenceQuote: c.ReferenceQuote,
	})
	if err != nil {
		return "", fmt.Errorf("measuring %s: %w", c.Key(), err)
	}

	pair := c.ReferenceBase + "/" + c.ReferenceQuote

	// The scheduled depth of the measurement mirrors the server: checks may
	// run after the ladder and can never change it, and the findings they
	// produce ride into storage with the record so a history read matches a
	// live read. With no checks configured, this is a no-op and the record
	// carries none — the same "not checked" the server serves.
	live := route.ToCorridorJSON(result, pair)
	checkRunner := s.Checks
	if recorder != nil && s.SnapshotChecksFactory != nil {
		checkRunner = s.SnapshotChecksFactory(recorder)
	} else if recorder != nil && checkRunner != nil {
		return "", fmt.Errorf("monitor: snapshot checks require a recorder-backed checks factory")
	}
	if checkRunner != nil {
		live = route.WithFindings(live, checkRunner.ForCorridor(ctx, checks.Corridor{
			Send:       c.Send,
			Receive:    c.Receive,
			Integrity:  result.Integrity.String(),
			Underlying: underlyingPair(result),
			Reference:  result.Reference,
			Baseline:   s.issuerDriftBaseline(ctx, c.Key()),
		}))
	}
	record := runstore.FromCorridorJSON(live)
	record.RecordedAt = started
	if recorder != nil {
		for _, rung := range result.Rungs {
			if rung.Err != nil || rung.Result == nil || rung.Result.Integrity == route.IntegrityUnknown {
				return "", fmt.Errorf("monitor: refusing to record incomplete snapshot for %s at size %s",
					c.Key(), rung.SendAmount)
			}
		}
		// Name the Horizon that answered rather than the one that was asked
		// (issue #701). The bytes in responses/ came from wherever the ladder
		// was pinned, and a manifest pointing a reader at the configured
		// primary during a failover sends them to someone else's logs. Empty
		// until an exchange settles it, which for a run with no fallback
		// configured means the configured address stands.
		if host := pin.Host(); host != "" {
			recorder.Sources.Horizon.BaseURL = host
		}
		if pin.FailedOver() {
			// Copied rather than appended in place: recorder.Notes is
			// Scheduler.SnapshotNotes, the slice every corridor in the sweep
			// starts from, so an in-place append writes into storage the
			// scheduler shares. No caller can observe that today — main.go
			// assigns a composite literal, which leaves no spare capacity — and
			// it is a leak the moment one passes a slice with room.
			recorder.Notes = append(append([]string(nil), recorder.Notes...),
				"answered by the Horizon failover host: "+pin.Host())
		}
		hash, err := snapshot.SaveContentAddressed(s.SnapshotDir, recorder)
		if err != nil {
			return "", fmt.Errorf("monitor: recording snapshot artifact for %s: %w", c.Key(), err)
		}
		record.Snapshot = hash
	}

	// Carry the second provider's mid and the divergence, so a later reader
	// can tell a corridor change from a benchmark change.
	ref := result.Reference
	record.Reference.AsOf = formatTime(ref.AsOf)
	// FetchedAt is when we last obtained the rate, which can be older than
	// the run when a cached rate was reused; a later reader needs it to
	// judge how current the benchmark was when this reading was taken.
	record.Reference.FetchedAt = formatTime(ref.FetchedAt)
	if !ref.SecondaryMid.IsZero() {
		record.Reference.SecondaryMid = ref.SecondaryMid.String()
		record.Reference.SecondarySource = ref.SecondarySource
		record.Reference.SecondaryAsOf = formatTime(ref.SecondaryAsOf)
	}
	if !ref.DivergencePct.IsZero() {
		record.Reference.DivergencePct = ref.DivergencePct.StringFixed(4)
	}
	if ref.Scorable() {
		record.Reference.ScoredAgainst = ref.Source
	}

	attrs := []any{
		"corridor", c.Key(),
		"integrity", result.Integrity.String(),
		"scored", ref.Scorable(),
		"reference", ref.Source,
		"agreement", ref.Agreement.String(),
		"took", s.now().Sub(started).Round(time.Millisecond).String(),
	}
	if ref.Scorable() {
		attrs = append(attrs,
			"floor_loss_pct", result.Floor.StringFixed(2),
			"worst_loss_pct", result.WorstLoss.StringFixed(2))
	}
	s.log().Info("corridor measured", attrs...)

	// A storage failure must not lose the measurement for the caller: it
	// already happened, and the reader is better served by a logged write
	// failure than by a sweep that reports success only when both the
	// network and the disk cooperated.
	//
	// It is still counted as a distinct outcome, because the history now has
	// a hole this corridor did not cause. An operator alerting on failures
	// would otherwise never learn the disk is full: the sweep looked healthy,
	// and the trend chart simply stopped having points.
	if err := s.store().Append(ctx, record); err != nil {
		s.log().Error("recording measurement failed",
			"corridor", c.Key(), "error", err)
		return metrics.SweepUnrecorded, nil
	}

	// The run is stored, so the corridor's structure can now be compared
	// with the run before it. A corridor moving from DIRECT to NO-MARKET
	// is the single most important event this monitor can observe, and
	// until now nothing called the detector that finds it: the change was
	// visible only by diffing stored records by hand.
	s.reportTransition(ctx, c.Key())
	return metrics.SweepOK, nil
}

func (s *Scheduler) issuerDriftBaseline(ctx context.Context, corridor string) *checks.IssuerDriftBaseline {
	record, err := s.store().Latest(ctx, corridor)
	if err != nil {
		s.log().Warn("loading issuer drift baseline failed", "corridor", corridor, "error", err)
		return nil
	}
	if record == nil {
		return nil
	}
	return checks.IssuerDriftBaselineFromChecks(record.Checks)
}

// underlyingPair names the pair a DERIVATIVE corridor's paths traverse, when
// the ladder identified exactly one. It mirrors the server's helper: a book
// metric should measure the market that exists rather than report "no book" for
// a corridor whose liquidity sits one hop away, and more than one dependency
// stays empty because there is no single pair to measure.
func underlyingPair(res *route.LadderResult) asset.Asset {
	if res == nil || res.Integrity != route.IntegrityDerivative || len(res.DependsOn) != 1 {
		return asset.Asset{}
	}
	return res.DependsOn[0]
}

// reportTransition logs the corridor's most recent structural change, when
// the store records one.
//
// The comparison is the two most recent stored runs, so a corridor whose
// structure has not moved says nothing at all. The absence of a transition
// is the normal case, not a missing observation, and logging it would bury
// the events that matter.
//
// UNKNOWN is excluded by the detector itself, so a transient upstream
// failure cannot raise a structural alarm here: the same reasoning that
// keeps a monitor that cries wolf from being muted.
func (s *Scheduler) reportTransition(ctx context.Context, corridor string) {
	transition, err := runstore.DetectLatestTransition(ctx, s.store(), corridor)
	if err != nil {
		// A failed read is not a failed measurement. The record is
		// stored, and the next sweep will compare against it.
		s.log().Error("integrity transition check failed",
			"corridor", corridor, "error", err)
		return
	}
	if transition == nil {
		return
	}
	s.log().Warn("corridor integrity transition",
		"corridor", corridor,
		"previous_integrity", transition.PreviousIntegrity,
		"current_integrity", transition.CurrentIntegrity,
		"transition_type", transition.TransitionType.String(),
		"previous_run_at", transition.PreviousRunAt.UTC().Format(time.RFC3339),
		"current_run_at", transition.CurrentRunAt.UTC().Format(time.RFC3339),
		"previous_depends_on", transition.PreviousDependsOn,
		"current_depends_on", transition.CurrentDependsOn)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
