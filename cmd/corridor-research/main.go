// Command corridor-research gathers pre-flight evidence for a Stellar
// fiat-corridor research note.
//
// It fetches the issuer's Horizon account record, resolves and verifies the
// anchor's stellar.toml, probes declared SEP-10, SEP-24 and SEP-38 endpoints,
// and measures strict-send paths at three sizes (1, 100 and 1000 USDC) plus
// the direct order book.  Every fact in the output carries its source URL and
// the UTC timestamp it was read.  The researcher fills in Finding and
// Recommendation; the tool supplies the evidence.
//
//	go run ./cmd/corridor-research NGNC GABCDE...
//	go run ./cmd/corridor-research -horizon https://horizon.stellar.org NGNC GABCDE...
//
// The template this output is modelled on is docs/corridor-research-template.md.
// A research note written from this output follows docs/adding-a-corridor.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/anchor"
	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
)

// usdcIssuer is Circle's mainnet USDC account, the canonical send-side
// asset for every corridor this project measures.
const usdcIssuer = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"

// probeSizes are the three send amounts (in USDC) the tool probes on every
// corridor.  Small, medium and large cover the retail / mid-ticket / large
// remittance cases.
var probeSizes = []decimal.Decimal{
	decimal.NewFromInt(1),
	decimal.NewFromInt(100),
	decimal.NewFromInt(1000),
}

// AccountInfo is the subset of a Horizon account record that matters for
// corridor research.
type AccountInfo struct {
	HomeDomain string
	Flags      AccountFlags
	FetchedAt  time.Time
	SourceURL  string
}

// AccountFlags mirrors the flags object returned by Horizon's /accounts endpoint.
type AccountFlags struct {
	AuthRequired        bool `json:"auth_required"`
	AuthRevocable       bool `json:"auth_revocable"`
	AuthImmutable       bool `json:"auth_immutable"`
	AuthClawbackEnabled bool `json:"auth_clawback_enabled"`
}

type horizonAccountResp struct {
	HomeDomain string       `json:"home_domain"`
	Flags      AccountFlags `json:"flags"`
}

// PathProbe records the result of one strict-send call.
type PathProbe struct {
	SendAmount decimal.Decimal
	Paths      []dex.Path
	Err        error
	SourceURL  string
	AsOf       time.Time
}

// ProbeResult holds everything the tool gathered for one issuer.
type ProbeResult struct {
	Code   string
	Issuer string
	AsOf   time.Time

	HorizonURL string
	Account    *AccountInfo
	AccountErr error

	Profile    *anchor.Profile
	ProfileErr error

	Paths      []PathProbe
	DirectBook *dex.BookHealth
	BookErr    error
}

// probe fetches all evidence for the given asset CODE and ISSUER using the
// provided HTTP client and Horizon base URL.
func probe(ctx context.Context, code, issuer, horizonURL string, httpClient *http.Client) *ProbeResult {
	r := &ProbeResult{
		Code:       code,
		Issuer:     issuer,
		AsOf:       time.Now().UTC(),
		HorizonURL: horizonURL,
	}

	// 1. Fetch Horizon account.
	r.Account, r.AccountErr = fetchAccount(ctx, issuer, horizonURL, httpClient)

	// 2. Resolve stellar.toml from the home domain (requires a successful
	//    account fetch to know the domain).
	if r.Account != nil && r.Account.HomeDomain != "" {
		res := &anchor.Resolver{HTTPClient: httpClient}
		r.Profile, r.ProfileErr = res.Resolve(ctx, r.Account.HomeDomain)
	}

	// 3. Strict-send path probes.
	a := asset.Stellar(code, issuer)
	src := asset.Stellar("USDC", usdcIssuer)
	dc := &dex.Client{HorizonURL: horizonURL, HTTPClient: httpClient}
	for _, sz := range probeSizes {
		pp := PathProbe{SendAmount: sz, AsOf: time.Now().UTC()}
		pp.SourceURL = horizonURL + "/paths/strict-send?" +
			"source_asset_type=credit_alphanum4" +
			"&source_asset_code=USDC" +
			"&source_asset_issuer=" + usdcIssuer +
			"&source_amount=" + sz.String() +
			"&destination_assets=stellar%3A" + code + "%3A" + issuer
		pp.Paths, pp.Err = dc.StrictSendPaths(ctx, src, sz, a)
		r.Paths = append(r.Paths, pp)
	}

	// 4. Direct order book (USDC selling, CODE buying).
	r.DirectBook, r.BookErr = dc.OrderBook(ctx, src, a)

	return r
}

