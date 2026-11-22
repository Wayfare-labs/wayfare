package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/Wayfare-labs/wayfare/anchor"
	"github.com/Wayfare-labs/wayfare/asset"
)

// registryOutcome is the verdict on one registration entry after re-reading
// the issuer's stellar.toml.
//
// The three outcomes are deliberately distinct. "Still verified" and "changed"
// both mean the document was retrieved and read; "unreachable" means it was
// not, so nothing can be said about the entry either way. Collapsing an
// unreachable document into "changed" would report drift that was never
// observed — the same category error the project refuses everywhere else, where
// an unknown is never dressed up as a finding.
type registryOutcome int

const (
	// registryStillVerified means the document was read and still lists the
	// registered code and issuer, declaring the registered SEP-1 status.
	registryStillVerified registryOutcome = iota
	// registryChanged means the document was read and does not corroborate
	// the registration: the entry is gone, its issuer moved, its status
	// differs, or the document itself no longer parses.
	registryChanged
	// registryUnreachable means the document could not be retrieved at all,
	// so no claim is made either way.
	registryUnreachable
)

// String renders the outcome the way the report prints it.
func (o registryOutcome) String() string {
	switch o {
	case registryStillVerified:
		return "still verified"
	case registryChanged:
		return "changed"
	case registryUnreachable:
		return "unreachable"
	default:
		return "unknown"
	}
}

// registryCheck is the result of re-checking one registry entry.
type registryCheck struct {
	Entry asset.Entry
	// Outcome is the verdict for this entry.
	Outcome registryOutcome
	// Detail explains a changed or unreachable outcome. It is empty for a
	// still-verified entry, where the registration speaks for itself.
	Detail string
	// TOMLURL is the document the verdict was read from. It is recorded even
	// when the fetch failed, so the report names what was attempted.
	TOMLURL string
}

// verifyRegistry re-fetches the stellar.toml for every entry and reports
// whether each one still matches code, issuer and status.
//
// # What it compares
//
// The registry records three facts about an asset: its code, its issuing
// account and the SEP-1 status the issuer declared when the entry was written.
// This re-reads the issuer's own document and checks all three. Code alone is
// never enough — anyone can issue a token called "NGNC" — so a code found under
// a different issuer is a change, not a match.
//
// A status field missing from the document is read as "live", the same default
// anchor.Currency.Live applies, because SEP-1 defines no separate "absent"
// state. That is what keeps PYUSD, whose issuer declares no status, verified.
//
// # Why one fetch per domain
//
// NGNC, GHSC and KESC are three entries in one document, as are USDZ and ZARZ.
// The document is fetched once per domain and reused, so the report reflects one
// read of one document rather than three reads that could disagree, and an
// issuer is not asked for the same file five times.
//
// # What it does not do
//
// It does not write anything, does not need the run store, and does not update
// the registry. A changed entry is reported for a human to correct in known.go;
// the compile-time registry stays the source of truth, and this command is how
// its aging verification dates get checked.
func verifyRegistry(ctx context.Context, entries []asset.Entry, resolver *anchor.Resolver) []registryCheck {
	if resolver == nil {
		resolver = &anchor.Resolver{}
	}

	// Cache the fetch per domain, including the failure: several entries
	// share a domain, and a domain that was unreachable is still unreachable
	// for the next entry in the same run.
	type fetched struct {
		profile *anchor.Profile
		err     error
	}
	documents := make(map[string]fetched)

	out := make([]registryCheck, 0, len(entries))
	for _, e := range entries {
		domain := strings.TrimSpace(e.HomeDomain)
		if domain == "" {
			// ValidateEntry forbids this, but a report is a better answer
			// than a panic if the invariant ever slips.
			out = append(out, registryCheck{
				Entry:   e,
				Outcome: registryUnreachable,
				Detail:  "the entry records no home domain, so no stellar.toml URL could be fetched",
			})
			continue
		}

		doc, ok := documents[domain]
		if !ok {
			profile, err := resolver.Resolve(ctx, domain)
			doc = fetched{profile: profile, err: err}
			documents[domain] = doc
		}

		tomlURL := anchor.TOMLURL(domain)
		if doc.err != nil || doc.profile == nil {
			out = append(out, registryCheck{
				Entry:   e,
				Outcome: registryUnreachable,
				Detail:  fetchError(doc.err),
				TOMLURL: tomlURL,
			})
			continue
		}

		check := checkRegistryEntry(e, doc.profile)
		check.TOMLURL = tomlURL
		out = append(out, check)
	}
	return out
}

// fetchError renders why a document could not be used, without assuming the
// resolver returned an error alongside nothing.
func fetchError(err error) string {
	if err != nil {
		return err.Error()
	}
	return "the resolver returned no document and no error"
}

