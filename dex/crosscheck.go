package dex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

// CrossCheckVerdict states the agreement status between primary Horizon and
// the secondary source.
type CrossCheckVerdict string

const (
	// VerdictAgree means the two order books match within the allowable tolerance.
	VerdictAgree CrossCheckVerdict = "AGREE"

	// VerdictDisagree means both report books, but prices or spreads diverge beyond tolerance.
	VerdictDisagree CrossCheckVerdict = "DISAGREE"

	// VerdictPrimaryEmpty means Horizon reports no book, but the secondary found one.
	VerdictPrimaryEmpty CrossCheckVerdict = "PRIMARY_EMPTY"

	// VerdictSecondaryEmpty means Horizon reports an active book, but the secondary has none.
	VerdictSecondaryEmpty CrossCheckVerdict = "SECONDARY_EMPTY"

	// VerdictBothEmpty means both sources confirm no bids or asks exist.
	VerdictBothEmpty CrossCheckVerdict = "BOTH_EMPTY"
)

// CrossCheckResult summarizes the differential comparison between Horizon's
// direct order book and an independent second source (issue #727).
type CrossCheckResult struct {
	Selling   asset.Asset       `json:"selling"`
	Buying    asset.Asset       `json:"buying"`
	Verdict   CrossCheckVerdict `json:"verdict"`
	Agrees    bool              `json:"agrees"`
	Primary   *BookHealth       `json:"primary"`
	Secondary *BookHealth       `json:"secondary"`

	// Percentage differences between secondary and primary ((secondary - primary) / primary * 100).
	BidDiffPct   decimal.Decimal `json:"bid_diff_pct"`
	AskDiffPct   decimal.Decimal `json:"ask_diff_pct"`
	MidDiffPct   decimal.Decimal `json:"mid_diff_pct"`
	SpreadDiffPp decimal.Decimal `json:"spread_diff_pp"`

	Notes []string `json:"notes"`
}

// DefaultMaxDivergencePct is the default maximum price divergence percentage
// allowed before declaring DISAGREE (2.0%).
var DefaultMaxDivergencePct = decimal.NewFromInt(2)

// CompareOrderBooks evaluates a primary BookHealth (from Horizon) against a
// secondary BookHealth (e.g. from StellarExpert, secondary node, or RPC reconstruction).
func CompareOrderBooks(primary, secondary *BookHealth, maxDivergence decimal.Decimal) CrossCheckResult {
	if maxDivergence.IsZero() || maxDivergence.IsNegative() {
		maxDivergence = DefaultMaxDivergencePct
	}

	res := CrossCheckResult{
		Selling:   primary.Selling,
		Buying:    primary.Buying,
		Primary:   primary,
		Secondary: secondary,
		Notes:     []string{},
	}

	primaryHasBook := primary.BidLevels > 0 || primary.AskLevels > 0
	secondaryHasBook := secondary.BidLevels > 0 || secondary.AskLevels > 0

	switch {
	case !primaryHasBook && !secondaryHasBook:
		res.Verdict = VerdictBothEmpty
		res.Agrees = true
		res.Notes = append(res.Notes, "both sources confirm no executable order book exists on the DEX")
		return res

	case !primaryHasBook && secondaryHasBook:
		res.Verdict = VerdictPrimaryEmpty
		res.Agrees = false
		res.Notes = append(res.Notes, "Horizon reports empty order book, but secondary source reports liquidity")
		return res

	case primaryHasBook && !secondaryHasBook:
		res.Verdict = VerdictSecondaryEmpty
		res.Agrees = false
		res.Notes = append(res.Notes, "Horizon reports active order book, but secondary source reports no liquidity")
		return res
	}

	// Both report active books. Compare mid, bid, ask, and spread.
	res.Agrees = true
	res.Verdict = VerdictAgree

	// Mid comparison
	if !primary.Mid.IsZero() && !secondary.Mid.IsZero() {
		diff := secondary.Mid.Sub(primary.Mid).Div(primary.Mid).Mul(decimal.NewFromInt(100))
		res.MidDiffPct = diff
		if diff.Abs().GreaterThan(maxDivergence) {
			res.Agrees = false
			res.Verdict = VerdictDisagree
			res.Notes = append(res.Notes, fmt.Sprintf("mid price diverges by %s%% (exceeds %s%% limit)", diff.StringFixed(2), maxDivergence.StringFixed(2)))
		}
	}

	// Best bid comparison
	if !primary.BestBid.IsZero() && !secondary.BestBid.IsZero() {
		diff := secondary.BestBid.Sub(primary.BestBid).Div(primary.BestBid).Mul(decimal.NewFromInt(100))
		res.BidDiffPct = diff
		if diff.Abs().GreaterThan(maxDivergence) {
			res.Agrees = false
			res.Verdict = VerdictDisagree
			res.Notes = append(res.Notes, fmt.Sprintf("best bid diverges by %s%%", diff.StringFixed(2)))
		}
	}

	// Best ask comparison
	if !primary.BestAsk.IsZero() && !secondary.BestAsk.IsZero() {
		diff := secondary.BestAsk.Sub(primary.BestAsk).Div(primary.BestAsk).Mul(decimal.NewFromInt(100))
		res.AskDiffPct = diff
		if diff.Abs().GreaterThan(maxDivergence) {
			res.Agrees = false
			res.Verdict = VerdictDisagree
			res.Notes = append(res.Notes, fmt.Sprintf("best ask diverges by %s%%", diff.StringFixed(2)))
		}
	}

	// Spread difference in percentage points
	if !primary.SpreadPct.IsZero() && !secondary.SpreadPct.IsZero() {
		res.SpreadDiffPp = secondary.SpreadPct.Sub(primary.SpreadPct)
	}

	if res.Agrees {
		res.Notes = append(res.Notes, "independent cross-check confirmed order book prices within tolerance")
	}

	return res
}

