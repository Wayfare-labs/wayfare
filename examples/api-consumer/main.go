// Command api-consumer is the reference implementation of reading Wayfare.
//
// It fetches one corridor and prints only what may honestly be printed from the
// response. The interesting part of this program is not the HTTP call — it is
// the four refusals it implements:
//
//	live: false        the reading is stored, so it is labelled with its age
//	                   rather than rendered as current
//	scored: false      no reference rate could be scored against, so no loss
//	                   figure and no verdict are rendered at all
//	recommended: null  nothing is worth taking, and the program says so
//	                   instead of substituting the best-scoring rung
//
// The fifth refusal is defensive: a recommendation whose own verdict is
// UNUSABLE is not rendered, because a recommendation implies its winner is
// worth taking. The API does not produce that combination today —
// `route.Ladder` recommends only a POOR-or-better quote — but a consumer that
// trusted it blindly would be the one publishing the claim.
//
// **Every money field is decoded as a string.** On the wire, amounts, rates and
// percentages are decimal strings, never JSON numbers (ADR 006). Decoding them
// into float64 would reintroduce the rounding error the engine avoids
// internally, and declaring them as strings means a JSON number in one of these
// fields is a decode error — loud, rather than a silently rounded figure.
//
// The reading rules are written out in docs/api-consumer.md.
//
// Usage:
//
//	go run ./examples/api-consumer
//	go run ./examples/api-consumer -to GHSC -live
//	go run ./examples/api-consumer -base http://localhost:8080
//
// Exit status: 0 when a recommendation was rendered, 2 when the response was
// read and there is nothing worth taking, 1 when nothing could be read.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// defaultBase is the instance documented in README.md. It runs with
// `-history-first`, so an unadorned request is answered from embedded history
// unless -live is passed.
const defaultBase = "https://wayfare-cdb9.onrender.com"

// Exit codes. They are a contract for a script reading this program's output,
// the same way cmd/ladder's exit code is a contract for a script reading its.
const (
	exitRendered = 0 // a recommendation was printed
	exitUnread   = 1 // nothing could be read: transport, HTTP or decode failure
	exitNothing  = 2 // the response was read, and nothing is worth taking
)

func main() {
	from := flag.String("from", "USDC", "send asset code")
	to := flag.String("to", "NGNC", "receive asset code")
	live := flag.Bool("live", false, "measure now (?live=1) instead of serving stored history")
	base := flag.String("base", defaultBase, "base URL of a Wayfare instance")
	timeout := flag.Duration("timeout", 180*time.Second, "overall request timeout")
	flag.Parse()

	client := &http.Client{}
	code, err := consume(context.Background(), client, *base, *from, *to, *live, *timeout, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "api-consumer:", err)
		os.Exit(exitUnread)
	}
	os.Exit(code)
}

