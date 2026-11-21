package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Wayfare-labs/wayfare/anchor"
)

// CandidateFiatToken records a discovered asset with its stellar.toml evidence.
type CandidateFiatToken struct {
	AssetCode       string `json:"asset_code"`
	AssetIssuer     string `json:"asset_issuer"`
	HomeDomain      string `json:"home_domain"`
	Holders         int    `json:"holders"`
	AnchorAsset     string `json:"anchor_asset"`
	AnchorAssetType string `json:"anchor_asset_type"`
	Status          string `json:"status"`
	QuoteServer     string `json:"quote_server"`
	TOMLURL         string `json:"toml_url"`
	Evidence        string `json:"evidence"`
}

// Config configures candidate discovery.
type Config struct {
	Source           string // "stellarexpert" (default) or "horizon"
	HorizonURL       string
	StellarExpertURL string
	Limit            int
	RateLimitDelay   time.Duration
	OutputCSV        string
	HTTPClient       *http.Client
}

// Standard fiat ISO codes and known fiat pegs.
var knownFiatCodes = map[string]bool{
	"USD": true, "EUR": true, "GBP": true, "NGN": true, "GHS": true,
	"KES": true, "BRL": true, "ZAR": true, "PHP": true, "INR": true,
	"MXN": true, "ARS": true, "CAD": true, "AUD": true, "JPY": true,
	"CHF": true, "TZS": true, "UGX": true, "XOF": true, "XAF": true,
	"COP": true, "CLP": true, "PEN": true, "TRY": true, "SGD": true,
	"IDR": true, "IDRT": true,
}

// IsFiatCandidate evaluates whether an asset's TOML currency entry indicates
// a fiat-pegged token.
func IsFiatCandidate(c anchor.Currency) bool {
	if strings.EqualFold(c.AnchorAssetType, "fiat") {
		return true
	}
	anchorCode := strings.ToUpper(strings.TrimSpace(c.AnchorAsset))
	if knownFiatCodes[anchorCode] {
		return true
	}
	// Check for common ISO currency code format if is_asset_anchored is true
	if c.IsAssetAnchored && len(anchorCode) == 3 && anchorCode != strings.ToUpper(c.Code) {
		return true
	}
	return false
}

type horizonAssetRecord struct {
	AssetCode   string `json:"asset_code"`
	AssetIssuer string `json:"asset_issuer"`
	Accounts    struct {
		Authorized int `json:"authorized"`
	} `json:"accounts"`
	Links struct {
		Toml struct {
			Href string `json:"href"`
		} `json:"toml"`
	} `json:"_links"`
}

type horizonAssetsResponse struct {
	Embedded struct {
		Records []horizonAssetRecord `json:"records"`
	} `json:"_embedded"`
	Links struct {
		Next struct {
			Href string `json:"href"`
		} `json:"next"`
	} `json:"_links"`
}

type expertAssetRecord struct {
	Asset      string `json:"asset"`
	Code       string `json:"code"`
	Domain     string `json:"domain"`
	Trustlines struct {
		Authorized int `json:"authorized"`
		Funded     int `json:"funded"`
		Total      int `json:"total"`
	} `json:"trustlines"`
	TomlInfo struct {
		Code            string `json:"code"`
		Issuer          string `json:"issuer"`
		AnchorAsset     string `json:"anchorAsset"`
		AnchorAssetType string `json:"anchorAssetType"`
		Status          string `json:"status"`
	} `json:"tomlInfo"`
}

type expertAssetsResponse struct {
	Embedded struct {
		Records []expertAssetRecord `json:"records"`
	} `json:"_embedded"`
	Links struct {
		Next struct {
			Href string `json:"href"`
		} `json:"next"`
	} `json:"_links"`
}

