package checks

import (
	"context"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/anchor"
	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
)

// Runner resolves a subject and runs a set of checks against it.
//
// It sits between the engine and the individual checks so that resolving an
// anchor's stellar.toml happens once per corridor rather than once per check.
// Several checks read the same document, and fetching it repeatedly would
// multiply requests at a third party for no gain.
type Runner struct {
	// Checks are run in order. Empty means Default().
	Checks []Check

	// Metrics are measured after the checks, over the same subject. Nil means
	// DefaultMetrics; a non-nil empty slice means "measure nothing", which is
	// how a caller opts out of a set that costs upstream requests.
	//
	// Metrics are only run by ForCorridor: a corridor metric answers a
	// question about a pair, so the single-asset ForAsset path has nothing to
	// measure and stays checks-only. See docs/adding-a-metric.md.
	Metrics []Metric

	// Resolver fetches an anchor's stellar.toml. Nil means a default
	// resolver; supplying one with a snapshot-backed HTTP client is what
	// makes a whole sweep replayable.
	Resolver *anchor.Resolver

	// HTTPClient is handed to checks that need one, so a single transport —
	// and therefore a single snapshot — covers the sweep.
	HTTPClient *http.Client

	// HorizonURL is passed to on-chain checks. Empty means mainnet.
	HorizonURL string

	// Timeout bounds the whole sweep for one corridor. Zero means 20s.
	Timeout time.Duration

	// profileCache holds stellar.toml profiles already resolved this sweep,
	// keyed by home domain. NGNC, GHSC and KESC share one issuer and domain,
	// so a sweep measuring all three fetches the document once instead of
	// once per asset. A cache hit preserves the profile's original FetchedAt.
	profileCache map[string]*anchor.Profile
}

// Default returns the project's built-in checks.
//
// Deliberately small. These checks are the worked examples contributors copy,
// and a long default list would make the set feel closed rather than like the
// beginning of a backlog.
func (r *Runner) Default() []Check {
	return []Check{
		AnchorAssetISO4217{},
		SEP12ServerPublished{},
		SEP31Info{HTTPClient: r.client()},
		SEP10EndpointResponds{HTTPClient: r.client()},
		SEP6InfoListsAsset{HTTPClient: r.client()},
		SEP24InfoListsAsset{HTTPClient: r.client()},
		SEP38QuoteServerPublished{},
		IssuerAuthFlags{HorizonURL: r.HorizonURL, HTTPClient: r.client()},
		IssuerFlagImmutability{HorizonURL: r.HorizonURL, HTTPClient: r.client()},
		IssuerDrift{HorizonURL: r.HorizonURL, HTTPClient: r.client()},
		HomeDomainRoundTrip{HorizonURL: r.HorizonURL, HTTPClient: r.client(), Resolver: r.resolver()},
	}
}

// Corridor is a measured corridor a runner is qualifying, beyond the pair
// itself.
//
// It carries what the ladder already established so a metric can be told it
// rather than rediscovering it with more upstream requests. Every field is
// optional in the same sense Subject's are: empty means unknown, and a metric
// reports undetermined rather than inferring.
type Corridor struct {
	// Send and Receive are the corridor's pair.
	Send    asset.Asset
	Receive asset.Asset

	// Integrity is the ladder's structural classification — "DIRECT",
	// "DERIVATIVE", "NO-MARKET", or empty when the ladder did not
	// establish one.
	Integrity string

	// Underlying is the direct pair a DERIVATIVE corridor's paths actually
	// traverse, when the caller has one to offer. Empty leaves a DERIVATIVE
	// corridor's book metrics undetermined rather than guessing a pair.
	Underlying asset.Asset

	// Reference is the benchmark the ladder scored against. Metrics derived
	// against a reference report undetermined when it is unscorable, and
	// short-circuit before any network call.
	Reference refrate.Rate

	// Baseline is the issuer drift observation stored by the previous run of
	// this corridor, if any. With it the issuer.drift check reports what
	// changed since then; nil means there is nothing to compare against yet.
	Baseline *IssuerDriftBaseline
	// FloorSize and FloorEffectiveRate identify the smallest priced ladder
	// rung. They are carried into the metric set rather than fetched again.
	FloorSize          decimal.Decimal
	FloorEffectiveRate decimal.Decimal
}

func (r *Runner) checks() []Check {
	if len(r.Checks) > 0 {
		return r.Checks
	}
	return r.Default()
}

// metrics returns the metrics a sweep runs: the configured set, or the default
// set when none was configured.
func (r *Runner) metrics(c Corridor) []Metric {
	if r.Metrics != nil {
		return r.Metrics
	}
	return r.DefaultMetrics(c)
}

// DefaultMetrics returns the metrics a corridor sweep runs when none were
// configured.
//
// Deliberately small: four order-book metrics plus a free metric using the
// ladder's existing floor rung. The ladder has already spent a dozen
// pathfinding round trips by the time a corridor reaches this point, so the
// order-book metrics read the book:
// spread, concentration, observed depth, and deviation from the reference
// mid. The floor-loss metric reuses the measured smallest rung and makes no
// additional upstream request.
//
// PriceImpactMetric and DepthMetric.RunExecutable are excluded on purpose: both
// sweep pathfinding at every probed size, and repeating a ladder that was just
// priced is a cost a caller should opt into rather than inherit.
func (r *Runner) DefaultMetrics(c Corridor) []Metric {
	d := r.dex()
	return []Metric{
		SpreadMetric{DEX: d},
		ConcentrationMetric{DEX: d},
		DepthMetric{DEX: d},
		DeviationMetric{DEX: d, Reference: c.Reference},
		FloorLossMetric{
			Size: c.FloorSize, EffectiveRate: c.FloorEffectiveRate,
			Reference: c.Reference,
		},
	}
}