// consume fetches one corridor and writes what may be rendered from it.
//
// It returns the process exit code and, when the response could not be read at
// all, an error. A non-200 response is an error rather than a rendered failure:
// an error body is not a measurement, and printing figures derived from one
// would be the exact substitution this program exists to refuse.
func consume(ctx context.Context, client *http.Client, base, from, to string, live bool, timeout time.Duration, out io.Writer) (int, error) {
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	doc, err := fetchCorridor(ctx, client, base, from, to, live)
	if err != nil {
		return exitUnread, err
	}

	label := assetLabel(doc.SendAsset) + " -> " + assetLabel(doc.ReceiveAsset)
	fmt.Fprintf(out, "%s\n", label)
	fmt.Fprintf(out, "measured_at %s\n", orUnknown(doc.MeasuredAt))

	// Rule 1: live is not a verdict, it describes where the response came
	// from. A stored reading is labelled with its age, never presented as
	// current — freshness must not be inferred from the request time.
	if doc.Live {
		fmt.Fprintf(out, "live        true (measured now)\n")
	} else {
		fmt.Fprintf(out, "live        false — STORED READING, not a live measurement\n")
		if doc.Stale == nil {
			// The envelope is documented as present whenever live is
			// false. If it is missing, the age is unknown, and unknown is
			// reported as unknown rather than assumed to be recent.
			fmt.Fprintf(out, "stale       (no stale block in the response: age unknown)\n")
		} else {
			// The age is the response's, not this program's estimate:
			// stale.age_seconds and stale.age_human are what the
			// envelope is for.
			fmt.Fprintf(out, "stale       recorded %s, %s (%ds)\n",
				orUnknown(doc.Stale.RecordedAt), orUnknown(doc.Stale.AgeHuman), doc.Stale.AgeSeconds)
		}
	}

	fmt.Fprintf(out, "integrity   %s\n", orUnknown(doc.Integrity))
	switch doc.Integrity {
	case "NO-MARKET":
		fmt.Fprintf(out, "            no path was returned at any size: the absence of a price, not a bad one\n")
	case "DERIVATIVE":
		fmt.Fprintf(out, "            every path traverses another fiat token: %s\n", dependsOnLabel(doc.DependsOn))
	}
	fmt.Fprintf(out, "reference   %s %s (%s)\n",
		orUnknown(doc.ReferencePair), orUnknown(doc.ReferenceMid), orUnknown(doc.ReferenceSource))

	// Rule 2: scored is the gate on every loss figure and every verdict.
	// When the two reference providers diverged past tolerance there is no
	// mid to score against, and a consumer that rendered the numbers anyway
	// would present them as measurements.
	if !doc.Scored {
		fmt.Fprintf(out, "scored      false — reference agreement %s\n", orUnknown(doc.ReferenceAgreement))
		if doc.ReferenceNote != "" {
			fmt.Fprintf(out, "            %s\n", doc.ReferenceNote)
		}
		fmt.Fprintf(out, "\nNo loss figures and no verdicts are rendered: with no scorable reference\n")
		fmt.Fprintf(out, "there is nothing to score a route against, and a percentage here would be\n")
		fmt.Fprintf(out, "an artefact of which provider was believed.\n")
		return exitNothing, nil
	}
	fmt.Fprintf(out, "scored      true (agreement %s)\n", orUnknown(doc.ReferenceAgreement))
	fmt.Fprintf(out, "floor       %s%% at %s\n", orUnknown(doc.Floor), orUnknown(doc.FloorSize))
	fmt.Fprintf(out, "worst       %s%% at %s\n", orUnknown(doc.WorstLoss), orUnknown(doc.WorstSize))

	// Rule 3: a null recommendation means nothing is worth taking. It is
	// not an oversight and not an absence of data, so the program does not
	// substitute the best-scoring rung.
	if doc.Recommended == nil {
		fmt.Fprintf(out, "recommended none — no size produced a verdict of POOR or better\n")
		printRungs(out, doc.Rungs)
		return exitNothing, nil
	}

	// Rule 4, the defensive one: a recommendation whose own verdict is worse
	// than POOR is refused. The API does not produce this today; a consumer
	// that rendered it would be publishing the claim.
	if !recommendable(doc.Recommended.Verdict) {
		fmt.Fprintf(out, "recommended withheld — the response recommends a route graded %s, and a\n",
			orUnknown(doc.Recommended.Verdict))
		fmt.Fprintf(out, "            recommendation means its winner is worth taking\n")
		return exitNothing, nil
	}

	q := doc.Recommended
	fmt.Fprintf(out, "recommended %s %s -> %s %s\n",
		orUnknown(doc.RecommendedSize), assetLabel(doc.SendAsset), q.ReceiveAmount, assetLabel(doc.ReceiveAsset))
	fmt.Fprintf(out, "            %s, effective rate %s, %s%% below the %s mid\n",
		orUnknown(q.Description), orUnknown(q.EffectiveRate), orUnknown(q.LossPct), orUnknown(doc.ReferenceSource))
	fmt.Fprintf(out, "            verdict %s\n", orUnknown(q.Verdict))
	// Warnings travel with the figure. A receive amount that has to be
	// redeemed for fiat is not the same claim as cash in a bank account, and
	// dropping the warning changes what the number means.
	for _, w := range q.Warnings {
		fmt.Fprintf(out, "            warning: %s\n", w)
	}
	if doc.Finding != "" {
		fmt.Fprintf(out, "finding     %s\n", doc.Finding)
	}
	printRungs(out, doc.Rungs)
	return exitRendered, nil
}

// printRungs prints every rung, and an unpriced rung is never printed as a
// zero. A size that could not be measured and a size that measured nothing are
// different facts, and only one of them has a number.
func printRungs(out io.Writer, rungs []rung) {
	if len(rungs) == 0 {
		return
	}
	fmt.Fprintf(out, "\nrungs\n")
	for _, r := range rungs {
		switch {
		case r.Priced && r.Quote != nil:
			fmt.Fprintf(out, "  %-12s %s%%  %s  %s\n",
				r.SendAmount, orUnknown(r.Quote.LossPct), orUnknown(r.Quote.Verdict), r.Quote.Description)
		case r.Error != "":
			fmt.Fprintf(out, "  %-12s not measured: %s\n", r.SendAmount, r.Error)
		default:
			fmt.Fprintf(out, "  %-12s not priced\n", r.SendAmount)
		}
	}
}

