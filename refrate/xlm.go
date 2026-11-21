package refrate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
)

// Design notes for non-fiat send assets (XLM):
// - Reference source for XLM crypto/fiat mid: Coinbase public spot price API (XLM-USD).
// - Layer rules remain intact: Corridor keys accept XLM as Send asset, routed via Stellar dex/pathfinding with USD anchoring conversion.

// XLMProvider fetches XLM/USD or crypto mids from public endpoints.
type XLMProvider struct {
	Client *http.Client
	URL    string
}

// NewXLMProvider returns a new XLM provider.
func NewXLMProvider() *XLMProvider {
	return &XLMProvider{
		Client: &http.Client{Timeout: 10 * time.Second},
		URL:    "https://api.coinbase.com/v2/prices/XLM-USD/spot",
	}
}

// FetchMid fetches the current XLM reference rate against USD.
func (p *XLMProvider) FetchMid(ctx context.Context) (Rate, error) {
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	url := p.URL
	if url == "" {
		url = "https://api.coinbase.com/v2/prices/XLM-USD/spot"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return Rate{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Rate{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Rate{}, fmt.Errorf("coinbase xlm spot status %d", resp.StatusCode)
	}

	var res struct {
		Data struct {
			Base     string `json:"base"`
			Currency string `json:"currency"`
			Amount   string `json:"amount"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return Rate{}, err
	}

	mid, err := decimal.NewFromString(res.Data.Amount)
	if err != nil {
		return Rate{}, err
	}

	now := time.Now().UTC()
	return Rate{
		Base:      "XLM",
		Quote:     "USD",
		Mid:       mid,
		AsOf:      now,
		Source:    "coinbase",
		FetchedAt: now,
	}, nil
}
