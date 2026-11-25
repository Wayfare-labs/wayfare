package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/shopspring/decimal"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

type StaleInfo struct {
	RecordedAt  string `json:"recorded_at"`
	AgeSeconds  int64  `json:"age_seconds"`
	AgeHuman    string `json:"age_human"`
	StoreHash   string `json:"store_hash"`
	StoreHeight int64  `json:"store_height"`
}

type AssetInfo struct {
	Code   string `json:"code"`
	Issuer string `json:"issuer"`
}

type Rung struct {
	Size          decimal.Decimal `json:"size"`
	SendAmount    decimal.Decimal `json:"send_amount"`
	Receive       decimal.Decimal `json:"receive"`
	Mid           decimal.Decimal `json:"mid"`
	EffectiveRate decimal.Decimal `json:"effective_rate"`
	LossPercent   decimal.Decimal `json:"loss_percent"`
	Verdict       string          `json:"verdict"`
	Route         any             `json:"route,omitempty"`
}

type RecommendedRung struct {
	Size          decimal.Decimal `json:"size"`
	SendAmount    decimal.Decimal `json:"send_amount"`
	Receive       decimal.Decimal `json:"receive"`
	Mid           decimal.Decimal `json:"mid"`
	EffectiveRate decimal.Decimal `json:"effective_rate"`
	LossPercent   decimal.Decimal `json:"loss_percent"`
	Verdict       string          `json:"verdict"`
}

type CorridorResponse struct {
	Live        bool             `json:"live"`
	Stale       *StaleInfo       `json:"stale,omitempty"`
	Scored      bool             `json:"scored"`
	Agreement   *decimal.Decimal `json:"agreement,omitempty"`
	AsOfGap     string           `json:"as_of_gap,omitempty"`
	Send        AssetInfo        `json:"send"`
	Receive     AssetInfo        `json:"receive"`
	Integrity   string           `json:"integrity"`
	Recommended *RecommendedRung `json:"recommended"`
	Rungs       []Rung           `json:"rungs"`
	Findings    any              `json:"findings,omitempty"`
}

type CorridorParams struct {
	From string
	To   string
	Live bool
}

func (c *Client) Corridor(ctx context.Context, p CorridorParams) (*CorridorResponse, error) {
	u, err := url.Parse(c.BaseURL + "/api/corridor")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if p.From != "" {
		q.Set("from", p.From)
	}
	if p.To != "" {
		q.Set("to", p.To)
	}
	if p.Live {
		q.Set("live", "1")
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api returned status %d", resp.StatusCode)
	}

	var body CorridorResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return &body, nil
}

type AssetListResponse struct {
	Assets []AssetInfo `json:"assets"`
}

func (c *Client) Assets(ctx context.Context) ([]AssetInfo, error) {
	u := c.BaseURL + "/api/assets"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api returned status %d", resp.StatusCode)
	}

	var body AssetListResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Assets, nil
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	return c.client().Do(req)
}