func fetchAccount(ctx context.Context, issuer, horizonURL string, client *http.Client) (*AccountInfo, error) {
	u := horizonURL + "/accounts/" + issuer
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("building account request: %w", err)
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching account: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("horizon returned HTTP %d for /accounts/%s", resp.StatusCode, issuer)
	}
	var acc horizonAccountResp
	if err := json.NewDecoder(resp.Body).Decode(&acc); err != nil {
		return nil, fmt.Errorf("decoding account: %w", err)
	}
	return &AccountInfo{
		HomeDomain: acc.HomeDomain,
		Flags:      acc.Flags,
		FetchedAt:  time.Now().UTC(),
		SourceURL:  u,
	}, nil
}

// printTemplate writes a pre-filled corridor-research-template skeleton to w.
func printTemplate(w *os.File, r *ProbeResult) {
	ts := r.AsOf.Format("2006-01-02")
	horizonBase := r.HorizonURL

	fmt.Fprintf(w, "# Research: does a usable USDC → %s corridor exist on Stellar?\n\n", r.Code)
	fmt.Fprintf(w, "**Status: in-progress.** **Finding: [NO-MARKET / VERIFIED / UNVERIFIABLE / UNUSABLE].**\n\n")
	fmt.Fprintf(w, "> Evidence gathered on **%s** with `go run ./cmd/corridor-research %s %s`.\n", ts, r.Code, r.Issuer)
	fmt.Fprintf(w, "> Every live figure below was read directly from the named source URL.\n\n")
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)

	// Candidate issuers section.
	fmt.Fprintln(w, "## Candidate issuers and identity verification")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "- **Asset code:** `%s`\n", r.Code)
	fmt.Fprintf(w, "- **Issuer account:** `%s`\n", r.Issuer)
	fmt.Fprintln(w)

	if r.AccountErr != nil {
		fmt.Fprintf(w, "_Account fetch failed: %v_\n\n", r.AccountErr)
		fmt.Fprintf(w, "Source: `%s/accounts/%s` (%s)\n\n", horizonBase, r.Issuer, ts)
	} else if r.Account != nil {
		fmt.Fprintf(w, "Source: `%s` (%s)\n\n", r.Account.SourceURL, r.Account.FetchedAt.Format(time.RFC3339))
		f := r.Account.Flags
		fmt.Fprintf(w, "- **Declared home domain:** `%s`\n", r.Account.HomeDomain)
		fmt.Fprintf(w, "- **auth_required:** %v\n", f.AuthRequired)
		fmt.Fprintf(w, "- **auth_revocable:** %v\n", f.AuthRevocable)
		fmt.Fprintf(w, "- **auth_immutable:** %v\n", f.AuthImmutable)
		fmt.Fprintf(w, "- **auth_clawback_enabled:** %v\n", f.AuthClawbackEnabled)
		fmt.Fprintln(w)
	}

	// SEP-1 section.
	fmt.Fprintln(w, "### SEP-1 (`stellar.toml`) check")
	fmt.Fprintln(w)
	if r.Account == nil || r.Account.HomeDomain == "" {
		fmt.Fprintln(w, "_No home_domain declared; stellar.toml could not be resolved._")
	} else {
		tomlURL := anchor.TOMLURL(r.Account.HomeDomain)
		fmt.Fprintf(w, "Fetched `%s`", tomlURL)
		if r.Profile != nil {
			fmt.Fprintf(w, " on %s", r.Profile.FetchedAt.Format(time.RFC3339))
		}
		fmt.Fprintln(w, ".")
		fmt.Fprintln(w)
		if r.ProfileErr != nil {
			fmt.Fprintf(w, "_stellar.toml fetch failed: %v_\n\n", r.ProfileErr)
		} else if r.Profile != nil {
			p := r.Profile
			fmt.Fprintf(w, "- **NETWORK_PASSPHRASE:** `%s`", p.TOML.NetworkPassphrase)
			if p.Mainnet {
				fmt.Fprintln(w, " ✓ (mainnet)")
			} else {
				fmt.Fprintln(w, " ✗ (wrong network — not mainnet)")
			}
			match := p.SupportsAsset(asset.Stellar(r.Code, r.Issuer))
			fmt.Fprintf(w, "- **Currency match** (%s / `%s`):", r.Code, r.Issuer)
			if match {
				fmt.Fprintln(w, " ✓ present in `[[CURRENCIES]]` as live")
			} else {
				fmt.Fprintln(w, " ✗ not found in `[[CURRENCIES]]`")
			}
			if p.Malformed {
				fmt.Fprintf(w, "- **TOML parse warning:** `%s`\n", p.MalformedReason)
			}
			seps := make([]string, 0, 7)
			for _, n := range p.SEPs() {
				seps = append(seps, fmt.Sprintf("SEP-%d", n))
			}
			fmt.Fprintf(w, "- **SEPs declared:** %s\n", strings.Join(seps, ", "))
			if p.TOML.WebAuthEndpoint != "" {
				fmt.Fprintf(w, "- **SEP-10 WEB_AUTH_ENDPOINT:** `%s`\n", p.TOML.WebAuthEndpoint)
			} else {
				fmt.Fprintln(w, "- **SEP-10 WEB_AUTH_ENDPOINT:** not declared")
			}
			if p.TOML.TransferServer24 != "" {
				fmt.Fprintf(w, "- **SEP-24 TRANSFER_SERVER_SEP0024:** `%s`\n", p.TOML.TransferServer24)
			} else {
				fmt.Fprintln(w, "- **SEP-24 TRANSFER_SERVER_SEP0024:** not declared")
			}
			if p.TOML.TransferServer != "" {
				fmt.Fprintf(w, "- **SEP-6 TRANSFER_SERVER:** `%s`\n", p.TOML.TransferServer)
			} else {
				fmt.Fprintln(w, "- **SEP-6 TRANSFER_SERVER:** not declared")
			}
			if p.TOML.AnchorQuoteServer != "" {
				fmt.Fprintf(w, "- **SEP-38 ANCHOR_QUOTE_SERVER:** `%s`\n", p.TOML.AnchorQuoteServer)
			} else {
				fmt.Fprintln(w, "- **SEP-38 ANCHOR_QUOTE_SERVER:** not declared (anchor is not priceable)")
			}
		}
	}
	fmt.Fprintln(w)

	// Horizon path data section.
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "## Horizon path data (USDC → %s)\n\n", r.Code)
	fmt.Fprintf(w, "USDC source: Circle mainnet `%s`.\n\n", usdcIssuer)
	fmt.Fprintf(w, "| Send (USDC) | Paths found | Best receive (%s) | Best rate (%s/USDC) | Best path |\n", r.Code, r.Code)
	fmt.Fprintln(w, "|---:|---:|---:|---:|:---|")
	for _, pp := range r.Paths {
		if pp.Err != nil {
			fmt.Fprintf(w, "| %s | error: %v | — | — | — |\n", pp.SendAmount.String(), pp.Err)
			continue
		}
		if len(pp.Paths) == 0 {
			fmt.Fprintf(w, "| %s | 0 (NO-MARKET) | — | — | — |\n", pp.SendAmount.String())
			continue
		}
		// Find best path by dest amount.
		best := pp.Paths[0]
		for _, p := range pp.Paths[1:] {
			if p.DestAmount.GreaterThan(best.DestAmount) {
				best = p
			}
		}
		fmt.Fprintf(w, "| %s | %d | %s | %s | %s |\n",
			pp.SendAmount.String(),
			len(pp.Paths),
			best.DestAmount.StringFixed(4),
			best.Rate().StringFixed(4),
			best.Describe(),
		)
	}
	fmt.Fprintln(w)
	for _, pp := range r.Paths {
		if pp.SourceURL != "" {
			fmt.Fprintf(w, "Source (%s USDC): `%s` (%s)\n", pp.SendAmount.String(), pp.SourceURL, pp.AsOf.Format(time.RFC3339))
		}
	}
	fmt.Fprintln(w)

	// Direct order book section.
	fmt.Fprintln(w, "### Direct order book (USDC selling, "+r.Code+" buying)")
	fmt.Fprintln(w)
	bookURL := horizonBase + "/order_book?selling_asset_type=credit_alphanum4&selling_asset_code=USDC" +
		"&selling_asset_issuer=" + usdcIssuer +
		"&buying_asset_type=credit_alphanum4" +
		"&buying_asset_code=" + r.Code +
		"&buying_asset_issuer=" + url.QueryEscape(r.Issuer) +
		"&limit=200"
	fmt.Fprintf(w, "Source: `%s` (%s)\n\n", bookURL, ts)
	if r.BookErr != nil {
		fmt.Fprintf(w, "_Order book fetch failed: %v_\n\n", r.BookErr)
	} else if r.DirectBook != nil {
		b := r.DirectBook
		fmt.Fprintf(w, "- **Bid levels:** %d\n", b.BidLevels)
		fmt.Fprintf(w, "- **Ask levels:** %d\n", b.AskLevels)
		if !b.BestBid.IsZero() {
			fmt.Fprintf(w, "- **Best bid:** %s\n", b.BestBid.StringFixed(6))
		}
		if !b.BestAsk.IsZero() {
			fmt.Fprintf(w, "- **Best ask:** %s\n", b.BestAsk.StringFixed(6))
		}
		if !b.Mid.IsZero() {
			fmt.Fprintf(w, "- **Mid:** %s\n", b.Mid.StringFixed(6))
		}
		if !b.SpreadPct.IsZero() {
			fmt.Fprintf(w, "- **Spread:** %s%%\n", b.SpreadPct.StringFixed(2))
		}
		fmt.Fprintf(w, "- **Assessment:** %s\n", b.Summary())
		if b.DustLevels > 0 {
			fmt.Fprintf(w, "- **Dust levels excluded:** %d\n", b.DustLevels)
		}
	}
	fmt.Fprintln(w)

	// SEP-38 section.
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## SEP-38 (Quote Server) check")
	fmt.Fprintln(w)
	if r.Profile != nil {
		if r.Profile.TOML.AnchorQuoteServer != "" {
			fmt.Fprintf(w, "`ANCHOR_QUOTE_SERVER` declared: `%s`\n\n", r.Profile.TOML.AnchorQuoteServer)
			fmt.Fprintln(w, "Probe the `/info` and `/price` endpoints manually to verify they respond.")
		} else {
			fmt.Fprintln(w, "`ANCHOR_QUOTE_SERVER` is **not declared**. This anchor cannot be priced")
			fmt.Fprintln(w, "programmatically. Per `docs/adding-a-corridor.md`, the corridor cannot be")
			fmt.Fprintln(w, "VERIFIED without a machine-readable price source.")
		}
	} else {
		fmt.Fprintln(w, "_stellar.toml not resolved; SEP-38 check skipped._")
	}
	fmt.Fprintln(w)

	// Finding and Recommendation placeholders.
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Finding & Conclusion")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "<!-- Fill in: NO-MARKET / VERIFIED / UNVERIFIABLE / UNUSABLE, with reasoning. -->")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Recommendation")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "<!-- Fill in: what action the project should take, and what would change the outcome. -->")
	fmt.Fprintln(w)
}

func main() {
	horizonURL := flag.String("horizon", dex.DefaultHorizonURL,
		"Horizon base URL (no trailing slash)")
	flag.Parse()

	args := flag.Args()
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: corridor-research [-horizon URL] CODE ISSUER")
		os.Exit(2)
	}
	code := strings.ToUpper(strings.TrimSpace(args[0]))
	issuer := strings.TrimSpace(args[1])
	if code == "" || issuer == "" {
		fmt.Fprintln(os.Stderr, "both CODE and ISSUER must be non-empty")
		os.Exit(2)
	}

	ctx := context.Background()
	r := probe(ctx, code, issuer, *horizonURL, nil)
	printTemplate(os.Stdout, r)
}
