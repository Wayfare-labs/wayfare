package asset

import (
	"fmt"
	"sort"
	"strings"
)

// Verified mainnet issuers.
//
// Every issuer here was read from the issuer's own published stellar.toml
// (SEP-1) rather than copied from a block explorer or a blog post. Asset code
// alone is never sufficient identification: anyone can issue a token called
// "USDC", and a router that matched on code alone would happily quote a
// worthless lookalike. The verification date is recorded because anchors do
// rotate issuers.
//
// Verification status, 2026-08-08, read from
// https://ngnc.online/.well-known/stellar.toml:
//
//   - NGNC   VERIFIED, status="live". Issued by LINK.IO LTD., pegged 1:1 to
//     NGN, anchor_asset_type="fiat". NETWORK_PASSPHRASE = public mainnet.
//
//   - GHSC   VERIFIED as published, status="pending". Same issuing account as
//     NGNC. The anchor itself does not declare this asset in service.
//
//   - KESC   VERIFIED as published, status="pending". Same issuing account.
//     Note the entry sets anchor_asset="KESC", naming its own token rather
//     than the ISO-4217 code KES that SEP-1 intends. Read as KES.
//
//   - USDC   NOT YET VERIFIED against circle.com's stellar.toml. This is the
//     widely-published Circle issuer and Horizon accepted it for live
//     orderbook and path queries, which proves it is a real, actively traded
//     issuer — but not that it is Circle's. Confirm before any mainnet
//     execution path ships. See VerifyAgainstTOML in package anchor.
//
// The pending status on GHSC and KESC is a first-class finding, not a detail
// to route around. Per SEP-1 only "live" means in service, and the monitor
// reports an asset its own issuer has not launched as exactly that rather
// than pricing it as though it were tradeable.
//
// Verification status, 2026-08-26, read from each issuer's own stellar.toml:
//
//   - NGNT   VERIFIED, status="live", read from
//     https://cowrie.exchange/.well-known/stellar.toml. Issued by Cowrie
//     Integrated Systems (Lagos, Nigeria), pegged 1:1 to NGN,
//     anchor_asset_type="fiat", anchor_asset="NGN". Mainnet. The most
//     widely-held NGNT on mainnet (tens of thousands of trustlines), so the
//     token a naira path is likely to route through.
//
//   - USDZ   VERIFIED, status="live", read from
//     https://zeam.money/.well-known/stellar.toml. Issued by Zeam Mint
//     (Pty) Ltd, operated by Zeam SA (Pty) Ltd, regulated by the South
//     African FSCA (FSP 53737) and SARB as a Third Party Payment Provider.
//     The toml declares anchor_asset="USD, USDC, yUSDC, USDT" — a USD peg
//     backed by a basket of Tier-1 stablecoins — so the peg is read as USD.
//     Mainnet. Appeared as a hop in recorded live USDC->NGNC paths
//     (testdata/snapshots, 2026-08-21).
//
//   - ZARZ   VERIFIED, status="live", read from the same zeam.money
//     document. Issued by Zeam Mint (Pty) Ltd, pegged 1:1 to ZAR,
//     anchor_asset_type="fiat", anchor_asset="ZAR". Mainnet. A South
//     African rand token from a regulated issuer.
//
//   - EURMTL VERIFIED, status="live", read from
//     https://mtl.montelibero.org/.well-known/stellar.toml. Issued by
//     Montelibero, pegged 1:1 to EUR, anchor_asset_type="fiat",
//     anchor_asset="EUR". Mainnet. The most widely-held euro stablecoin on
//     Stellar, so a euro corridor's paths are likely to route through it.
//
//   - PYUSD  VERIFIED, read from
//     https://token-metadata.paxos.com/.well-known/stellar.toml. Issued by
//     Paxos Trust Company, powering PayPal USD, "100% backed by U.S. dollar
//     deposits, short-term U.S Treasuries and similar cash equivalents" and
//     "redeemable 1:1 for U.S. dollars". Mainnet. The toml declares no
//     status field (treated as live per SEP-1) and no anchor_asset; the peg
//     is read as USD from the description. Appeared as a hop in recorded
//     live USDC->NGNC paths (testdata/snapshots, 2026-08-21).
//
// Tokens considered and deliberately NOT registered, with the reason:
//
//   - USDT (Tether) — https://tether.to/.well-known/stellar.toml serves the
//     site's homepage, not a stellar.toml, so the widely-published issuer
//     cannot be verified from its own document. Until it can, USDT hops stay
//     in the unregistered gap rather than being guessed at.
//
//   - GYEN / ZUSD (GMO-Z.com Trust) — the issuer's own stellar.toml declares
//     issuance "wound down", with 1:1 redemption available only during a
//     fixed period ending 2026-11-11. That is not a live asset, so it is not
//     priced as one; the wind-down is a fact about the token, not a peg to
//     register.
//
//   - Stably Z-tokens (ZUSD, ZAR, ZAUD, ...) — stably.io serves no
//     stellar.toml (HTTP 400), so no issuer account can be verified.
//
//   - BRL (Capitual), IDRT (Rupiah Token) — no reachable stellar.toml.
//
// # Bridge assets
//
// A bridge hop is a token a path routes through that is deliberately treated
// as non-fiat: native XLM by construction, and any issued token registered
// as a non-fiat bridge. The category is a stated decision, not an absence
// from fiatPegs: XLM is not a fiat dependency because the project says so,
// not because the map happens not to contain it.
//
// Tokens observed as hops in recorded live paths that fall here: BLND
// (Blend), AQUA (Aquarius), yUSDC and wrapped BTC. None of them is
// fiat-pegged, so routing through them does not make a corridor derivative.
//
// # The unknown-token false-negative
//
// A hop that is neither native nor registered is "unknown", and an unknown
// hop is currently treated as evidence of an independent market — exactly
// the same as XLM. That default is right for identification (an unrecognised
// "NGNC" must never be assumed to track the naira) and wrong for
// classification: an unknown fiat stablecoin routed through makes a
// DERIVATIVE corridor look DIRECT. This is a known, bounded false-negative.
// It is bounded because route.classify surfaces every unknown hop it sees,
// so the gap is visible instead of silent. The fix is registration, never
// guessing.
const (
	// USDCIssuer is Circle's mainnet USDC issuing account.
	USDCIssuer = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"

	// LinkIOIssuer issues NGNC, GHSC and KESC from one account. It is the
	// single point of failure behind this issuer's entire African-fiat set.
	LinkIOIssuer = "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"

	// NGNCIssuer is retained as the original name for LinkIOIssuer.
	NGNCIssuer = LinkIOIssuer

	// CowrieIssuer issues NGNT, the naira token of Cowrie Integrated
	// Systems. Verified 2026-08-26 from cowrie.exchange's stellar.toml.
	CowrieIssuer = "GAWODAROMJ33V5YDFY3NPYTHVYQG7MJXVJ2ND3AOGIHYRWINES6ACCPD"

	// ZeamIssuer issues USDZ, the US-dollar token of Zeam Mint (Pty) Ltd.
	// Verified 2026-08-26 from zeam.money's stellar.toml.
	ZeamIssuer = "GAKTLPC4ZV37SSCITQ5IS5AQ4WPF4CF4VZJQPPAROSGXMYOATF5U6XPR"

	// ZeamZARIssuer issues ZARZ, Zeam's South African rand token. A
	// separate account from ZeamIssuer, per the same stellar.toml.
	ZeamZARIssuer = "GAROH4EV3WVVTRQKEY43GZK3XSRBEYETRVZ7SVG5LHWOAANSMCTJBB3U"

	// MonteliberoIssuer issues EURMTL, the euro stablecoin of Montelibero.
	// Verified 2026-08-26 from mtl.montelibero.org's stellar.toml.
	MonteliberoIssuer = "GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V"

	// PaxosIssuer issues PYUSD, PayPal USD, on behalf of Paxos Trust
	// Company. Verified 2026-08-26 from token-metadata.paxos.com's
	// stellar.toml.
	PaxosIssuer = "GDQE7IXJ4HUHV6RQHIUPRJSEZE4DRS5WY577O2FY6YQ5LVWZ7JZTU2V5"
)