// DiscoverCandidates walks StellarExpert's asset directory or Horizon assets,
// resolves each issuer's stellar.toml, and extracts assets declaring a fiat anchor_asset (issue #726).
func DiscoverCandidates(ctx context.Context, cfg Config) ([]CandidateFiatToken, error) {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	if cfg.Source == "horizon" || (cfg.Source == "" && cfg.HorizonURL != "" && cfg.StellarExpertURL == "") {
		return discoverFromHorizon(ctx, cfg, client)
	}
	return discoverFromStellarExpert(ctx, cfg, client)
}

func discoverFromHorizon(ctx context.Context, cfg Config, client *http.Client) ([]CandidateFiatToken, error) {
	resolver := &anchor.Resolver{HTTPClient: client}
	tomlCache := make(map[string]*anchor.Profile)

	var candidates []CandidateFiatToken
	seenTokens := make(map[string]bool)

	nextURL := fmt.Sprintf("%s/assets?order=asc&limit=200", strings.TrimSuffix(cfg.HorizonURL, "/"))
	processed := 0

	for nextURL != "" && processed < cfg.Limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, fmt.Errorf("building request: %w", err)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("querying assets: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			time.Sleep(2 * time.Second)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("horizon returned HTTP %d for %s", resp.StatusCode, nextURL)
		}

		var page horizonAssetsResponse
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decoding horizon assets: %w", err)
		}

		if len(page.Embedded.Records) == 0 {
			break
		}

		for _, rec := range page.Embedded.Records {
			processed++
			if processed > cfg.Limit {
				break
			}

			tokenKey := rec.AssetCode + ":" + rec.AssetIssuer
			if seenTokens[tokenKey] {
				continue
			}
			seenTokens[tokenKey] = true

			tomlHref := rec.Links.Toml.Href
			if tomlHref == "" {
				continue
			}

			u, err := url.Parse(tomlHref)
			if err != nil || u.Host == "" {
				continue
			}
			domain := u.Host

			profile, ok := tomlCache[domain]
			if !ok {
				if cfg.RateLimitDelay > 0 {
					time.Sleep(cfg.RateLimitDelay)
				}
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, tomlHref, nil)
				if err != nil {
					continue
				}
				resp, err := client.Do(req)
				if err != nil {
					p, err := resolver.Resolve(ctx, domain)
					if err == nil {
						profile = p
					}
				} else {
					if resp.StatusCode == http.StatusOK {
						raw, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024))
						resp.Body.Close()
						if err == nil {
							profile = anchor.ProfileFromRaw(domain, raw)
						}
					} else {
						resp.Body.Close()
					}
				}
				if profile != nil {
					tomlCache[domain] = profile
				}
			}

			if profile == nil {
				continue
			}

			for _, curr := range profile.TOML.Currencies {
				if strings.EqualFold(curr.Code, rec.AssetCode) && (curr.Issuer == "" || curr.Issuer == rec.AssetIssuer) {
					if IsFiatCandidate(curr) {
						evidence := fmt.Sprintf("anchor_asset=%q; anchor_asset_type=%q; is_asset_anchored=%v",
							curr.AnchorAsset, curr.AnchorAssetType, curr.IsAssetAnchored)
						candidates = append(candidates, CandidateFiatToken{
							AssetCode:       rec.AssetCode,
							AssetIssuer:     rec.AssetIssuer,
							HomeDomain:      domain,
							Holders:         rec.Accounts.Authorized,
							AnchorAsset:     curr.AnchorAsset,
							AnchorAssetType: curr.AnchorAssetType,
							Status:          curr.Status,
							QuoteServer:     profile.TOML.AnchorQuoteServer,
							TOMLURL:         anchor.TOMLURL(domain),
							Evidence:        evidence,
						})
					}
				}
			}
		}

		nextURL = page.Links.Next.Href
		if cfg.RateLimitDelay > 0 {
			time.Sleep(cfg.RateLimitDelay)
		}
	}

	return candidates, nil
}

