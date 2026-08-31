package refrate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// Agreement describes how two reference providers related on one pair.
type Agreement int

const (
	// AgreementSingle means only one provider answered. The rate is usable
	// and uncorroborated, and says so rather than implying a cross-check
	// happened.
	AgreementSingle Agreement = iota

	// AgreementAgree means both answered within DivergenceAgree.
	AgreementAgree

	// AgreementDisagree means the two differ by more than DivergenceAgree
	// but less than DivergenceMalfunction: a genuine disagreement about the
	// rate, scored conservatively.
	AgreementDisagree

	// AgreementStale means the two answers describe materially different
	// moments, so the gap measures lag rather than disagreement.
	AgreementStale

	// AgreementMalfunction means the two differ by more than
	// DivergenceMalfunction. At that distance one feed is not disagreeing,
	// it is broken, and nothing is scored against either.
	AgreementMalfunction
)

func (a Agreement) String() string {
	switch a {
	case AgreementAgree:
		return "AGREE"
	case AgreementDisagree:
		return "DISAGREE"
	case AgreementStale:
		return "STALE"
	case AgreementMalfunction:
		return "MALFUNCTION"
	default:
		return "SINGLE"
	}
}

// Divergence thresholds, as percentages.
//
// # Why 2% for agreement
//
// Two feeds quoting the same official rate for the same pair should agree to
// well inside one percent; they differ only by aggregation lag and source mix,
// not by economics. Measured live on 2026-08-21, exchangerate-api and
// currency-api quoted USD/NGN at 1348.0585 and 1350.2568 — 0.16% apart. Two
// percent is therefore an order of magnitude above observed normal, which
// makes crossing it a real signal rather than noise.
//
// # Why 10% for malfunction
//
// The conservative-scoring rule below deliberately prefers the mid that makes
// a corridor look worse. That is right for a genuine disagreement and wrong
// for a broken feed: a provider that is weeks stale, quoting the wrong pair,
// or off by a decimal place would win every time, and the project's bias
// against flattering a corridor would become a mechanism for exaggerating one.
//
// Ten percent is where disagreement stops being a plausible description. A
// feed a fortnight stale on an ordinary currency does not move that far, so a
// gap that size means the two are measuring different things — a different
// pair, an official rate against a parallel-market one, or a misplaced
// decimal. Wayfare cannot adjudicate which of those is the benchmark, and a
// verdict would then be an artefact of that choice rather than a measurement.
// So it refuses to score, and says why.
var (
	DivergenceAgree       = decimal.NewFromInt(2)
	DivergenceMalfunction = decimal.NewFromInt(10)
)

// StaleGap is how far apart two rates' as-of moments may be before their
// difference is read as lag rather than as disagreement.
//
// Both providers publish roughly daily, so a gap beyond two days means one is
// beyond its own refresh cycle.
var StaleGap = 48 * time.Hour

// errorClass labels a provider error for degradation reporting.
type errorClass int

const (
	// errClassUnknown is the fallback for errors that do not match any
	// typed error in the taxonomy.
	errClassUnknown errorClass = iota

	// errClassUnavailable means the provider did not answer at all —
	// network failure, timeout, or HTTP-level error.
	errClassUnavailable

	// errClassUnparseable means the provider answered with a body that
	// could not be interpreted as a rate.
	errClassUnparseable
)

// classifyError identifies which category of provider failure occurred.
func classifyError(err error) errorClass {
	var unavailable *ErrUnavailable
	if errors.As(err, &unavailable) {
		return errClassUnavailable
	}
	var unparseable *ErrUnparseable
	if errors.As(err, &unparseable) {
		return errClassUnparseable
	}
	return errClassUnknown
}

// errorDescription renders an error class as a human-readable phrase for use
// in degradation notes.
func errorDescription(err error) string {
	switch classifyError(err) {
	case errClassUnavailable:
		return "unavailable"
	case errClassUnparseable:
		return "returned an unparseable response"
	default:
		return "unavailable"
	}
}

