// Package failover keeps one Horizon host's outage from blanking every corridor.
//
// # Why this sits at the transport
//
// Horizon is the single upstream whose failure makes every corridor UNKNOWN:
// pathfinding has no substitute, and a down host costs the whole service no
// matter which layer notices it. The migration plan's Option B
// (docs/horizon-rpc-migration.md) is the stopgap of pointing at a second,
// independently operated Horizon, which answers the same queries with the same
// AMM-inclusive routing.
//
// The switch happens here rather than inside the clients or the engine because
// that is where an exchange is visible with its status intact. dex reports a
// 404 and a 500 with the same untyped error, and route flattens a Horizon
// failure into a note on a priced rung before either the scheduler or an HTTP
// handler can see it, so a caller-level failover would have meant widening
// dex's error taxonomy and changing the result shape every response and stored
// record is built from. Both are maintainer-owned surfaces. At a
// http.RoundTripper the decision is fifteen lines and touches neither.
//
// # Why a decision is per ladder and not per exchange
//
// Two Horizons can disagree about which paths exist: they are separate
// processes, reading the same ledger at slightly different moments, with their
// own path-finding depth limits. A price curve is a comparison across sizes,
// and comparing sizes measured by different providers measures the providers'
// differences as well as the corridor's. So one ladder asks one host, and the
// answer never changes mid-ladder — including when that host breaks halfway
// through, which costs the remaining rungs their measurement rather than
// silently stitching two curves together. That is the trade-off issue #701
// asks for.
//
// The scope is a Pin carried in the context, established by the caller that
// knows a ladder is starting (server.handleCorridor, Scheduler.measure). A
// request with no Pin is served by the primary alone: a missing scope is a
// wiring mistake, and the failure mode it must never produce is the mixing
// this package exists to prevent.
//
// # Why only one host's traffic moves
//
// A failover transport is usually mounted on a client of its own, where every
// request it sees belongs to the host it fronts. The snapshot recorder is not
// that: one recorder stands under every upstream of a recorded run, so a
// transport mounted beneath it also sees reference-rate and counterparty-check
// requests. Moving those would send a currency feed's query to a Horizon, and
// no amount of care about statuses makes that correct.
//
// So the transport is told which origin it fronts and leaves every other request
// exactly as it found it.
package failover

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/Wayfare-labs/wayfare/transport"
)

// Transport routes one ladder's HTTP exchanges to one Horizon host.
//
// The zero value is inert: with no Secondary it is a pass-through, so an
// operator who configures one Horizon gets exactly the behaviour this package
// did not exist for. Build one with New to have both addresses checked at
// startup rather than on the first outage.
type Transport struct {
	// Base performs the exchange. Nil means http.DefaultTransport.
	Base http.RoundTripper

	// Primary is the origin this transport fronts. Requests aimed at any other
	// host pass through untouched.
	Primary string

	// Secondary is the failover host's base URL, as configured. Empty disables
	// failover.
	Secondary string

	// Logger reports a switch to the secondary host. Nil means slog.Default.
	// The event is logged once per ladder because it is the one moment the
	// service knows its primary is down, and a stopgap that is silently in
	// use is a stopgap nobody replaces.
	Logger *slog.Logger

	primary   *url.URL
	secondary *url.URL
}

// New returns a transport that moves requests aimed at primary over to
// secondary when primary is down.
//
// An empty secondary yields the inert pass-through rather than an error: the
// flag is optional, and "not configured" is not a bad value.
func New(primary, secondary string, base http.RoundTripper) (*Transport, error) {
	t := &Transport{Base: base, Primary: primary, Secondary: secondary}
	if secondary == "" {
		return t, nil
	}
	// The primary is checked with the same care even though it is only
	// compared against, never dialled from here: an address that fails to match
	// the requests the client actually makes would disable failover silently,
	// which is the one outcome an operator setting this flag cannot afford.
	p, err := parseOrigin("primary Horizon", primary)
	if err != nil {
		return nil, err
	}
	s, err := parseOrigin("Horizon failover", secondary)
	if err != nil {
		return nil, err
	}
	t.primary, t.secondary = p, s
	return t, nil
}

// parseOrigin accepts a bare scheme://host and rejects anything that would make
// the comparison against a request URL unreliable.
func parseOrigin(what, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, &url.Error{Op: "parse", URL: raw, Err: errBadScheme(what)}
	}
	if u.Host == "" {
		return nil, &url.Error{Op: "parse", URL: raw, Err: errNoHost(what)}
	}
	if u.Path != "" && u.Path != "/" {
		return nil, &url.Error{Op: "parse", URL: raw, Err: errWithPath(what)}
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u, nil
}

// The wording is a function rather than a value because the same three
// rejections apply to both addresses, and an operator needs to know which of
// the two they got wrong.
func errBadScheme(what string) error { return &configError{what + " URL must be http or https"} }

