package checks

import (
	"context"
	"net/http"
	"time"

	"github.com/Wayfare-labs/wayfare/anchor"
	"github.com/Wayfare-labs/wayfare/asset"
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
}

// Default returns the reference checks.
//
// Deliberately small. These three are the worked examples contributors copy,
// and a long default list would make the set feel closed rather than like the
// beginning of a backlog.
func (r *Runner) Default() []Check {
	return []Check{
		AnchorAssetISO4217{},
		SEP10EndpointResponds{HTTPClient: r.client()},
		SEP24InfoListsAsset{HTTPClient: r.client()},
		SEP38QuoteServerPublished{},
		IssuerAuthFlags{HorizonURL: r.HorizonURL, HTTPClient: r.client()},
		IssuerFlagImmutability{HorizonURL: r.HorizonURL, HTTPClient: r.client()},
		HomeDomainRoundTrip{HorizonURL: r.HorizonURL, HTTPClient: r.client(), Resolver: r.resolver()},
	}
}

func (r *Runner) checks() []Check {
	if len(r.Checks) > 0 {
		return r.Checks
	}
	return r.Default()
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
	ctx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()

	subject := Subject{Asset: a}

	// Resolve the publishing domain only when the association was verified.
	// A guessed domain would send every TOML-reading check at somebody
	// else's document and report confident findings about the wrong anchor.
	if domain, ok := asset.HomeDomain(a); ok {
		subject.Domain = domain
		if profile, err := r.resolver().Resolve(ctx, domain); err == nil {
			subject.Profile = profile
		}
		// A resolve failure is left as a nil profile on purpose. The
		// checks that need one say so individually, with a reason, which
		// is more useful than one sweep-level error standing in for
		// several different questions.
	}

	f := &Findings{}
	for _, res := range RunAll(ctx, r.checks(), subject) {
		f.Add(res)
	}
	return f
}