// Entry represents a verified asset or corridor registration record.
//
// Structured metadata fields (Status, VerificationDate, SourceURL, HomeDomain)
// are stored directly as data so that tools and APIs can inspect them.
type Entry struct {
	Code             string // Asset code, e.g. "NGNC", "USDC"
	Issuer           string // Stellar issuing account ID
	Peg              string // ISO-4217 fiat currency tracked (e.g. "NGN"); empty for non-fiat settlement assets
	Status           string // SEP-1 status declared by issuer: "live", "pending", "unverified", etc.
	VerificationDate string // Date verified against issuer's stellar.toml (YYYY-MM-DD)
	SourceURL        string // URL where stellar.toml was read
	HomeDomain       string // Domain publishing stellar.toml
}

// CorridorEntry is an alias for Entry for backward and semantic compatibility.
type CorridorEntry = Entry

// ValidateEntry checks that a registration entry has all required fields.
// Corridor destination tokens (non-USDC assets) require Code, Issuer, Peg, Status,
// VerificationDate, SourceURL, and HomeDomain.
func ValidateEntry(e Entry) error {
	if strings.TrimSpace(e.Code) == "" {
		return fmt.Errorf("asset code is required")
	}
	if strings.TrimSpace(e.Issuer) == "" {
		return fmt.Errorf("asset %s: issuer is required", e.Code)
	}
	// USDC is the settlement asset senders start from; all other registered assets
	// are corridor destination tokens whose peg is mandatory.
	if e.Code != "USDC" {
		if strings.TrimSpace(e.Peg) == "" {
			return fmt.Errorf("asset %s: fiat peg is required for corridor tokens", e.Code)
		}
		if strings.TrimSpace(e.Status) == "" {
			return fmt.Errorf("asset %s: SEP-1 status is required", e.Code)
		}
		if strings.TrimSpace(e.VerificationDate) == "" {
			return fmt.Errorf("asset %s: verification date is required", e.Code)
		}
		if strings.TrimSpace(e.SourceURL) == "" {
			return fmt.Errorf("asset %s: source URL is required", e.Code)
		}
		if strings.TrimSpace(e.HomeDomain) == "" {
			return fmt.Errorf("asset %s: home domain is required", e.Code)
		}
	}
	return nil
}

