package metrics

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"
)

// Upstream label values for the standard deployment.
//
// They are declared here rather than derived from a URL so that the label set
// is bounded by the code that wires it, and so that one operator's -horizon
// override cannot publish an arbitrary host as a metric label. A value appears
// in the exposition only if some client in this process was built with it.
const (
	// UpstreamHorizon is the Stellar Horizon path and account queries — the
	// upstream a measurement cannot happen without.
	UpstreamHorizon = "horizon"
	// UpstreamExchangeRateAPI, UpstreamCurrencyAPI and UpstreamFloatRates are
	// the three reference-rate feeds, named per feed because "the benchmark
	// is degrading" is only actionable if the operator can tell which of the
	// two mids stopped agreeing with the other.
	UpstreamExchangeRateAPI = "refrate.exchangerate-api"
	UpstreamCurrencyAPI     = "refrate.currency-api"
	UpstreamFloatRates      = "refrate.floatrates"
	// UpstreamChecks is the HTTP client the counterparty checks share, which
	// reaches Horizon and the anchor's own servers (stellar.toml, SEP-1/6/10/24/31
	// endpoints). It carries no finer label because one client serves both,
	// and a check's own findings already name what it probed.
	UpstreamChecks = "checks"
)

// Transport is an http.RoundTripper that records one exchange per request.
//
// # Why the count happens here and not in the clients
//
// The taxonomy this metric reports already exists as typed errors — dex
// distinguishes a 429 from a 500 (issue #121) and refrate distinguishes a
// provider that refused, broke, or never answered (issue #127). Those types
// are the right thing for a caller to branch on, and they are the wrong place
// to count: a Horizon failure inside a priced corridor never reaches the
// caller as an error at all. route.Engine.Quote records it as a note on the
// result and keeps the rung, so by the time the sweep or the HTTP handler sees
// anything, the error has been flattened into a string and the type is gone.
// Reaching for the count would have meant changing route's result shape — the
// thing every response and every stored record is built from — for an operator
// metric.
//
// The transport is the one layer where every upstream exchange is visible with
// its status and duration intact, in both the scheduled sweep and live
// requests, without a single line of the pricing path changing. What it
// cannot see is a response that arrived cleanly and could not be parsed: that
// fails one layer up, reads as HTTP 200 here, and is documented as a gap in
// docs/api.md rather than quietly counted as something else.
//
// A zero Transport is inert: with no Base it uses http.DefaultTransport, and a
// nil Collector makes it a pass-through.
type Transport struct {
	// Upstream is the label every exchange through this transport is
	// recorded under. It is set at wiring time, never inferred from a request.
	Upstream string

	// Collector receives one Upstream observation per completed exchange.
	// Nil disables recording entirely.
	Collector *Collector

	// Base is the transport actually used. Nil means http.DefaultTransport.
	Base http.RoundTripper
}

// NewTransport returns a Transport recording exchanges against collector
// under the label upstream, on top of base (nil for http.DefaultTransport).
func NewTransport(upstream string, collector *Collector, base http.RoundTripper) *Transport {
	return &Transport{Upstream: upstream, Collector: collector, Base: base}
}

// NewClient returns an *http.Client whose every exchange is recorded, for use
// where a client — not a transport — is what the upstream package accepts.
// timeout must match what that package would have used uninstrumented: the
// clients in dex and refrate build their own default clients, and replacing
// one with a client of a different timeout would change behaviour while
// measuring it.
func NewClient(upstream string, collector *Collector, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: NewTransport(upstream, collector, nil),
		Timeout:   timeout,
	}
}

// RoundTrip performs the exchange and records it.
//
// It never alters the response or the error: the caller — dex, refrate, an
// anchor resolver — still classifies and wraps failures exactly as it did, so
// the observable behaviour of every client behind this transport is unchanged.
// RoundTrip is called concurrently by the ladder (four rungs at a time) and by
// overlapping requests, which is why the collector takes its own lock instead
// of the transport keeping state.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if t.Collector == nil {
		return base.RoundTrip(req)
	}

	started := time.Now()
	resp, err := base.RoundTrip(req)
	// The duration is read before classification so a slow failure is not
	// charged with the cost of deciding which failure it was.
	took := time.Since(started)
	t.Collector.Upstream(t.Upstream, took, classifyExchange(req, resp, err))
	return resp, err
}

// classifyExchange names how one HTTP exchange failed, or "" when the upstream
// answered at all.
//
// An error takes precedence over a status because a transport error and a
// status cannot both happen in one exchange (RoundTrip returns a nil response
// on error), and because the deadline case must not be flattened into
// "transport": an upstream that is merely slow past the measurement budget is
// a different operational fact from one whose DNS record does not resolve.
func classifyExchange(req *http.Request, resp *http.Response, err error) string {
	if err != nil {
		if isDeadline(req, err) {
			return UpstreamTimeout
		}
		return UpstreamTransport
	}
	switch {
	case resp == nil:
		// A RoundTripper is allowed to return (nil, nil) only by
		// misbehaving; recording it as a transport failure keeps the counter
		// honest rather than skipping an exchange that clearly happened.
		return UpstreamTransport
	case resp.StatusCode == http.StatusTooManyRequests:
		return UpstreamRateLimited
	case resp.StatusCode >= http.StatusInternalServerError:
		return UpstreamServerError
	case resp.StatusCode >= http.StatusBadRequest:
		return UpstreamClientError
	default:
		return ""
	}
}

// isDeadline reports whether the exchange was cut off by a deadline rather
// than failing to connect at all.
//
// Neither the error nor the context alone can answer this. An http.Client with
// a Timeout does not hand the transport a deadline it can read as a cause: it
// sets a deadline on the request context *and* cancels through Request.Cancel,
// and whichever of those two goroutines wins the race decides what the
// transport returns — "context deadline exceeded" or "net/http: request
// canceled", the second being the same error an explicit cancel produces.
// Over ten runs of this package's own client-timeout test the error alone
// misreports one timeout in ten; the deadline below never does.
//
// The deadline itself is not raced, because a cancel caused by a timer cannot
// arrive before the time the timer was set for: if the request context carries
// a deadline and that deadline has passed by the time the exchange comes back
// failed, the exchange ran out of time. A cancel that beats the deadline — the
// caller stopping a ladder on purpose — still sees a live deadline, so it is
// correctly reported as a transport failure rather than as the upstream being
// slow.
//
// The error is checked too, for deadlines that belong to the socket rather than
// the request: a dial or response-header timeout inside the transport surfaces
// as os.ErrDeadlineExceeded with no request deadline involved.
func isDeadline(req *http.Request, err error) bool {
	ctx := req.Context()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}