// checkRegistryEntry compares one entry against the document that should
// declare it.
func checkRegistryEntry(e asset.Entry, p *anchor.Profile) registryCheck {
	result := registryCheck{Entry: e}

	// A document that was retrieved but does not parse was reached, so it
	// is not unreachable; it also cannot corroborate anything, so it is not
	// still verified. It is reported as changed, and the reason names the
	// parse failure rather than pretending the entry moved.
	if p.Malformed {
		result.Outcome = registryChanged
		result.Detail = "stellar.toml is not valid TOML, so the entry could not be confirmed: " + p.MalformedReason
		return result
	}

	var sameCode *anchor.Currency
	for i := range p.TOML.Currencies {
		c := p.TOML.Currencies[i]
		if !strings.EqualFold(strings.TrimSpace(c.Code), e.Code) {
			continue
		}
		if strings.TrimSpace(c.Issuer) == strings.TrimSpace(e.Issuer) {
			return checkRegistryStatus(e, c)
		}
		if sameCode == nil {
			// Keep the first same-code entry from another issuer so a
			// moved issuer can be named specifically instead of the
			// vaguer "entry is missing".
			cp := c
			sameCode = &cp
		}
	}

	result.Outcome = registryChanged
	if sameCode != nil {
		result.Detail = fmt.Sprintf(
			"code %s is now declared by issuer %s, not the registered %s",
			e.Code, strings.TrimSpace(sameCode.Issuer), e.Issuer)
		return result
	}
	result.Detail = fmt.Sprintf(
		"stellar.toml declares no [[CURRENCIES]] entry with code %s", e.Code)
	return result
}

// checkRegistryStatus compares the declared SEP-1 status with the registered
// one, once code and issuer are known to match.
func checkRegistryStatus(e asset.Entry, c anchor.Currency) registryCheck {
	result := registryCheck{Entry: e}

	declared := strings.TrimSpace(c.Status)
	if declared == "" {
		// SEP-1 has no "absent" status; anchor.Currency.Live reads an
		// omitted status as live, and this comparison has to agree with
		// the rest of the project or an asset would be live for the
		// router and unverified for the report.
		declared = "live"
	}
	registered := strings.TrimSpace(e.Status)

	if !strings.EqualFold(declared, registered) {
		result.Outcome = registryChanged
		result.Detail = fmt.Sprintf(
			"registry records status %q, stellar.toml declares %q", registered, declared)
		return result
	}

	result.Outcome = registryStillVerified
	return result
}

// printRegistryReport writes one line per asset and a summary, and returns the
// number of changed and unreachable entries so the caller can set an exit code.
func printRegistryReport(w io.Writer, checks []registryCheck) (changed, unreachable int) {
	for _, c := range checks {
		switch c.Outcome {
		case registryChanged:
			changed++
		case registryUnreachable:
			unreachable++
		}

		// The outcome leads the line so a reader scanning a long report
		// sees the verdicts aligned, and so a grep for one outcome finds
		// exactly those assets.
		fmt.Fprintf(w, "%-14s %-8s %s\n", c.Outcome, c.Entry.Code, c.Entry.Issuer)
		if c.Detail != "" {
			fmt.Fprintf(w, "               %s\n", c.Detail)
		}
		if c.TOMLURL != "" {
			fmt.Fprintf(w, "               %s\n", c.TOMLURL)
		}
	}

	verified := len(checks) - changed - unreachable
	fmt.Fprintf(w, "\n%d of %d assets still verified", verified, len(checks))
	if changed > 0 {
		fmt.Fprintf(w, ", %d changed", changed)
	}
	if unreachable > 0 {
		fmt.Fprintf(w, ", %d unreachable", unreachable)
	}
	fmt.Fprintln(w)

	return changed, unreachable
}

// verifyRegistryCmd runs -verify-registry: re-fetch every registered issuer's
// stellar.toml, print a per-asset verdict and exit.
//
// Exit codes follow -verify-store and -check-fresh: 0 means every entry still
// matches its issuer's document, 1 means at least one entry changed, 2 means
// at least one document could not be fetched and nothing changed. A definite
// finding outranks an unknown when both occur, because the finding is the
// actionable one. Nothing is fetched with a parallel fan-out: six issuers do not
// need it, and a report that read the same document at different moments would
// be harder to reason about.
func verifyRegistryCmd(ctx context.Context, entries []asset.Entry, resolver *anchor.Resolver, w io.Writer, logger *slog.Logger) int {
	if len(entries) == 0 {
		logger.Error("-verify-registry found no registry entries to check")
		return 2
	}

	checks := verifyRegistry(ctx, entries, resolver)
	changed, unreachable := printRegistryReport(w, checks)

	switch {
	case changed > 0:
		return 1
	case unreachable > 0:
		return 2
	default:
		return 0
	}
}
