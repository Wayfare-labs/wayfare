#!/usr/bin/env bash
# Ask both reference-rate providers for every registered corridor's fiat and
# report any gap.
#
# Usage:
#   ./scripts/check-provider-coverage.sh
#
# Exit codes:
#   0 - Every registered corridor's fiat is served by both providers.
#   1 - Gap: a provider publishes no rate for a registered corridor's fiat.
#   2 - Inconclusive: a provider did not answer (network failure, rate limit,
#       missing prerequisite). Absence and failure are different findings, so
#       they never share an exit code.
#
# Why this check exists. Scoring a corridor requires a reference mid, and a
# reference mid requires *both* providers of refrate.Cross to answer: with one
# observation there is no cross-check, Agreement is SINGLE, and AGREE is
# unreachable — permanently, for every measurement of that fiat. The corridor
# still measures and still publishes, so a missing fiat is a quiet, permanent
# degradation rather than an error anyone sees in production. This asks the
# question at corridor-registration time instead, which is when it can still
# be acted on.
#
# Why a shell wrapper around a Go program. The corridor list is the registry
# in asset/known.go — the same source the monitor classifies corridors from —
# and the questions are asked through refrate.ExchangeRateAPI and
# refrate.CurrencyAPI, the same Provider implementations wayfared runs. That
# keeps the check measuring production endpoints, production parsing, and the
# production ErrNoRate semantics rather than a re-derivation of them that
# could drift. The helper program is written to a temporary directory under
# bin/ (gitignored, dot-prefixed so `go test ./...` never sees it) and removed
# on exit.
#
# Deliberately NOT part of `make offline-test` or CI: it queries live
# providers, which is exactly the network dependency docs/offline-testing.md
# exists to exclude. It is a step you run yourself when adding a corridor.
#
# Environment (testing only):
#   COVCHECK_ERAPI_URL       Override exchangerate-api's base URL.
#   COVCHECK_CURRENCYAPI_URL Override currency-api's base URL.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

die() { echo "provider coverage check FAILED: $*" >&2; exit 2; }

command -v go >/dev/null 2>&1 || die "go is required"

mkdir -p bin
work="$(mktemp -d "${PWD}/bin/.covcheck-XXXXXX")" || die "could not create a working directory under bin/"
trap 'rm -rf "$work"' EXIT

cat > "$work/main.go" <<'GOEOF'
// Command covcheck asks both reference-rate providers whether they serve the
// fiat peg of every registered corridor, and reports any gap.
//
// It is run by scripts/check-provider-coverage.sh, which writes it into a
// temporary directory inside the repository so it can import the asset and
// refrate packages directly. The registry supplies the corridor list and the
// providers are the same implementations wayfared runs, so the question asked
// here — "does this provider publish USD/<peg> for this corridor's fiat?" —
// is asked with the production endpoints, the production parsing, and the
// production no-rate semantics.
//
// Exit codes: 0 every fiat served by both providers; 1 a gap (ErrNoRate, a
// structural absence); 2 inconclusive (a provider did not answer).
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/refrate"
)

const (
	exitCovered = 0 // both providers serve every corridor fiat
	exitGap     = 1 // at least one provider publishes no rate for a fiat
	exitFailed  = 2 // at least one check did not complete; coverage unknown
)

// cell is one provider's answer for one fiat.
type cell struct {
	state string // ok | GAP | RATE-LIMITED | ERROR
	note  string // mid when ok, reason when not
}

func (c cell) ok() bool { return c.state == "ok" }

// memoTransport answers repeat GETs for the same URL from one recorded
// response, so each provider is fetched once no matter how many registered
// fiats are checked.
//
// Both providers key their whole universe of rates on the base currency:
// exchangerate-api returns every USD pair from one /latest/USD document and
// currency-api from one usd.json. Asking per fiat through the real Provider
// implementations is what keeps semantics identical to production, but it
// would re-fetch the same document once per fiat — needless load on the
// free tiers this project depends on, and needless exposure to the rate
// limits a burst would invite. Memoising at the transport layer gets one
// upstream request per document while every response still travels the
// production parse path.
type memoTransport struct {
	base    http.RoundTripper
	bodies  map[string][]byte
	status  map[string]int
	fetched int // requests actually sent upstream, reported for honesty
}

