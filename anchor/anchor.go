// Package anchor discovers what an anchor can actually do.
//
// Anchors are the bridge between Stellar and a bank account, and they differ
// enormously in what they expose to software. The difference is not
// documented anywhere central — it has to be read from each anchor's own
// stellar.toml, per SEP-1.
//
// The distinction that drives this package is between an anchor a program can
// get a price from and one it cannot. An anchor publishes ANCHOR_QUOTE_SERVER
// to declare SEP-38 support; without that field there is no machine-readable
// rate anywhere, and the only way to learn the price is for a human to open
// the anchor's hosted SEP-24 flow and read it off the screen.
//
// This is not hypothetical. Checked on 2026-08-04, ngnc.online — the primary
// naira anchor, issuing NGNC on mainnet — publishes WEB_AUTH_ENDPOINT and
// TRANSFER_SERVER_SEP0024 and no quote server. The naira leg of this corridor
// therefore cannot be priced through its anchor at all.
package anchor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/Wayfare-labs/wayfare/asset"
)

// Currency is one entry from the [[CURRENCIES]] array of a stellar.toml.
type Currency struct {
	Code            string `toml:"code"`
	Issuer          string `toml:"issuer"`
	Status          string `toml:"status"`
	IsAssetAnchored bool   `toml:"is_asset_anchored"`
	AnchorAsset     string `toml:"anchor_asset"`
	Desc            string `toml:"desc"`
}

// Asset converts the entry to an asset.Asset.
func (c Currency) Asset() asset.Asset {
	return asset.Stellar(c.Code, c.Issuer)
}

// Live reports whether the anchor considers the asset in service. SEP-1
// defines "live", "dead", "test" and "private"; anything not explicitly live
// should not be routed through.
func (c Currency) Live() bool {
	return c.Status == "" || strings.EqualFold(c.Status, "live")
}

// TOML is the subset of a stellar.toml this project reads.
type TOML struct {
	Version             string     `toml:"VERSION"`
	NetworkPassphrase   string     `toml:"NETWORK_PASSPHRASE"`
	WebAuthEndpoint     string     `toml:"WEB_AUTH_ENDPOINT"`
	TransferServer      string     `toml:"TRANSFER_SERVER"`
	TransferServer24    string     `toml:"TRANSFER_SERVER_SEP0024"`
	DirectPaymentServer string     `toml:"DIRECT_PAYMENT_SERVER"`
	AnchorQuoteServer   string     `toml:"ANCHOR_QUOTE_SERVER"`
	KYCServer           string     `toml:"KYC_SERVER"`
	SigningKey          string     `toml:"SIGNING_KEY"`
	OrgName             string     `toml:"ORG_NAME"`
	OrgURL              string     `toml:"ORG_URL"`
	Currencies          []Currency `toml:"CURRENCIES"`
}

// MainnetPassphrase identifies the Stellar public network.
const MainnetPassphrase = "Public Global Stellar Network ; September 2015"

// Profile is an anchor's capabilities, resolved from its stellar.toml.
type Profile struct {
	Domain string
	TOML   TOML

	// Priceable reports whether a program can obtain a rate from this
	// anchor without human interaction. This is the single most important
	// field: a corridor served only by non-priceable anchors cannot be
	// compared, only guessed at.
	Priceable bool

	// SEP24, SEP31, SEP6 record which transfer flows are offered.
	SEP24 bool
	SEP31 bool
	SEP6  bool

	// SEP10 reports whether the anchor declares WEB_AUTH_ENDPOINT, i.e.
	// whether it offers programmatic authentication at all. See
	// checks.SEP10EndpointResponds for whether a declared endpoint works.
	SEP10 bool

	// SEP12 reports whether the anchor declares KYC_SERVER.
	SEP12 bool

	// Mainnet reports whether the TOML declares the public network.
	Mainnet bool

	// Malformed reports that the anchor's stellar.toml is not valid TOML
	// and the fields below were recovered by a salvage pass. The profile is
	// still usable, but the defect is surfaced: a published document that
	// does not parse says something about how the anchor is operated, and
	// the recovered fields carry less confidence than parsed ones.
	Malformed bool

	// MalformedReason is the strict parser's error, retained so the defect
	// can be reported to the anchor rather than merely worked around.
	MalformedReason string
}