// dex returns the Horizon client metrics use, built from the same transport the
// checks use so a snapshot replayer serves metrics from the recording too.
func (r *Runner) dex() *dex.Client {
	return &dex.Client{HorizonURL: r.HorizonURL, HTTPClient: r.client()}
}

func (r *Runner) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return 20 * time.Second
}

func (r *Runner) resolver() *anchor.Resolver {
	if r.Resolver != nil {
		return r.Resolver
	}
	return &anchor.Resolver{HTTPClient: r.client()}
}

// client returns the transport checks use.
//
// The default is guarded, because every URL reached through it — a
// stellar.toml, a declared auth endpoint — is published by the party being
// audited. An explicitly supplied client is used unchanged, which is what lets
// a snapshot replayer serve recorded bytes with no network at all.
func (r *Runner) client() *http.Client {
	if r.HTTPClient != nil {
		return r.HTTPClient
	}
	return GuardedClient(15 * time.Second)
}

// ForAsset runs every check against one asset, resolving its anchor first.
//
// A failure to resolve the anchor is not a failure of the sweep: the checks
// that need a profile report themselves undetermined, and the ones that do not
// — the on-chain checks — still run and still produce findings. Returning
// nothing because a document could not be fetched would discard observations
// that were perfectly obtainable.
func (r *Runner) ForAsset(ctx context.Context, a asset.Asset) *Findings {
	return r.ForAssetWithBaseline(ctx, a, nil)
}

// ForAssetWithBaseline runs checks with the stored issuer drift observation,
// if one exists, as the baseline for this asset.
func (r *Runner) ForAssetWithBaseline(ctx context.Context, a asset.Asset, baseline *IssuerDriftBaseline) *Findings {
	ctx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()

	f := &Findings{}
	for _, res := range RunAll(ctx, r.checksWithBaseline(baseline), r.subject(ctx, a)) {
		f.Add(res)
	}
	return f
}

// ForCorridor runs every check and every metric against one measured corridor.
//
// Checks still run against the receive asset, exactly as ForAsset runs them: a
// check asks a question about the anchor that issues the asset the user ends up
// holding. Metrics run against the pair, because that is the subject a quantity
// has. Both land in the same Findings, and neither can move the corridor's
// headline — the composition rule is enforced by Findings having no path back
// into the engine (ADR 002).
func (r *Runner) ForCorridor(ctx context.Context, c Corridor) *Findings {
	ctx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()

	subject := r.subject(ctx, c.Receive)
	subject.Send = c.Send
	subject.Receive = c.Receive
	subject.Integrity = c.Integrity
	subject.Underlying = c.Underlying
	subject.ReferenceAgreement = c.Reference.Agreement.String()

	f := &Findings{}
	for _, res := range RunAll(ctx, r.checksWithBaseline(c.Baseline), subject) {
		f.Add(res)
	}
	for _, m := range r.metrics(c) {
		f.AddMetric(RunMetric(ctx, m, subject))
	}
	return f
}

// subject resolves what the checks and metrics need to know about an asset's
// anchor once per sweep.
func (r *Runner) subject(ctx context.Context, a asset.Asset) Subject {
	subject := Subject{Asset: a}

	// Resolve the publishing domain only when the association was verified.
	// A guessed domain would send every TOML-reading check at somebody
	// else's document and report confident findings about the wrong anchor.
	if domain, ok := asset.HomeDomain(a); ok {
		subject.Domain = domain
		if r.profileCache == nil {
			r.profileCache = make(map[string]*anchor.Profile)
		}
		if profile, cached := r.profileCache[domain]; cached {
			// Sweep-scoped cache hit: reuse the profile resolved earlier
			// for another asset on the same domain. One fetch per domain
			// per sweep, and FetchedAt stays at the original fetch time.
			subject.Profile = profile
		} else if profile, err := r.resolver().Resolve(ctx, domain); err == nil {
			subject.Profile = profile
			r.profileCache[domain] = profile
		}
		// A resolve failure is left as a nil profile on purpose. The
		// checks that need one say so individually, with a reason, which
		// is more useful than one sweep-level error standing in for
		// several different questions.
	}
	return subject
}

// checksWithBaseline returns the check set with the issuer drift baseline, if
// one is given, installed on the issuer.drift check. The runner's own check
// set is never modified: each call works on a copy.
func (r *Runner) checksWithBaseline(baseline *IssuerDriftBaseline) []Check {
	checks := append([]Check(nil), r.checks()...)
	if baseline == nil {
		return checks
	}
	for i, check := range checks {
		switch drift := check.(type) {
		case IssuerDrift:
			drift.Baseline = baseline
			checks[i] = drift
		case *IssuerDrift:
			if drift != nil {
				copy := *drift
				copy.Baseline = baseline
				checks[i] = copy
			}
		}
	}
	return checks
}