// registry is the single source of truth for all verified assets and corridors.
// Maps like known, fiatPegs, and homeDomains are automatically derived from this slice.
var registry = []Entry{
	{
		Code:             "USDC",
		Issuer:           USDCIssuer,
		Peg:              "",
		Status:           "unverified",
		VerificationDate: "",
		SourceURL:        "",
		HomeDomain:       "",
	},
	{
		Code:             "NGNC",
		Issuer:           LinkIOIssuer,
		Peg:              "NGN",
		Status:           "live",
		VerificationDate: "2026-08-08",
		SourceURL:        "https://ngnc.online/.well-known/stellar.toml",
		HomeDomain:       "ngnc.online",
	},
	{
		Code:             "GHSC",
		Issuer:           LinkIOIssuer,
		Peg:              "GHS",
		Status:           "pending",
		VerificationDate: "2026-08-08",
		SourceURL:        "https://ngnc.online/.well-known/stellar.toml",
		HomeDomain:       "ngnc.online",
	},
	{
		Code:             "KESC",
		Issuer:           LinkIOIssuer,
		Peg:              "KES",
		Status:           "pending",
		VerificationDate: "2026-08-08",
		SourceURL:        "https://ngnc.online/.well-known/stellar.toml",
		HomeDomain:       "ngnc.online",
	},
	{
		Code:             "NGNT",
		Issuer:           CowrieIssuer,
		Peg:              "NGN",
		Status:           "live",
		VerificationDate: "2026-08-26",
		SourceURL:        "https://cowrie.exchange/.well-known/stellar.toml",
		HomeDomain:       "cowrie.exchange",
	},
	{
		Code:             "USDZ",
		Issuer:           ZeamIssuer,
		Peg:              "USD",
		Status:           "live",
		VerificationDate: "2026-08-26",
		SourceURL:        "https://zeam.money/.well-known/stellar.toml",
		HomeDomain:       "zeam.money",
	},
	{
		Code:             "ZARZ",
		Issuer:           ZeamZARIssuer,
		Peg:              "ZAR",
		Status:           "live",
		VerificationDate: "2026-08-26",
		SourceURL:        "https://zeam.money/.well-known/stellar.toml",
		HomeDomain:       "zeam.money",
	},
	{
		Code:             "EURMTL",
		Issuer:           MonteliberoIssuer,
		Peg:              "EUR",
		Status:           "live",
		VerificationDate: "2026-08-26",
		SourceURL:        "https://mtl.montelibero.org/.well-known/stellar.toml",
		HomeDomain:       "mtl.montelibero.org",
	},
	{
		Code:             "PYUSD",
		Issuer:           PaxosIssuer,
		Peg:              "USD",
		Status:           "live",
		VerificationDate: "2026-08-26",
		SourceURL:        "https://token-metadata.paxos.com/.well-known/stellar.toml",
		HomeDomain:       "token-metadata.paxos.com",
	},
}