func errNoHost(what string) error { return &configError{what + " URL must name a host"} }

func errWithPath(what string) error {
	return &configError{what + " URL must be a bare origin without a path"}
}

// configError keeps the operator-facing wording in one place; url.Error wraps
// it so the failing value is still reported.
type configError struct{ msg string }

func (e *configError) Error() string { return e.msg }

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if t.secondary == nil {
		return base.RoundTrip(req)
	}

	if !t.fronts(req.URL) {
		// Somebody else's upstream. Claiming no decision is as important as
		// sending no traffic: a reference-rate exchange that settled this
		// ladder would pin the corridor to whatever host answered the feed.
		return base.RoundTrip(req)
	}

	pin := pinFrom(req.Context())
	if pin == nil {
		// No ladder scope. Staying on the primary is the only choice that
		// cannot mix two providers inside one measurement.
		return base.RoundTrip(req)
	}

	mine, decision := pin.claim()
	if !mine {
		host, err := decision.await(req.Context())
		if err != nil {
			return nil, err
		}
		if host == origin(req.URL) {
			return base.RoundTrip(req)
		}
		out, err := cloneFor(req, t.secondary)
		if err != nil {
			return nil, err
		}
		return base.RoundTrip(out)
	}

	resp, err := base.RoundTrip(req)
	if err != nil && req.Context().Err() != nil {
		// The ladder's own budget expired. Retrying against another host
		// would spend time that does not exist and report the outage as
		// slow, so the deadline wins and nothing is switched.
		decision.resolve(origin(req.URL), false)
		return resp, err
	}
	if !isOutage(resp, err) {
		// A 2xx, 3xx or 4xx is the primary answering, which settles the
		// ladder on the primary whether or not the answer is good.
		decision.resolve(origin(req.URL), false)
		return resp, err
	}

	// Only requests without a body can be re-issued. When this one has a body
	// the primary's answer is the only one available, so it is returned
	// rather than replaced by an error the caller cannot tell apart from an
	// outage. Every endpoint this transport fronts is a GET, so that is a
	// guarantee about the callers, not a coincidence.
	retried, cloneErr := cloneFor(req, t.secondary)
	if cloneErr != nil {
		decision.resolve(origin(req.URL), false)
		return resp, err
	}

	// The primary is down for this exchange. Discard its response before
	// issuing another: a body left unread pins a connection, and the caller
	// only ever sees the answer this method returns.
	drain(resp)

	resp, err = base.RoundTrip(retried)
	decision.resolve(origin(retried.URL), true)
	t.logSwitch(req, retried, resp, err)
	return resp, err
}

// fronts reports whether a request is aimed at the origin this transport was
// told to front.
//
// The comparison is case-insensitive on both halves because a configured
// address and the URL built from it are not guaranteed to agree in case, and a
// mismatch here reads as "failover is off" to nobody but the operator who set
// it. An unset primary fronts nothing, which is unreachable through New — both
// halves are parsed together — and is the inert reading for a hand-built struct.
func (t *Transport) fronts(requestURL *url.URL) bool {
	if t.primary == nil || requestURL == nil {
		return false
	}
	return strings.EqualFold(t.primary.Scheme, requestURL.Scheme) &&
		strings.EqualFold(t.primary.Host, requestURL.Host)
}

// logSwitch records the moment a ladder moved to the second host.
//
// Only the exchange that settled the ladder reaches this, so a ladder that
// fails over logs once. When the second host fails too the line says so, and
// says it at error level: "switched to the failover host" read on its own looks
// like a recovery, and an operator has to be able to tell a working stopgap from
// a corridor with no Horizon at all.
func (t *Transport) logSwitch(req, retried *http.Request, resp *http.Response, err error) {
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}
	attrs := []any{
		"path", req.URL.Path,
		"from", origin(req.URL),
		"to", origin(retried.URL),
	}
	if isOutage(resp, err) {
		// Both hosts are down for this ladder. The status and the transport
		// error are different facts about it — one is a provider failing to
		// serve, the other a host that cannot be reached at all — so each is
		// reported as itself and both are error level.
		if resp != nil {
			attrs = append(attrs, "status", resp.StatusCode)
		}
		if err != nil {
			attrs = append(attrs, "error", transport.SanitizeTransportError(err))
		}
		logger.Error("horizon failover host also failed", attrs...)
		return
	}
	logger.Info("switched to the horizon failover host", attrs...)
}

// isOutage reports whether an exchange failed in a way a different Horizon
// could fix.
//
// Only two shapes qualify: no HTTP response at all, and a 5xx. A 4xx is the
// upstream answering this request specifically — an unknown asset or a
// malformed query is wrong on every host, and re-asking a second provider
// turns a client bug into doubled load on someone else's service. 429 is
// included in that refusal for the same reason: the quota is the reason, and
// another host would only be a way to ignore it.
func isOutage(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	return resp != nil && resp.StatusCode >= http.StatusInternalServerError
}