func discoverFromStellarExpert(ctx context.Context, cfg Config, client *http.Client) ([]CandidateFiatToken, error) {
	baseURL := strings.TrimSuffix(cfg.StellarExpertURL, "/")
	if baseURL == "" {
		baseURL = "https://api.stellar.expert"
	}

	resolver := &anchor.Resolver{HTTPClient: client}
	tomlCache := make(map[string]*anchor.Profile)

	var candidates []CandidateFiatToken
	seenTokens := make(map[string]bool)

	nextURL := fmt.Sprintf("%s/explorer/public/asset?sort=rating&order=desc&limit=50", baseURL)
	processed := 0

	for nextURL != "" && processed < cfg.Limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, fmt.Errorf("building request: %w", err)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("querying stellarexpert: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			time.Sleep(2 * time.Second)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("stellarexpert returned HTTP %d for %s", resp.StatusCode, nextURL)
		}

		var page expertAssetsResponse
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decoding stellarexpert assets: %w", err)
		}

		if len(page.Embedded.Records) == 0 {
			break
		}

		for _, rec := range page.Embedded.Records {
			processed++
			if processed > cfg.Limit {
				break
			}

			if rec.Code == "XLM" || rec.Asset == "XLM" {
				continue
			}

			issuer := rec.TomlInfo.Issuer
			if issuer == "" {
				// Asset format: CODE-ISSUER-ID
				parts := strings.Split(rec.Asset, "-")
				if len(parts) >= 2 {
					issuer = parts[1]
				}
			}

			tokenKey := rec.Code + ":" + issuer
			if seenTokens[tokenKey] {
				continue
			}
			seenTokens[tokenKey] = true

			domain := rec.Domain
			if domain == "" {
				continue
			}

			profile, ok := tomlCache[domain]
			if !ok {
				if cfg.RateLimitDelay > 0 {
					time.Sleep(cfg.RateLimitDelay)
				}
				p, err := resolver.Resolve(ctx, domain)
				if err == nil {
					profile = p
				}
				if profile != nil {
					tomlCache[domain] = profile
				}
			}

			if profile == nil {
				// Fallback to tomlInfo from StellarExpert if direct TOML resolution failed
				if strings.EqualFold(rec.TomlInfo.AnchorAssetType, "fiat") || knownFiatCodes[strings.ToUpper(rec.TomlInfo.AnchorAsset)] {
					evidence := fmt.Sprintf("anchor_asset=%q; anchor_asset_type=%q (via stellarexpert directory)",
						rec.TomlInfo.AnchorAsset, rec.TomlInfo.AnchorAssetType)
					holders := rec.Trustlines.Authorized
					if holders == 0 {
						holders = rec.Trustlines.Total
					}
					candidates = append(candidates, CandidateFiatToken{
						AssetCode:       rec.Code,
						AssetIssuer:     issuer,
						HomeDomain:      domain,
						Holders:         holders,
						AnchorAsset:     rec.TomlInfo.AnchorAsset,
						AnchorAssetType: rec.TomlInfo.AnchorAssetType,
						Status:          rec.TomlInfo.Status,
						QuoteServer:     "",
						TOMLURL:         anchor.TOMLURL(domain),
						Evidence:        evidence,
					})
				}
				continue
			}

			matched := false
			for _, curr := range profile.TOML.Currencies {
				if strings.EqualFold(curr.Code, rec.Code) && (curr.Issuer == "" || curr.Issuer == issuer) {
					if IsFiatCandidate(curr) {
						matched = true
						evidence := fmt.Sprintf("anchor_asset=%q; anchor_asset_type=%q; is_asset_anchored=%v",
							curr.AnchorAsset, curr.AnchorAssetType, curr.IsAssetAnchored)
						holders := rec.Trustlines.Authorized
						if holders == 0 {
							holders = rec.Trustlines.Total
						}
						candidates = append(candidates, CandidateFiatToken{
							AssetCode:       rec.Code,
							AssetIssuer:     issuer,
							HomeDomain:      domain,
							Holders:         holders,
							AnchorAsset:     curr.AnchorAsset,
							AnchorAssetType: curr.AnchorAssetType,
							Status:          curr.Status,
							QuoteServer:     profile.TOML.AnchorQuoteServer,
							TOMLURL:         anchor.TOMLURL(domain),
							Evidence:        evidence,
						})
					}
				}
			}

			// If domain TOML resolved but didn't list this specific code under CURRENCIES,
			// check if StellarExpert's tomlInfo was fiat
			if !matched && (strings.EqualFold(rec.TomlInfo.AnchorAssetType, "fiat") || knownFiatCodes[strings.ToUpper(rec.TomlInfo.AnchorAsset)]) {
				evidence := fmt.Sprintf("anchor_asset=%q; anchor_asset_type=%q (via stellarexpert directory)",
					rec.TomlInfo.AnchorAsset, rec.TomlInfo.AnchorAssetType)
				holders := rec.Trustlines.Authorized
				if holders == 0 {
					holders = rec.Trustlines.Total
				}
				candidates = append(candidates, CandidateFiatToken{
					AssetCode:       rec.Code,
					AssetIssuer:     issuer,
					HomeDomain:      domain,
					Holders:         holders,
					AnchorAsset:     rec.TomlInfo.AnchorAsset,
					AnchorAssetType: rec.TomlInfo.AnchorAssetType,
					Status:          rec.TomlInfo.Status,
					QuoteServer:     profile.TOML.AnchorQuoteServer,
					TOMLURL:         anchor.TOMLURL(domain),
					Evidence:        evidence,
				})
			}
		}

		nextHref := page.Links.Next.Href
		if nextHref == "" {
			break
		}
		if strings.HasPrefix(nextHref, "http") {
			nextURL = nextHref
		} else {
			nextURL = baseURL + nextHref
		}
		if cfg.RateLimitDelay > 0 {
			time.Sleep(cfg.RateLimitDelay)
		}
	}

	return candidates, nil
}

