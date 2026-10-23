package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SEP10EndpointResponds probes whether an anchor's declared authentication
// endpoint actually answers.
//
// # Why declared and working are different facts
//
// A stellar.toml is a claim. WEB_AUTH_ENDPOINT says "authenticate here", and
// nothing in the document proves anything is listening. Three states matter and
// are routinely conflated:
//
//	not declared     the anchor offers no programmatic authentication
//	declared, dead   the anchor says it does, and does not
//	declared, live   it works
//
// The middle one is the interesting case, and it is invisible to anyone who
// only reads the TOML. It is also the more damning: publishing an endpoint that
// does not answer says something about how an anchor is operated that silence
// does not.
//
// # What it can determine
//
// Whether the declared endpoint returns a well-formed SEP-10 challenge for a
// syntactically valid account.
//
// # What it cannot determine
//
// Whether the challenge is *correctly signed*, or would be accepted back. This
// deliberately does not verify the signature: doing so requires the anchor's
// signing key and an XDR parse, and a challenge that is well-formed but
// mis-signed is a different, narrower defect that deserves its own check.
//
// It also cannot distinguish an anchor that is down right now from one that is
// permanently broken. A single probe is a moment, not a pattern — telling those
// apart needs history.
type SEP10EndpointResponds struct {
	// HTTPClient is the seam that makes this replayable from a snapshot.
	HTTPClient *http.Client

	// ProbeAccount is the account the challenge is requested for. It only
	// needs to be well-formed; no key is held and nothing is signed.
	ProbeAccount string
}

// probeAccount is the canonical all-zero ed25519 account: StrKey version byte
// 0x30, thirty-two zero bytes, and a correct CRC16 checksum.
//
// It is used purely to ask for a challenge. Nothing is signed with it, and no
// private key for it exists or is needed — a SEP-10 server issues a challenge
// to any well-formed account, whether or not it is funded.
//
// The checksum matters more than it looks. An earlier version of this constant
// had an invalid one, so every anchor correctly rejected it with HTTP 400 and
// this check reported a false failure against a perfectly healthy endpoint.
// Verified against Horizon and a live anchor before being committed.
const probeAccount = "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF"

// Describe implements Check.
func (SEP10EndpointResponds) Describe() Descriptor {
	return Descriptor{
		ID:       "sep10.endpoint-responds",
		Scope:    ScopeAnchor,
		Cost:     CostOneRequest,
		Severity: SeverityWarning,
		Title:    "Declared SEP-10 endpoint returns a challenge",
		CanDetermine: "Whether the WEB_AUTH_ENDPOINT declared in the anchor's stellar.toml " +
			"answers with a well-formed SEP-10 challenge.",
		CannotDetermine: "Whether the challenge is correctly signed or would be accepted " +
			"back — that needs the anchor's signing key and an XDR parse, and is a narrower " +
			"defect deserving its own check. Nor can one probe distinguish an anchor that is " +
			"down now from one permanently broken.",
	}
}

func (c SEP10EndpointResponds) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	// Guarded by default: this probes a URL the audited anchor published.
	return GuardedClient(15 * time.Second)
}

func (c SEP10EndpointResponds) account() string {
	if c.ProbeAccount != "" {
		return c.ProbeAccount
	}
	return probeAccount
}