func (m *memoTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	key := req.URL.String()
	if body, ok := m.bodies[key]; ok {
		return &http.Response{
			StatusCode: m.status[key],
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}
	resp, err := m.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	m.bodies[key], m.status[key] = body, resp.StatusCode
	m.fetched++
	return &http.Response{
		StatusCode: resp.StatusCode,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     resp.Header,
		Request:    req,
	}, nil
}

// ask queries one provider for USD/peg and classifies the answer.
//
// The distinction the classification preserves is the one the rest of the
// project insists on: ErrNoRate means the provider answered and does not
// publish the fiat — a structural gap, reported as such — while everything
// else means the provider did not answer, and coverage is unknown rather
// than absent.
func ask(ctx context.Context, p refrate.Provider, peg string) cell {
	r, err := p.Rate(ctx, "USD", peg)
	switch {
	case err == nil:
		return cell{state: "ok", note: r.Mid.String()}

	case errorIs[*refrate.ErrNoRate](err):
		return cell{state: "GAP"}

	case errorIs[*refrate.ErrRateLimited](err):
		return cell{state: "RATE-LIMITED", note: err.Error()}

	case errorIs[*refrate.ErrUnavailable](err):
		return cell{state: "ERROR", note: "no answer: " + err.Error()}

	case errorIs[*refrate.ErrUnparseable](err):
		return cell{state: "ERROR", note: "unparseable response: " + err.Error()}

	default:
		return cell{state: "ERROR", note: err.Error()}
	}
}

func errorIs[T error](err error) bool {
	var target T
	return errors.As(err, &target)
}

func main() {
	// The providers log every non-200 and missing rate at Error level. Those
	// lines duplicate what the report below states, so the report is the only
	// surface here; wayfared keeps the logs where its own runs need them.
	refrate.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	transport := &memoTransport{
		base:   http.DefaultTransport,
		bodies: make(map[string][]byte),
		status: make(map[string]int),
	}
	// Same 10s per-request timeout the providers default to when handed no
	// client — this check asks the same questions under the same clock.
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport}

	primary := &refrate.ExchangeRateAPI{
		BaseURL: os.Getenv("COVCHECK_ERAPI_URL"),
		Client:  client,
	}
	secondary := &refrate.CurrencyAPI{
		BaseURL: os.Getenv("COVCHECK_CURRENCYAPI_URL"),
		Client:  client,
	}
	providers := []refrate.Provider{primary, secondary}

	// The corridor list is the registry: every entry with a Peg is a corridor
	// whose benchmark fiat must be published by both providers to be
	// cross-checked. USDC carries no peg — it is the settlement asset every
	// corridor starts from, not a corridor.
	corridorsByFiat := make(map[string][]string)
	for _, e := range asset.Registry() {
		if e.Peg == "" {
			continue
		}
		corridorsByFiat[e.Peg] = append(corridorsByFiat[e.Peg], e.Code)
	}
	fiats := make([]string, 0, len(corridorsByFiat))
	for peg := range corridorsByFiat {
		fiats = append(fiats, peg)
	}
	sort.Strings(fiats)
	for peg := range corridorsByFiat {
		sort.Strings(corridorsByFiat[peg])
	}

	fmt.Println("Reference-rate provider coverage for registered corridor fiats")
	fmt.Printf("queried %s, base USD against every registered corridor's Peg\n",
		time.Now().UTC().Format(time.RFC3339))
	fmt.Printf("corridor list: asset.Registry() — %d corridor(s), %d fiat(s)\n\n",
		countCorridors(corridorsByFiat), len(fiats))
	for _, p := range providers {
		fmt.Printf("  %-16s %s\n", p.Name(), effectiveBaseURL(p))
	}
	fmt.Println()

	if len(fiats) == 0 {
		fmt.Println("No registered corridor fiats found — nothing to check.")
		os.Exit(exitCovered)
	}

	ctx := context.Background()
	results := make(map[string]map[string]cell, len(fiats))
	for _, peg := range fiats {
		byProvider := make(map[string]cell, len(providers))
		for _, p := range providers {
			byProvider[p.Name()] = ask(ctx, p, peg)
		}
		results[peg] = byProvider
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "FIAT\tCORRIDORS")
	for _, p := range providers {
		fmt.Fprintf(w, "\t%s", p.Name())
	}
	fmt.Fprintln(w)
	for _, peg := range fiats {
		fmt.Fprintf(w, "%s\t%s", peg, strings.Join(corridorsByFiat[peg], " "))
		for _, p := range providers {
			c := results[peg][p.Name()]
			switch c.state {
			case "ok":
				fmt.Fprintf(w, "\tok %s", c.note)
			case "GAP":
				fmt.Fprintf(w, "\tGAP")
			default:
				fmt.Fprintf(w, "\t%s", c.state)
			}
		}
		fmt.Fprintln(w)
	}
	w.Flush()
	fmt.Println()

	ok, gaps, failures := 0, 0, 0
	for _, peg := range fiats {
		for _, p := range providers {
			c := results[peg][p.Name()]
			switch {
			case c.ok():
				ok++
			case c.state == "GAP":
				gaps++
				fmt.Printf("GAP: %s publishes no rate for USD/%s.\n", p.Name(), peg)
				fmt.Printf("  Corridors affected: %s.\n", strings.Join(corridorsByFiat[peg], ", "))
				fmt.Println("  Every measurement of those corridors degrades to SINGLE: with one")
				fmt.Println("  observation there is no cross-check, so AGREE is unreachable —")
				fmt.Println("  the corridor still measures, but uncorroborated, forever.")
				fmt.Println()
			default:
				failures++
				fmt.Printf("%s: %s for USD/%s: %s\n", c.state, p.Name(), peg, c.note)
				fmt.Println("  The provider did not answer, so coverage for this fiat is unknown,")
				fmt.Println("  not absent. Re-run before drawing any conclusion.")
				fmt.Println()
			}
		}
	}

	fmt.Printf("%d checks over %d fiat(s): %d ok, %d gap, %d failed.\n",
		len(fiats)*len(providers), len(fiats), ok, gaps, failures)
	fmt.Printf("upstream requests sent: %d (one per provider document; responses are\n"+
		"replayed per fiat so the free-tier quotas are not spent per pair)\n", transport.fetched)

	switch {
	case gaps > 0:
		fmt.Println("RESULT: GAP — a corridor fiat one provider does not serve.")
		os.Exit(exitGap)
	case failures > 0:
		fmt.Println("RESULT: INCONCLUSIVE — at least one provider did not answer.")
		os.Exit(exitFailed)
	default:
		fmt.Println("RESULT: covered — both providers serve every registered corridor fiat.")
		os.Exit(exitCovered)
	}
}

func countCorridors(byFiat map[string][]string) int {
	n := 0
	for _, codes := range byFiat {
		n += len(codes)
	}
	return n
}

// effectiveBaseURL reports the endpoint actually queried, for the record.
func effectiveBaseURL(p refrate.Provider) string {
	switch p.(type) {
	case *refrate.ExchangeRateAPI:
		if u := os.Getenv("COVCHECK_ERAPI_URL"); u != "" {
			return u
		}
		return refrate.DefaultExchangeRateAPI
	default:
		if u := os.Getenv("COVCHECK_CURRENCYAPI_URL"); u != "" {
			return u
		}
		return refrate.DefaultCurrencyAPI
	}
}
GOEOF

# Built and executed rather than `go run`: go run reports a non-zero program
# exit as its own failure and collapses this script's documented exit codes
# (2 in particular) into 1, and those codes are the script's interface.
go build -o "$work/covcheck" "./${work#"$PWD"/}" \
	|| die "could not compile the coverage helper"

"$work/covcheck"
