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

// DefaultCorridors are every registered corridor destination.
//
// The sweep covers the whole registry so that no verified corridor is
// measured in one place and served in another: a destination registered in
// asset is a destination the monitor prices. The case-study trio (USDC-NGNC
// DIRECT, USDC-GHSC DERIVATIVE, USDC-KESC NO-MARKET) still exercises each
// integrity state, and the remaining registered corridors extend the same
// coverage to every verified destination token.
func DefaultCorridors() []Corridor {
	return []Corridor{
		{Send: asset.USDC(), Receive: asset.NGNC(), ReferenceBase: "USD", ReferenceQuote: "NGN"},
		{Send: asset.USDC(), Receive: asset.GHSC(), ReferenceBase: "USD", ReferenceQuote: "GHS"},
		{Send: asset.USDC(), Receive: asset.KESC(), ReferenceBase: "USD", ReferenceQuote: "KES"},
		{Send: asset.USDC(), Receive: asset.NGNT(), ReferenceBase: "USD", ReferenceQuote: "NGN"},
		{Send: asset.USDC(), Receive: asset.USDZ(), ReferenceBase: "USD", ReferenceQuote: "USD"},
		{Send: asset.USDC(), Receive: asset.ZARZ(), ReferenceBase: "USD", ReferenceQuote: "ZAR"},
		{Send: asset.USDC(), Receive: asset.EURMTL(), ReferenceBase: "USD", ReferenceQuote: "EUR"},
		{Send: asset.USDC(), Receive: asset.PYUSD(), ReferenceBase: "USD", ReferenceQuote: "USD"},
	}
}

// DefaultInterval is how often corridors are measured.
//
// Six hours. The corridors measured so far move on the scale of days — the
// 2026-08-04 and 2026-08-08 runs differ by about a percent — so a shorter
// interval would buy resolution nobody needs while multiplying load on a
// shared public service. One run is roughly three dozen Horizon calls per
// corridor; at this cadence that is negligible for Horizon and comfortably
// inside both rate providers' free tiers once refrate.Cached is in the stack.
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
		if err := s.measure(ctx, c); err != nil {
			failures++
			s.log().Error("corridor measurement failed",
				"corridor", c.Key(), "error", err)
		}
	}

	if failures == len(s.corridors()) && failures > 0 {
		return fmt.Errorf("monitor: all %d corridors failed to measure", failures)
	}
	return nil
}

// measure runs one corridor's ladder and appends the result.
func (s *Scheduler) measure(ctx context.Context, c Corridor) error {
	started := s.now()
	engine := s.Engine
	var recorder *snapshot.Recorder
	if s.SnapshotDir != "" {
		if s.SnapshotEngineFactory == nil {
			return fmt.Errorf("monitor: snapshot directory configured without a snapshot engine factory")
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
			return fmt.Errorf("monitor: snapshot engine factory returned nil for %s", c.Key())
		}
	}

	result, err := engine.Ladder(ctx, route.LadderRequest{
		SendAsset:      c.Send,
		ReceiveAsset:   c.Receive,
		Sizes:          route.DefaultSizes,
		ReferenceBase:  c.ReferenceBase,
		ReferenceQuote: c.ReferenceQuote,
	})
	if err != nil {
		return fmt.Errorf("measuring %s: %w", c.Key(), err)
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
		return fmt.Errorf("monitor: snapshot checks require a recorder-backed checks factory")
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
				return fmt.Errorf("monitor: refusing to record incomplete snapshot for %s at size %s",
					c.Key(), rung.SendAmount)
			}
		}
		hash, err := snapshot.SaveContentAddressed(s.SnapshotDir, recorder)
		if err != nil {
			return fmt.Errorf("monitor: recording snapshot artifact for %s: %w", c.Key(), err)
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
	record.Reference.ScoredAgainst = ref.Source

	s.log().Info("corridor measured",
		"corridor", c.Key(),
		"integrity", result.Integrity.String(),
		"floor_loss_pct", result.Floor.StringFixed(2),
		"worst_loss_pct", result.WorstLoss.StringFixed(2),
		"scored", ref.Scorable(),
		"reference", ref.Source,
		"agreement", ref.Agreement.String(),
		"took", s.now().Sub(started).Round(time.Millisecond).String())

	// A storage failure must not lose the measurement 
}