// SupportsAsset reports whether the anchor lists a live currency matching a.
//
// Both code and issuer must match. Matching on code alone would let any
// anchor claim any asset.
func (p Profile) SupportsAsset(a asset.Asset) bool {
	for _, c := range p.TOML.Currencies {
		if c.Live() && c.Code == a.Code && c.Issuer == a.Issuer {
			return true
		}
	}
	return false
}

// LiveCurrencies returns the anchor's in-service currencies.
func (p Profile) LiveCurrencies() []Currency {
	out := make([]Currency, 0, len(p.TOML.Currencies))
	for _, c := range p.TOML.Currencies {
		if c.Live() {
			out = append(out, c)
		}
	}
	return out
}

// sepFields is a fixed inspection order, not a set: the value in a map
// iterates in an unspecified order, and a capability inventory that printed
// its SEPs in a different order on every call would be its own small
// instance of the non-determinism this project refuses in its measurements.
var sepFields = []struct {
	number int
	name   string
	has    func(Profile) bool
}{
	{1, "stellar.toml (this document)", func(Profile) bool { return true }},
	{6, "programmatic deposit/withdraw", func(p Profile) bool { return p.SEP6 }},
	{10, "web authentication", func(p Profile) bool { return p.SEP10 }},
	{12, "KYC", func(p Profile) bool { return p.SEP12 }},
	{24, "hosted deposit/withdraw", func(p Profile) bool { return p.SEP24 }},
	{31, "cross-border payment", func(p Profile) bool { return p.SEP31 }},
	{38, "quotes", func(p Profile) bool { return p.Priceable }},
}

// SEPs lists the numbers of the SEPs this anchor advertises, ascending.
//
// This is the capability inventory issue #184 asks for: everything this
// package already reads from a stellar.toml — WEB_AUTH_ENDPOINT,
// TRANSFER_SERVER, TRANSFER_SERVER_SEP0024, DIRECT_PAYMENT_SERVER,
// KYC_SERVER, ANCHOR_QUOTE_SERVER — mapped to the SEP number each field
// declares, in one place, so a reader (or another program) gets the
// counterparty picture without re-deriving it from six separate booleans or
// re-reading the document by hand.
//
// SEP-1 is always present: a Profile exists only because a stellar.toml was
// fetched (or, for a Malformed one, at least partially recovered), and
// publishing that document at the well-known path is what SEP-1 defines.
func (p Profile) SEPs() []int {
	out := make([]int, 0, len(sepFields))
	for _, f := range sepFields {
		if f.has(p) {
			out = append(out, f.number)
		}
	}
	return out
}

// SEPCapabilities is SEPs, rendered for a reader: each entry is "SEP-N name".
func (p Profile) SEPCapabilities() []string {
	out := make([]string, 0, len(sepFields))
	for _, f := range sepFields {
		if f.has(p) {
			out = append(out, fmt.Sprintf("SEP-%d (%s)", f.number, f.name))
		}
	}
	return out
}