// Run implements Check.
func (c SEP10EndpointResponds) Run(ctx context.Context, s Subject) CheckResult {
	d := c.Describe()

	if s.Profile == nil {
		return Undetermined(d, s,
			"no stellar.toml has been resolved for this anchor, so no endpoint was declared to probe")
	}

	endpoint := strings.TrimSpace(s.Profile.TOML.WebAuthEndpoint)
	tomlURL := "https://" + s.Profile.Domain + "/.well-known/stellar.toml"
	// Evidence about the cached document carries the time it was originally
	// fetched, not the time this check runs.
	at := s.Profile.FetchedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}

	// Not declaring an endpoint is not a failure. The anchor has said it
	// offers no programmatic authentication, which is a legitimate position
	// and a different fact from claiming one that does not work.
	if endpoint == "" {
		return Undetermined(d, s,
			"the anchor declares no WEB_AUTH_ENDPOINT, so it offers no programmatic "+
				"authentication to probe",
			Evidence{
				Source:     tomlURL + " → WEB_AUTH_ENDPOINT",
				Observed:   "(field absent)",
				ObservedAt: at,
			})
	}

	probeURL, err := buildChallengeURL(endpoint, c.account())
	if err != nil {
		return Fail(d, s,
			"the declared WEB_AUTH_ENDPOINT is not a usable URL: "+err.Error(),
			Evidence{
				Source:     tomlURL + " → WEB_AUTH_ENDPOINT",
				Observed:   "\"" + endpoint + "\"",
				ObservedAt: at,
			})
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return Undetermined(d, s, "could not build the request: "+err.Error())
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client().Do(req)
	if err != nil {
		// A declared endpoint that cannot be reached IS the finding. This
		// is the one place a transport error is a determined failure rather
		// than an unknown: the anchor published this address, and it does
		// not answer.
		return Fail(d, s,
			"the declared SEP-10 endpoint did not respond: "+err.Error(),
			Evidence{Source: probeURL, Observed: "request failed", ObservedAt: at})
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Read the body: an anchor rejecting *our probe account* is our
		// problem, not a defect in its endpoint. Blaming the anchor for a
		// request we malformed is exactly the false failure this check
		// produced when its probe account had a bad checksum.
		msg := errorMessage(io.LimitReader(resp.Body, maxErrorBody))
		if rejectsProbe(msg) {
			return Undetermined(d, s,
				fmt.Sprintf("the endpoint rejected the probe account rather than failing: %q. "+
					"This is a defect in the probe, not in the anchor.", msg),
				Evidence{Source: probeURL,
					Observed:   fmt.Sprintf("HTTP %d: %s", resp.StatusCode, msg),
					ObservedAt: at})
		}

		observed := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if msg != "" {
			observed += ": " + msg
		}
		return Fail(d, s,
			fmt.Sprintf("the declared SEP-10 endpoint returned HTTP %d rather than a challenge",
				resp.StatusCode),
			Evidence{Source: probeURL, Observed: observed, ObservedAt: at})
	}

	var body struct {
		Transaction       string `json:"transaction"`
		NetworkPassphrase string `json:"network_passphrase"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Fail(d, s,
			"the endpoint answered but the response was not JSON: "+err.Error(),
			Evidence{Source: probeURL, Observed: "unparseable body", ObservedAt: at})
	}

	if strings.TrimSpace(body.Transaction) == "" {
		return Fail(d, s,
			"the endpoint answered with JSON containing no transaction, so it is not a SEP-10 challenge",
			Evidence{Source: probeURL,
				Observed:   "200 OK, no \"transaction\" field",
				ObservedAt: at})
	}

	ev := Evidence{
		Source:     probeURL,
		Observed:   fmt.Sprintf("200 OK, challenge of %d bytes", len(body.Transaction)),
		ObservedAt: at,
	}
	summary := "the declared SEP-10 endpoint returned a well-formed challenge"
	if body.NetworkPassphrase != "" {
		summary += " for " + body.NetworkPassphrase
	}
	return Pass(d, s, summary, ev)
}

// buildChallengeURL appends the account parameter SEP-10 requires.
func buildChallengeURL(endpoint, account string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("scheme %q is not http or https", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("no host in %q", endpoint)
	}
	q := u.Query()
	q.Set("account", account)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// errorMessage extracts an anchor's JSON error string, if it sent one.
func errorMessage(r io.Reader) string {
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return ""
	}
	return strings.TrimSpace(body.Error)
}

// rejectsProbe reports whether an error names the account we supplied.
//
// Deliberately conservative: it matches only phrasings that clearly refer to
// the account argument. A broader match would let a genuinely broken endpoint
// excuse itself by mentioning the word "account" in an unrelated error.
func rejectsProbe(msg string) bool {
	if msg == "" {
		return false
	}
	m := strings.ToLower(msg)
	for _, phrase := range []string{
		"public key",
		"invalid account",
		"account is invalid",
		"malformed account",
		"invalid ed25519",
	} {
		if strings.Contains(m, phrase) {
			return true
		}
	}
	return false
}
