package dex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

func TestCompareOrderBooks_Agree(t *testing.T) {
	selling := asset.Stellar("USDC", "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN")
	buying := asset.Stellar("NGNC", "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6")

	primary := &BookHealth{
		Selling:   selling,
		Buying:    buying,
		BidLevels: 5,
		AskLevels: 5,
		BestBid:   decimal.RequireFromString("1500.00"),
		BestAsk:   decimal.RequireFromString("1510.00"),
		Mid:       decimal.RequireFromString("1505.00"),
		SpreadPct: decimal.RequireFromString("0.6644"),
	}

	secondary := &BookHealth{
		Selling:   selling,
		Buying:    buying,
		BidLevels: 4,
		AskLevels: 6,
		BestBid:   decimal.RequireFromString("1501.00"),
		BestAsk:   decimal.RequireFromString("1509.50"),
		Mid:       decimal.RequireFromString("1505.25"),
		SpreadPct: decimal.RequireFromString("0.5647"),
	}

	res := CompareOrderBooks(primary, secondary, decimal.NewFromInt(2))
	if !res.Agrees {
		t.Errorf("expected agreement, got notes: %v", res.Notes)
	}
	if res.Verdict != VerdictAgree {
		t.Errorf("expected %s, got %s", VerdictAgree, res.Verdict)
	}
}

func TestCompareOrderBooks_DisagreeOnPrice(t *testing.T) {
	selling := asset.Stellar("USDC", "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN")
	buying := asset.Stellar("NGNC", "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6")

	primary := &BookHealth{
		Selling:   selling,
		Buying:    buying,
		BidLevels: 5,
		AskLevels: 5,
		BestBid:   decimal.RequireFromString("1500.00"),
		BestAsk:   decimal.RequireFromString("1510.00"),
		Mid:       decimal.RequireFromString("1505.00"),
		SpreadPct: decimal.RequireFromString("0.66"),
	}

	secondary := &BookHealth{
		Selling:   selling,
		Buying:    buying,
		BidLevels: 5,
		AskLevels: 5,
		BestBid:   decimal.RequireFromString("1600.00"),
		BestAsk:   decimal.RequireFromString("1610.00"),
		Mid:       decimal.RequireFromString("1605.00"),
		SpreadPct: decimal.RequireFromString("0.62"),
	}

	res := CompareOrderBooks(primary, secondary, decimal.NewFromInt(2))
	if res.Agrees {
		t.Errorf("expected disagreement due to mid divergence, got agrees=true")
	}
	if res.Verdict != VerdictDisagree {
		t.Errorf("expected %s, got %s", VerdictDisagree, res.Verdict)
	}
}

func TestCompareOrderBooks_EmptyBooks(t *testing.T) {
	selling := asset.Stellar("USDC", "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN")
	buying := asset.Stellar("NGNC", "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6")

	emptyA := &BookHealth{Selling: selling, Buying: buying, BidLevels: 0, AskLevels: 0}
	emptyB := &BookHealth{Selling: selling, Buying: buying, BidLevels: 0, AskLevels: 0}

	res := CompareOrderBooks(emptyA, emptyB, decimal.NewFromInt(2))
	if !res.Agrees || res.Verdict != VerdictBothEmpty {
		t.Errorf("expected VerdictBothEmpty, got %s (agrees=%v)", res.Verdict, res.Agrees)
	}

	hasBook := &BookHealth{Selling: selling, Buying: buying, BidLevels: 1, AskLevels: 1, BestBid: decimal.NewFromInt(100), BestAsk: decimal.NewFromInt(101)}
	resPrimaryEmpty := CompareOrderBooks(emptyA, hasBook, decimal.NewFromInt(2))
	if resPrimaryEmpty.Verdict != VerdictPrimaryEmpty {
		t.Errorf("expected VerdictPrimaryEmpty, got %s", resPrimaryEmpty.Verdict)
	}

	resSecondaryEmpty := CompareOrderBooks(hasBook, emptyA, decimal.NewFromInt(2))
	if resSecondaryEmpty.Verdict != VerdictSecondaryEmpty {
		t.Errorf("expected VerdictSecondaryEmpty, got %s", resSecondaryEmpty.Verdict)
	}
}

func TestRPCOfferCheck_VerifyOffer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"result": {
				"latestLedger": 52140192,
				"entries": [
					{
						"key": "AAAAAgAAAAD...",
						"xdr": "AAAAAgAAAAD...",
						"lastModifiedLedgerSeq": 52140100
					}
				]
			}
		}`))
	}))
	defer srv.Close()

	checker := &RPCOfferCheck{RPCURL: srv.URL}
	res, err := checker.VerifyOfferOnLedger(context.Background(), "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6", 12345, "AAAAAgAAAAD...")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Verified || !res.FoundOnChain {
		t.Errorf("expected verified=true, got %v", res.Verified)
	}
	if res.LedgerSeq != 52140192 {
		t.Errorf("expected ledger seq 52140192, got %d", res.LedgerSeq)
	}
}

func TestRPCOfferCheck_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"result": {
				"latestLedger": 52140192,
				"entries": []
			}
		}`))
	}))
	defer srv.Close()

	checker := &RPCOfferCheck{RPCURL: srv.URL}
	res, err := checker.VerifyOfferOnLedger(context.Background(), "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6", 99999, "AAAAAgAAAAD...")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.FoundOnChain || res.Verified {
		t.Errorf("expected found_on_chain=false, got true")
	}
}