// WriteCSV exports candidates to a CSV file.
func WriteCSV(w io.Writer, candidates []CandidateFiatToken) error {
	cw := csv.NewWriter(w)
	header := []string{
		"asset_code", "asset_issuer", "home_domain", "holders",
		"anchor_asset", "anchor_asset_type", "status", "quote_server", "toml_url", "evidence",
	}
	if err := cw.Write(header); err != nil {
		return err
	}

	for _, c := range candidates {
		row := []string{
			c.AssetCode,
			c.AssetIssuer,
			c.HomeDomain,
			strconv.Itoa(c.Holders),
			c.AnchorAsset,
			c.AnchorAssetType,
			c.Status,
			c.QuoteServer,
			c.TOMLURL,
			c.Evidence,
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func main() {
	source := flag.String("source", "stellarexpert", "Asset directory source ('stellarexpert' or 'horizon')")
	horizon := flag.String("horizon", "https://horizon.stellar.org", "Horizon API base URL")
	expertURL := flag.String("stellarexpert", "https://api.stellar.expert", "StellarExpert API base URL")
	limit := flag.Int("limit", 200, "Maximum assets to inspect")
	delayMs := flag.Int("delay-ms", 100, "Rate-limit delay in milliseconds between requests")
	outPath := flag.String("output", "data/candidate-fiat-tokens-2026-11-20.csv", "Output CSV file path")
	flag.Parse()

	cfg := Config{
		Source:           *source,
		HorizonURL:       *horizon,
		StellarExpertURL: *expertURL,
		Limit:            *limit,
		RateLimitDelay:   time.Duration(*delayMs) * time.Millisecond,
		OutputCSV:        *outPath,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Fprintf(os.Stderr, "Discovering candidate fiat tokens via %s (limit %d)...\n", cfg.Source, cfg.Limit)
	candidates, err := DiscoverCandidates(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Discovered %d candidate fiat token(s). Writing to %s\n", len(candidates), cfg.OutputCSV)

	if err := os.MkdirAll(filepath.Dir(cfg.OutputCSV), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Creating output dir: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Create(cfg.OutputCSV)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Creating output file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	if err := WriteCSV(f, candidates); err != nil {
		fmt.Fprintf(os.Stderr, "Writing CSV: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Done.\n")
}