// RPCOfferCheck probes Stellar RPC getLedgerEntries to verify that a specific
// offer reported by Horizon exists in the current ledger state.
type RPCOfferCheck struct {
	RPCURL     string
	HTTPClient *http.Client
}

// RPCOfferVerification is the finding of verifying an offer via RPC.
type RPCOfferVerification struct {
	Seller       string          `json:"seller"`
	OfferID      int64           `json:"offer_id"`
	FoundOnChain bool            `json:"found_on_chain"`
	SellingAsset string          `json:"selling_asset"`
	BuyingAsset  string          `json:"buying_asset"`
	Amount       decimal.Decimal `json:"amount"`
	Price        decimal.Decimal `json:"price"`
	LedgerSeq    uint32          `json:"ledger_seq"`
	Verified     bool            `json:"verified"`
	Error        string          `json:"error,omitempty"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type rpcLedgerEntriesResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Result  struct {
		LatestLedger uint32 `json:"latestLedger"`
		Entries      []struct {
			Key                string `json:"key"`
			XDR                string `json:"xdr"`
			LastModifiedLedger uint32 `json:"lastModifiedLedgerSeq"`
		} `json:"entries"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// VerifyOfferOnLedger checks whether an offer (identified by seller and offerID)
// is recorded in current ledger state via Stellar RPC getLedgerEntries.
func (c *RPCOfferCheck) VerifyOfferOnLedger(ctx context.Context, seller string, offerID int64, keyXDR string) (*RPCOfferVerification, error) {
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	reqBody := rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "getLedgerEntries",
		Params: map[string]any{
			"keys": []string{keyXDR},
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("encoding rpc request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.RPCURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("building rpc request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rpc call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rpc endpoint returned HTTP %d", resp.StatusCode)
	}

	var rpcResp rpcLedgerEntriesResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("decoding rpc response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	ver := &RPCOfferVerification{
		Seller:       seller,
		OfferID:      offerID,
		LedgerSeq:    rpcResp.Result.LatestLedger,
		FoundOnChain: len(rpcResp.Result.Entries) > 0,
	}
	if ver.FoundOnChain {
		ver.Verified = true
	}
	return ver, nil
}