// fetchCorridor performs the request and decodes the two shapes a response can
// take: a corridor document, or the API's error object.
func fetchCorridor(ctx context.Context, client *http.Client, base, from, to string, live bool) (*corridorDoc, error) {
	endpoint, err := corridorURL(base, from, to, live)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// The error shape is {"error": "...", "code": "..."}. Both are
		// surfaced: the code is what a program branches on, the message is
		// what a human reads.
		var e errorDoc
		if decodeErr := json.NewDecoder(resp.Body).Decode(&e); decodeErr != nil {
			return nil, fmt.Errorf("%s returned HTTP %d with an unreadable body", endpoint, resp.StatusCode)
		}
		return nil, fmt.Errorf("%s returned HTTP %d (%s): %s",
			endpoint, resp.StatusCode, orUnknown(e.Code), orUnknown(e.Error))
	}

	var doc corridorDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		// A reportable failure, not a soft one: this is also the path a
		// money field arriving as a JSON number takes, because those fields
		// are declared as strings.
		return nil, fmt.Errorf("decoding the corridor response: %w", err)
	}
	// Nothing is validated beyond the decode here on purpose. A missing
	// field is not a reason to invent one: consume prints "(not reported)"
	// or "age unknown" instead, because a default would read as a
	// measurement the response never made.
	return &doc, nil
}

// corridorURL builds the request URL. from and to are passed through
// url.Values so that a code, however it was typed, cannot become a second
// query parameter or a different path.
func corridorURL(base, from, to string, live bool) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "", errors.New("no base URL given")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parsing base URL %q: %w", base, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("base URL %q is not http or https", base)
	}

	q := url.Values{}
	q.Set("from", from)
	q.Set("to", to)
	if live {
		// On a -history-first deployment this is the only way to ask for a
		// measurement rather than a stored reading; it prices a full ladder
		// and takes tens of seconds.
		q.Set("live", "1")
	}
	return u.String() + "/api/corridor?" + q.Encode(), nil
}

// recommendable reports whether a verdict is one this program will render as a
// recommendation. POOR is the worst grade the engine recommends; anything
// beyond it is refused rather than displayed.
func recommendable(verdict string) bool {
	switch verdict {
	case "GOOD", "FAIR", "POOR":
		return true
	default:
		return false
	}
}

func assetLabel(a assetJSON) string {
	if a.Code == "" {
		return "unknown asset"
	}
	if a.Issuer == "" {
		return a.Code
	}
	return a.Code + " (" + short(a.Issuer) + ")"
}

func dependsOnLabel(as []assetJSON) string {
	if len(as) == 0 {
		return "no dependency was reported"
	}
	labels := make([]string, 0, len(as))
	for _, a := range as {
		labels = append(labels, a.Code)
	}
	return strings.Join(labels, ", ")
}

func short(issuer string) string {
	if len(issuer) <= 4 {
		return issuer
	}
	return issuer[:4] + "…"
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(not reported)"
	}
	return s
}

// The wire shapes this program reads. Only the fields it renders are declared:
// decoding into a narrower struct is how a consumer states which parts of the
// contract it actually depends on.
//
// Every money field is a string. See the package comment.

type assetJSON struct {
	Code   string `json:"code"`
	Issuer string `json:"issuer"`
	Peg    string `json:"peg"`
	Asset  string `json:"asset"`
}

type quote struct {
	Description   string   `json:"description"`
	Source        string   `json:"source"`
	ReceiveAmount string   `json:"receive_amount"`
	EffectiveRate string   `json:"effective_rate"`
	LossPct       string   `json:"loss_pct"`
	LossAmount    string   `json:"loss_amount"`
	Verdict       string   `json:"verdict"`
	Warnings      []string `json:"warnings"`
}

type rung struct {
	SendAmount string `json:"send_amount"`
	Priced     bool   `json:"priced"`
	Integrity  string `json:"integrity"`
	Quote      *quote `json:"quote"`
	Error      string `json:"error"`
}

type staleJSON struct {
	RecordedAt string `json:"recorded_at"`
	AgeSeconds int64  `json:"age_seconds"`
	AgeHuman   string `json:"age_human"`
}

type corridorDoc struct {
	SendAsset    assetJSON `json:"send_asset"`
	ReceiveAsset assetJSON `json:"receive_asset"`

	Integrity string      `json:"integrity"`
	DependsOn []assetJSON `json:"depends_on"`

	ReferenceMid       string `json:"reference_mid"`
	ReferenceSource    string `json:"reference_source"`
	ReferencePair      string `json:"reference_pair"`
	ReferenceAgreement string `json:"reference_agreement"`
	ReferenceNote      string `json:"reference_note"`
	Scored             bool   `json:"scored"`

	Floor     string `json:"floor_loss_pct"`
	FloorSize string `json:"floor_size"`
	WorstLoss string `json:"worst_loss_pct"`
	WorstSize string `json:"worst_size"`

	Recommended     *quote `json:"recommended"`
	RecommendedSize string `json:"recommended_size"`

	Live       bool       `json:"live"`
	Stale      *staleJSON `json:"stale"`
	Finding    string     `json:"finding"`
	MeasuredAt string     `json:"measured_at"`
	Rungs      []rung     `json:"rungs"`
}

type errorDoc struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}