var (
	known       = make(map[string]Asset)
	fiatPegs    = make(map[string]string)
	homeDomains = make(map[string]string)
	entries     = make(map[string]Entry)
)

func init() {
	for _, e := range registry {
		if err := ValidateEntry(e); err != nil {
			panic(fmt.Sprintf("asset: invalid registry entry %q: %v", e.Code, err))
		}
		a := Stellar(e.Code, e.Issuer)
		known[e.Code] = a
		if e.Peg != "" {
			fiatPegs[e.Code+":"+e.Issuer] = e.Peg
		}
		if e.HomeDomain != "" {
			homeDomains[e.Issuer] = e.HomeDomain
		}
		entries[e.Code+":"+e.Issuer] = e
	}
}

// USDC is the settlement asset senders start from.
func USDC() Asset { return Stellar("USDC", USDCIssuer) }

// NGNC is the naira-denominated token that terminates the on-chain leg.
// Declared live by its issuer.
func NGNC() Asset { return Stellar("NGNC", LinkIOIssuer) }

// GHSC is the Ghanaian cedi token from the same issuer as NGNC. Its issuer
// declares it status="pending" — not in service.
func GHSC() Asset { return Stellar("GHSC", LinkIOIssuer) }

// KESC is the Kenyan shilling token from the same issuer as NGNC. Its issuer
// declares it status="pending" — not in service.
func KESC() Asset { return Stellar("KESC", LinkIOIssuer) }

// NGNT is the naira token of Cowrie Integrated Systems, declared live by its
// issuer. It is the most widely-held NGNT on mainnet.
func NGNT() Asset { return Stellar("NGNT", CowrieIssuer) }

// USDZ is Zeam's US-dollar token, declared live by its issuer.
func USDZ() Asset { return Stellar("USDZ", ZeamIssuer) }

// ZARZ is Zeam's South African rand token, declared live by its issuer.
func ZARZ() Asset { return Stellar("ZARZ", ZeamZARIssuer) }

// EURMTL is Montelibero's euro stablecoin, declared live by its issuer.
func EURMTL() Asset { return Stellar("EURMTL", MonteliberoIssuer) }

// PYUSD is PayPal USD, issued by Paxos Trust Company, declared live by its
// issuer (the toml omits a status field, which per SEP-1 means live).
func PYUSD() Asset { return Stellar("PYUSD", PaxosIssuer) }