// Explain states in plain language what the anchor can and cannot do.
//
// The wording is deliberately blunt about non-priceable anchors, because the
// failure mode this guards against is a user assuming an integration exists
// where none does.
func (p Profile) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s", p.Domain)
	if p.TOML.OrgName != "" {
		fmt.Fprintf(&b, " (%s)", p.TOML.OrgName)
	}
	b.WriteString("\n")

	if p.Malformed {
		b.WriteString("  WARNING:    stellar.toml is not valid TOML; fields below were\n")
		b.WriteString("              recovered by a salvage pass and are less reliable.\n")
		fmt.Fprintf(&b, "              parser said: %s\n", p.MalformedReason)
	}

	if !p.Mainnet && p.TOML.NetworkPassphrase != "" {
		fmt.Fprintf(&b, "  network:    %s (NOT mainnet)\n", p.TOML.NetworkPassphrase)
	}

	fmt.Fprintf(&b, "  SEPs:       %s\n", strings.Join(p.SEPCapabilities(), ", "))

	flows := []string{}
	if p.SEP24 {
		flows = append(flows, "SEP-24 hosted")
	}
	if p.SEP31 {
		flows = append(flows, "SEP-31 cross-border")
	}
	if p.SEP6 {
		flows = append(flows, "SEP-6 programmatic")
	}
	if len(flows) == 0 {
		flows = append(flows, "none")
	}
	fmt.Fprintf(&b, "  transfer:   %s\n", strings.Join(flows, ", "))

	if p.Priceable {
		fmt.Fprintf(&b, "  quotes:     SEP-38 at %s\n", p.TOML.AnchorQuoteServer)
	} else {
		b.WriteString("  quotes:     NONE. No ANCHOR_QUOTE_SERVER in stellar.toml,\n")
		b.WriteString("              so this anchor publishes no machine-readable rate.\n")
		if p.SEP24 {
			b.WriteString("              Its rate is visible only inside the hosted SEP-24\n")
			b.WriteString("              flow, to a human, after authenticating.\n")
		}
	}

	if cs := p.LiveCurrencies(); len(cs) > 0 {
		codes := make([]string, 0, len(cs))
		for _, c := range cs {
			codes = append(codes, c.Code)
		}
		fmt.Fprintf(&b, "  live assets: %s\n", strings.Join(codes, ", "))
	}
	return b.String()
}

// Resolver fetches and parses stellar.toml files.
type Resolver struct {
	HTTPClient *http.Client
}

func (r *Resolver) httpClient() *http.Client {
	if r.HTTPClient != nil {
		return r.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// TOMLURL returns the well-known location defined by SEP-1.
func TOMLURL(domain string) string {
	domain = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(domain), "https://"), "/")
	return "https://" + domain + "/.well-known/stellar.toml"
}

// Resolve fetches a domain's stellar.toml and derives its capability profile.
func (r *Resolver) Resolve(ctx context.Context, domain string) (*Profile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, TOMLURL(domain), nil)
	if err != nil {
		return nil, fmt.Errorf("anchor: building request for %s: %w", domain, err)
	}

	resp, err := r.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("anchor: fetching stellar.toml for %s: %w", domain, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anchor: %s returned HTTP %d for stellar.toml", domain, resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxTOMLBytes))
	if err != nil {
		return nil, fmt.Errorf("anchor: reading stellar.toml for %s: %w", domain, err)
	}

	var t TOML
	if _, err := toml.Decode(string(raw), &t); err != nil {
		// Not valid TOML. Recover what we can rather than losing the
		// anchor entirely, and record the defect. See salvage.go.
		p := profileFrom(domain, salvageTOML(string(raw)))
		p.Malformed = true
		p.MalformedReason = err.Error()
		return p, nil
	}
	return profileFrom(domain, t), nil
}

// maxTOMLBytes caps how much of a stellar.toml is read. SEP-1 sets a 100KB
// limit; this bounds memory when a domain serves something unexpected.
const maxTOMLBytes = 100 * 1024

// profileFrom derives capabilities from a parsed TOML. Split out so it can be
// tested against fixtures without network access.
func profileFrom(domain string, t TOML) *Profile {
	return &Profile{
		Domain:    domain,
		TOML:      t,
		Priceable: strings.TrimSpace(t.AnchorQuoteServer) != "",
		SEP24:     strings.TrimSpace(t.TransferServer24) != "",
		SEP31:     strings.TrimSpace(t.DirectPaymentServer) != "",
		SEP6:      strings.TrimSpace(t.TransferServer) != "",
		SEP10:     strings.TrimSpace(t.WebAuthEndpoint) != "",
		SEP12:     strings.TrimSpace(t.KYCServer) != "",
		Mainnet:   t.NetworkPassphrase == MainnetPassphrase,
	}
}

// QuoteClientBaseURL returns the SEP-38 base URL, or an error explaining why
// the anchor cannot be quoted.
func (p Profile) QuoteClientBaseURL() (string, error) {
	if !p.Priceable {
		return "", fmt.Errorf(
			"anchor: %s does not implement SEP-38 (no ANCHOR_QUOTE_SERVER in its stellar.toml)",
			p.Domain)
	}
	return p.TOML.AnchorQuoteServer, nil
}