func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	// The body is copied to a small bound rather than read to EOF: a 500 from
	// a broken Horizon can be arbitrarily large, and the point is only to let
	// the connection be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

// cloneFor returns a copy of req aimed at base.
//
// RoundTrippers must not modify the request they are handed, and the caller
// still owns this one — so the rewrite builds a new request instead. Only
// requests with no body can be re-issued: a body has one reader, and every
// endpoint this transport fronts is a GET.
func cloneFor(req *http.Request, base *url.URL) (*http.Request, error) {
	if req.Body != nil && req.Body != http.NoBody {
		return nil, errNotReplayable
	}
	out := req.Clone(req.Context())
	out.URL = cloneURL(req.URL)
	out.URL.Scheme = base.Scheme
	out.URL.Host = base.Host
	out.Host = ""
	return out, nil
}

var errNotReplayable = &configError{"a request with a body cannot be re-issued to a failover host"}

func cloneURL(u *url.URL) *url.URL {
	copied := *u
	return &copied
}

// origin renders the scheme://host a request actually went to, which is what
// a Pin records: two hosts differing only in port are two hosts.
func origin(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// ---------------------------------------------------------------------------
// Ladder scope
// ---------------------------------------------------------------------------

// Pin is one ladder's host decision, shared by every exchange made under the
// context WithPin returned.
type Pin struct {
	mu       sync.Mutex
	decision *decision
}

// decision is the one host choice, and the signal every other rung of the
// ladder waits on.
//
// The first caller to claim it performs the exchange that settles the question
// while the rest wait, because a choice made while others are already in
// flight on the first host is the mixing this type exists to prevent. Waiters
// can give up: the wait is bounded by the request context, so a hung primary
// costs one exchange per ladder rather than one per rung.
type decision struct {
	done chan struct{}

	mu       sync.Mutex
	host     string
	switched bool
	settled  bool
}

func (p *Pin) claim() (bool, *decision) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.decision == nil {
		p.decision = &decision{done: make(chan struct{})}
		return true, p.decision
	}
	return false, p.decision
}

// resolve settles the ladder on one host, and records whether that was the
// failover rather than the primary.
//
// The two facts travel together because the host alone cannot tell them apart:
// a caller comparing origins would have to reproduce the URL normalising that
// http.Request already did, and get a wrong answer whenever the configured
// primary differs from the request by a trailing slash.
func (d *decision) resolve(host string, switched bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.settled {
		return
	}
	d.host = host
	d.switched = switched
	d.settled = true
	close(d.done)
}

func (d *decision) await(ctx context.Context) (string, error) {
	select {
	case <-d.done:
		return d.host, nil
	case <-ctx.Done():
		// Unresolved by the time the caller gave up. Returning the context
		// error keeps "we never learned which host" out of the record.
		return "", ctx.Err()
	}
}

// Host reports the Horizon this ladder is pinned to, as a bare origin, or the
// empty string while no exchange has settled the choice.
//
// After a failed failover it names the failover host: the host whose answer the
// caller received, even when that answer was a refusal to give one. A record
// saying "primary" for a ladder that ended on the second host would point the
// reader at the wrong provider's status page.
//
// It is the accessor the snapshot recorder uses to say which host a published
// figure came from, and it stays empty for a process with no failover
// configured, which is how a caller tells "one Horizon" from "not yet decided".
func (p *Pin) Host() string {
	host, _ := p.settled()
	return host
}

// FailedOver reports whether this ladder was answered by the failover host
// rather than the configured one.
//
// Host is not enough to tell: a healthy primary also settles the ladder, and
// the two situations mean different things to whoever reads the record.
func (p *Pin) FailedOver() bool {
	_, switched := p.settled()
	return switched
}

// settled reads the decision once it exists and has been made, and reports the
// empty values while it has not.
func (p *Pin) settled() (host string, switched bool) {
	if p == nil {
		return "", false
	}
	p.mu.Lock()
	d := p.decision
	p.mu.Unlock()
	if d == nil {
		return "", false
	}
	select {
	case <-d.done:
		return d.host, d.switched
	default:
		return "", false
	}
}

type pinKey struct{}

// WithPin returns a context scoped to one Horizon host and the handle that
// reports which one it turned out to be.
//
// Call it once per ladder, before the fan-out, and pass the context to the
// engine. The returned Pin is safe to read from any goroutine.
func WithPin(ctx context.Context) (context.Context, *Pin) {
	pin := &Pin{}
	return context.WithValue(ctx, pinKey{}, pin), pin
}

func pinFrom(ctx context.Context) *Pin {
	pin, _ := ctx.Value(pinKey{}).(*Pin)
	return pin
}
