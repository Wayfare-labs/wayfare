package route

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

// sequenceLedgers is a LedgerSource that reports a new ledger on every call,
// the way the chain head advances while a ladder is being priced.
type sequenceLedgers struct {
	mu   sync.Mutex
	next int64
	err  error
}

func (s *sequenceLedgers) ChainHead(context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return 0, s.err
	}
	seq := s.next
	s.next += 2
	return seq, nil
}

// ghsLadder prices USDC->GHSC at the one size the chain fixture recorded,
// twice, so the ladder has two priced rungs answered by the same book.
func ghsLadder(t *testing.T, ledgers LedgerSource) *LadderResult {
	t.Helper()
	e := chainEngine(chainSnap(t, "usdc-ghsc-chain-direct-ngnc"), "USD/GHS", "11.7625")
	e.Ledgers = ledgers
	l, err := e.Ladder(context.Background(), LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   asset.GHSC(),
		Sizes:          []decimal.Decimal{decimal.RequireFromString("100"), decimal.RequireFromString("100")},
		ReferenceBase:  "USD",
		ReferenceQuote: "GHS",
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}
	return l
}

func TestLadderRecordsLedgerRangeAcrossRungs(t *testing.T) {
	l := ghsLadder(t, &sequenceLedgers{next: 61_000_000})

	for i, r := range l.Rungs {
		if r.Ledger == 0 {
			t.Errorf("rung %d priced but recorded no ledger", i)
		}
	}
	if l.LedgerRange == nil {
		t.Fatal("LedgerRange is nil; want the span the rungs were observed at")
	}
	if l.LedgerRange.First != 61_000_000 || l.LedgerRange.Last != 61_000_002 {
		t.Errorf("LedgerRange = %+v, want 61000000..61000002", *l.LedgerRange)
	}
	if l.LedgerRange.Span() != 2 {
		t.Errorf("Span = %d, want 2", l.LedgerRange.Span())
	}

	doc := ToCorridorJSON(l, "USD/GHS")
	if doc.LedgerRange == nil || *doc.LedgerRange != (LedgerRangeJSON{First: 61_000_000, Last: 61_000_002, Span: 2}) {
		t.Errorf("wire ledger_range = %+v, want first 61000000 last 61000002 span 2", doc.LedgerRange)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"ledger_range":{"first":61000000,"last":61000002,"span":2}`) {
		t.Errorf("wire document does not state the range:\n%s", raw)
	}
}

func TestLadderWithoutLedgerSourceStatesNoRange(t *testing.T) {
	l := ghsLadder(t, nil)

	if l.LedgerRange != nil {
		t.Errorf("LedgerRange = %+v with no ledger source; want nil (unknown)", *l.LedgerRange)
	}
	raw, err := json.Marshal(ToCorridorJSON(l, "USD/GHS"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"ledger`) {
		t.Errorf("an unknown ledger must be omitted, never published as 0:\n%s", raw)
	}
}

func TestLedgerLookupFailureLeavesTheMeasurementIntact(t *testing.T) {
	withoutSource := ghsLadder(t, nil)
	failing := ghsLadder(t, &sequenceLedgers{err: errors.New("horizon root unavailable")})

	if failing.LedgerRange != nil {
		t.Errorf("LedgerRange = %+v after every lookup failed; want nil", *failing.LedgerRange)
	}
	if failing.Floor.String() != withoutSource.Floor.String() || failing.Integrity != withoutSource.Integrity {
		t.Error("a failed ledger lookup changed the measurement itself")
	}
}

func TestLedgerRangeIgnoresRungsWithNoLedger(t *testing.T) {
	got := ledgerRange([]Rung{{Ledger: 0}, {Ledger: 105}, {Ledger: 101}, {Ledger: 0}, {Ledger: 103}})
	if got == nil || *got != (LedgerRange{First: 101, Last: 105}) {
		t.Errorf("ledgerRange = %+v, want 101..105", got)
	}
	if ledgerRange([]Rung{{}, {}}) != nil {
		t.Error("ledgerRange of rungs with no ledger must be nil")
	}
	if ledgerRange(nil) != nil {
		t.Error("ledgerRange of no rungs must be nil")
	}
}