// Cross queries two providers and reports whether they agree.
//
// # Why not average them
//
// A blended rate names no provider. Every figure Wayfare publishes has to be
// traceable to a source a reader can check, and the mean of two feeds is
// exactly the unattributable number this project refuses to produce. So one
// mid is chosen, and the record says which.
//
// # Which one is chosen
//
// On agreement, the primary. On genuine disagreement, whichever produces the
// *higher* loss — the more conservative reading — because the failure this
// project most needs to avoid is flattering a corridor. On malfunction,
// neither: see the threshold rationale above.
//
// Choosing the higher-loss mid means choosing the *larger* mid, since loss is
// measured as how far the achieved rate falls below the benchmark.
type Cross struct {
	Primary   Provider
	Secondary Provider
}

// Name identifies the composite.
func (c *Cross) Name() string {
	if c.Secondary == nil {
		return c.Primary.Name()
	}
	return c.Primary.Name() + "+" + c.Secondary.Name()
}

// Rate fetches from both providers and reconciles them.
//
// A failing secondary degrades to a single-source rate and says so through
// Agreement; it is never substituted for, and never silently ignored. A
// failing primary is an error only if the secondary also fails.
func (c *Cross) Rate(ctx context.Context, base, quote string) (Rate, error) {
	if c.Primary == nil {
		return Rate{}, fmt.Errorf("refrate: Cross requires a primary provider")
	}

	primary, primaryErr := c.Primary.Rate(ctx, base, quote)

	if c.Secondary == nil {
		if primaryErr != nil {
			return Rate{}, primaryErr
		}
		return singleSource(primary, "", nil), nil
	}

	secondary, secondaryErr := c.Secondary.Rate(ctx, base, quote)

	switch {
	case primaryErr != nil && secondaryErr != nil:
		return Rate{}, fmt.Errorf(
			"refrate: no reference rate for %s/%s: %s was %s (%v); %s was %s (%v)",
			base, quote,
			c.Primary.Name(), errorDescription(primaryErr), primaryErr,
			c.Secondary.Name(), errorDescription(secondaryErr), secondaryErr)

	case primaryErr != nil:
		return singleSource(secondary, c.Primary.Name(), primaryErr), nil

	case secondaryErr != nil:
		return singleSource(primary, c.Secondary.Name(), secondaryErr), nil
	}

	return reconcile(primary, secondary), nil
}

// singleSource records that only one provider produced the rate being returned.
//
// It is the one place the uncorroborated state is assembled, because that
// state arises three ways — no secondary is configured, the primary failed, or
// the secondary failed — and each of them must state the same thing: the rate
// is the survivor's, no cross-check happened, and nothing was filled in for
// the provider that is absent.
//
// Two properties are enforced here rather than left to the callers:
//
//   - The cross-check fields are cleared. AgreementSingle claims that no
//     second observation exists, so carrying a secondary mid or a divergence
//     alongside it would report a cross-check that never ran. Nothing sets
//     these on a plain provider today, and a composite that did would still be
//     rendered honestly.
//   - A zero mid is refused. reconcile has treated a zero as a broken feed
//     rather than as a rate, but it only runs when both providers answer, so
//     every single-source path returned before the guard. That made the
//     likeliest production path — one provider answering, often all that a free
//     tier leaves standing — the only way a zero reached a verdict, where it
//     reads as a benchmark of nothing: every route then scores against zero,
//     and the corridor's loss becomes a function of the quote alone. A zero is
//     an absent figure, so it is reported as unscoreable with the same reason
//     reconcile gives, and never as a usable rate.
//
// failedName and failedErr describe the provider that did not answer, and are
// empty when none was configured; a deployment with one provider has nothing
// to apologise for, so it carries no note.
func singleSource(survivor Rate, failedName string, failedErr error) Rate {
	survivor.SecondaryMid = decimal.Zero
	survivor.SecondarySource = ""
	survivor.SecondaryAsOf = time.Time{}
	survivor.DivergencePct = decimal.Zero

	if survivor.Mid.IsZero() {
		survivor.Agreement = AgreementMalfunction
		survivor.Note = fmt.Sprintf(
			"not scored: %s quoted %s; a zero rate is a broken feed", survivor.Source, survivor.Mid)
		return survivor
	}

	survivor.Agreement = AgreementSingle
	if failedName != "" {
		survivor.Note = fmt.Sprintf(
			"uncorroborated: %s was %s (%v)", failedName, errorDescription(failedErr), failedErr)
	}
	return survivor
}

