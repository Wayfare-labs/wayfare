package dex

import (
	"context"
	"fmt"
)

// ChainHead returns the ledger sequence the network has reached, as reported
// by Horizon's root endpoint.
//
// It is a freshness reference, not a pricing input: it tells a reader how
// current the chain is, which is what lets /healthz answer "how fresh is
// this data?" without any corridor response being parsed. An unavailable
// head is an error here, and every caller that must answer anyway renders it
// as unknown — never zero, which would read as a chain stuck at genesis.
func (c *Client) ChainHead(ctx context.Context) (int64, error) {
	var body struct {
		CoreLedgerSeq int64 `json:"core_ledger_seq"`
	}
	if err := c.get(ctx, "/", nil, &body); err != nil {
		return 0, err
	}
	if body.CoreLedgerSeq <= 0 {
		return 0, fmt.Errorf("dex: horizon root reported core_ledger_seq %d, want a positive ledger", body.CoreLedgerSeq)
	}
	return body.CoreLedgerSeq, nil
}
