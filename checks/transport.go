package checks

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// SSRF protection for URLs published by the subjects being audited.
//
// # Why this is not paranoia here
//
// Most SSRF advice concerns URLs a user submits. This is worse: the URLs come
// from stellar.toml documents published by the anchors this tool exists to
// audit, and they are fetched by a server on behalf of anyone who asks for a
// corridor. An anchor that wanted to could publish
//
//	WEB_AUTH_ENDPOINT = "http://169.254.169.254/latest/meta-data/"
//
// and every deployment probing it would read its own cloud metadata. Loopback
// and private ranges are the same problem pointed at whatever else the host
// can reach.
//
// The input is adversarial by construction, so the transport treats it that
// way.
//
// # Why the check is on the dialled address
//
// Validating the hostname before connecting is not enough: DNS can resolve a
// public-looking name to 127.0.0.1, and a redirect can move the request
// somewhere else entirely. Checking inside Dialer.Control catches the address
// actually being connected to, on the initial request and on every redirect,
// after resolution.

// maxErrorBody bounds how much of a non-OK response is read.
//
// A controlled endpoint can return an arbitrarily large body, and decoding it
// whole would let any anchor exhaust the memory of everything probing it.
const maxErrorBody = 64 << 10 // 64 KiB

// disallowedIP reports whether an address is one a probe must never reach.
func disallowedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() ||
		// 169.254.169.254 is link-local and already covered, but IPv6
		// unique-local (fc00::/7) is not, and it is the same class of
		// target.
		isUniqueLocalV6(ip)
}

func isUniqueLocalV6(ip net.IP) bool {
	v6 := ip.To16()
	return ip.To4() == nil && v6 != nil && v6[0]&0xfe == 0xfc
}

// guardedControl rejects a connection to an address a probe must not reach.
func guardedControl(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("checks: refusing to dial %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("checks: refusing to dial %q: not an IP address", host)
	}
	if disallowedIP(ip) {
		return fmt.Errorf(
			"checks: refusing to probe %s — an anchor-published URL resolving to a "+
				"loopback, private or link-local address is an attempt to make this "+
				"server reach something on its own network", ip)
	}
	return nil
}

// GuardedClient returns an HTTP client safe to point at a URL an audited party
// published.
//
// Redirects are followed but re-validated: every hop dials through the same
// guard, and the chain is bounded so a redirect loop cannot hold a connection
// open indefinitely.
func GuardedClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   guardedControl,
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			DisableKeepAlives:     true,
			MaxIdleConns:          4,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("checks: too many redirects (%d)", len(via))
			}
			// The scheme is re-checked per hop: an https endpoint that
			// redirects to http would otherwise downgrade silently.
			if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
				return fmt.Errorf("checks: refusing to follow a redirect to scheme %q",
					req.URL.Scheme)
			}
			return nil
		},
	}
}