// reconcile applies the three-band rule to two successful answers.
func reconcile(primary, secondary Rate) Rate {
	out := primary
	out.SecondaryMid = secondary.Mid
	out.SecondarySource = secondary.Source
	out.SecondaryAsOf = secondary.AsOf

	// Guard the division before anything else: a zero mid from either side
	// is a broken feed, not a 100% disagreement.
	if primary.Mid.IsZero() || secondary.Mid.IsZero() {
		out.Agreement = AgreementMalfunction
		out.Note = fmt.Sprintf(
			"not scored: %s quoted %s and %s quoted %s; a zero rate is a broken feed",
			primary.Source, primary.Mid, secondary.Source, secondary.Mid)
		return out
	}

	// Divergence relative to the smaller mid, so the figure does not depend
	// on which provider happens to be primary.
	lo, hi := primary.Mid, secondary.Mid
	if hi.LessThan(lo) {
		lo, hi = hi, lo
	}
	out.DivergencePct = hi.Sub(lo).Div(lo).Mul(decimal.NewFromInt(100))

	switch {
	case out.DivergencePct.GreaterThan(DivergenceMalfunction):
		out.Agreement = AgreementMalfunction
		out.Note = fmt.Sprintf(
			"not scored: %s quoted %s and %s quoted %s, a %s%% gap. Beyond %s%% the two "+
				"are not disagreeing about the rate, they are measuring different things, "+
				"and no verdict derived from either would be a measurement.",
			primary.Source, primary.Mid, secondary.Source, secondary.Mid,
			out.DivergencePct.StringFixed(2), DivergenceMalfunction)
		return out

	case staleApart(primary, secondary):
		out.Agreement = AgreementStale
		out.Note = fmt.Sprintf(
			"%s is as of %s and %s is as of %s; the %s%% gap measures staleness rather "+
				"than disagreement",
			primary.Source, primary.AsOf.UTC().Format(time.RFC3339),
			secondary.Source, secondary.AsOf.UTC().Format(time.RFC3339),
			out.DivergencePct.StringFixed(2))
		// Still scored, against the fresher of the two.
		if secondary.AsOf.After(primary.AsOf) {
			out.Mid, out.Source, out.AsOf = secondary.Mid, secondary.Source, secondary.AsOf
			out.SecondaryMid, out.SecondarySource, out.SecondaryAsOf =
				primary.Mid, primary.Source, primary.AsOf
		}
		return out

	case out.DivergencePct.GreaterThan(DivergenceAgree):
		out.Agreement = AgreementDisagree
		// Conservative: the larger mid produces the larger loss.
		if secondary.Mid.GreaterThan(primary.Mid) {
			out.Mid, out.Source, out.AsOf = secondary.Mid, secondary.Source, secondary.AsOf
			out.SecondaryMid, out.SecondarySource, out.SecondaryAsOf =
				primary.Mid, primary.Source, primary.AsOf
		}
		out.Note = fmt.Sprintf(
			"providers disagree by %s%%; scored against %s (%s), the more conservative of the two",
			out.DivergencePct.StringFixed(2), out.Source, out.Mid)
		return out

	default:
		out.Agreement = AgreementAgree
		return out
	}
}

// staleApart reports whether two rates describe materially different moments.
func staleApart(a, b Rate) bool {
	if a.AsOf.IsZero() || b.AsOf.IsZero() {
		return false
	}
	gap := a.AsOf.Sub(b.AsOf)
	if gap < 0 {
		gap = -gap
	}
	return gap > StaleGap
}