// Lookup resolves a verified token by its code.
//
// It returns false for anything not explicitly verified, so an unrecognised
// code is an error rather than a guess at an issuer.
func Lookup(code string) (Asset, bool) {
	a, ok := known[strings.ToUpper(strings.TrimSpace(code))]
	return a, ok
}

// LookupEntry returns the registration record for a given asset.
func LookupEntry(a Asset) (Entry, bool) {
	if a.Kind != KindStellar || a.Issuer == "" {
		return Entry{}, false
	}
	e, ok := entries[a.Code+":"+a.Issuer]
	return e, ok
}

// LookupEntryByCode returns the registration record for a given asset code.
func LookupEntryByCode(code string) (Entry, bool) {
	a, ok := Lookup(code)
	if !ok {
		return Entry{}, false
	}
	return LookupEntry(a)
}

// Registry returns a copy of all registered entries in the registry.
func Registry() []Entry {
	out := make([]Entry, len(registry))
	copy(out, registry)
	return out
}

// KnownCodes lists the verified token codes, sorted.
func KnownCodes() []string {
	codes := make([]string, 0, len(known))
	for c := range known {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	return codes
}

// HomeDomain reports the domain publishing an asset's stellar.toml, when the
// association has been verified.
//
// Returns false rather than guessing. A checker with no domain reports that it
// could not determine something, which is correct; one sent to a guessed
// domain would report a confident finding about the wrong anchor.
func HomeDomain(a Asset) (string, bool) {
	if a.Kind != KindStellar || a.Issuer == "" {
		return "", false
	}
	d, ok := homeDomains[a.Issuer]
	return d, ok
}

// FiatPeg reports the ISO-4217 currency a Stellar token claims to track, and
// whether the token is a known fiat-pegged asset at all.
//
// An unknown token reports false rather than guessing from its code. "NGNC"
// from an unrecognised issuer is not assumed to track the naira.
func FiatPeg(a Asset) (string, bool) {
	if a.Kind != KindStellar || a.Issuer == "" {
		return "", false
	}
	peg, ok := fiatPegs[a.Code+":"+a.Issuer]
	return peg, ok
}

// IsFiatToken reports whether a is a known fiat-pegged Stellar token.
func IsFiatToken(a Asset) bool {
	_, ok := FiatPeg(a)
	return ok
}

// HopKind classifies a hop asset the way route.classify needs it classified.
//
// The three categories are deliberately distinct, and the distinction is
// written down here rather than implied by map membership:
//
//   - HopFiat is a registered fiat-pegged token. A path through it inherits
//     a fiat dependency.
//   - HopBridge is native XLM or a registered non-fiat token (e.g. USDC).
//     It is deliberately not a fiat dependency.
//   - HopUnknown is a hop that is neither native nor registered. It is
//     treated like a bridge for classification, which is the known,
//     bounded false-negative documented in the package comment: an unknown
//     fiat stablecoin looks exactly like XLM. The gap is surfaced, never
//     guessed at.
type HopKind int

const (
	HopFiat HopKind = iota
	HopBridge
	HopUnknown
)

// String renders the classification for a reader.
func (k HopKind) String() string {
	switch k {
	case HopFiat:
		return "fiat"
	case HopBridge:
		return "bridge"
	default:
		return "unknown"
	}
}

// ClassifyHop reports how a hop asset is treated in corridor classification.
func ClassifyHop(a Asset) HopKind {
	if a.IsNative() {
		return HopBridge
	}
	if a.Kind != KindStellar || a.Issuer == "" {
		return HopUnknown
	}
	e, ok := entries[a.Code+":"+a.Issuer]
	if !ok {
		return HopUnknown
	}
	if e.Peg != "" {
		return HopFiat
	}
	return HopBridge
}

// NGN is off-chain naira — what actually lands in a recipient's bank account.
func NGN() Asset { return Fiat("NGN") }

// GHS is off-chain Ghanaian cedi.
func GHS() Asset { return Fiat("GHS") }

// KES is off-chain Kenyan shilling.
func KES() Asset { return Fiat("KES") }
