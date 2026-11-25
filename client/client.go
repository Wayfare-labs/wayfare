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

type CorridorParams struct {
	From string
	To   string
	Live bool
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

// Client is a typed Go client for the Wayfare API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// New creates a new API client.
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

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	return c.client().Do(req)
}

// CorridorResponse represents the response from GET /api/corridor.
type CorridorResponse struct {
	SendAsset         string                 `json:"send_asset"`
	ReceiveAsset      string                 `json:"receive_asset"`
	Integrity         string                 `json:"integrity"`
	ReferenceMid      decimal.Decimal        `json:"reference_mid"`
	ReferenceSource   string                 `json:"reference_source"`
	Scored            bool                   `json:"scored"`
	Recommended       *decimal.Decimal       `json:"recommended"`
	FloorLossPct      decimal.Decimal        `json:"floor_loss_pct"`
	WorstLossPct      decimal.Decimal        `json:"worst_loss_pct"`
	Live              bool                   `json:"live"`
	MeasuredAt        time.Time              `json:"measured_at"`
}

// GetCorridor fetches corridor measurements.
func (c *Client) GetCorridor(ctx context.Context, from, to string, live bool) (*CorridorResponse, error {
	u, err := url.Parse(c.BaseURL + "/api/corridor")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if from != "" {
		q.Set("from", from)
	}
	if to != "" {
		q.Set("to", to)
	}
	if live {
		q.Set("live", "1")
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var target struct {
		SendAsset         string           `json:"send_asset"`
		ReceiveAsset      string           `json:"receive_asset"`
		Integrity         string           `json:"integrity"`
		ReferenceMid      string           `json:"reference_mid"`
		ReferenceSource   string           `json:"reference_source"`
		Scored            bool             `json:"scored"`
		Recommended       *string          `json:"recommended"`
		FloorLossPct      string           `json:"floor_loss_pct"`
		WorstLossPct      string           `json:"worst_loss_pct"`
		Live              bool             `json:"live"`
		MeasuredAt        time.Time        `json:"measured_at"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&target); err != nil {
		return nil, err
	}

	res := &CorridorResponse{
		SendAsset:       target.SendAsset,
		ReceiveAsset:    target.ReceiveAsset,
		Integrity:       target.Integrity,
		ReferenceSource: target.ReferenceSource,
		Scored:          target.Scored,
		Live:            target.Live,
		MeasuredAt:      target.MeasuredAt,
	}

	if target.ReferenceMid != "" {
		res.ReferenceMid, _ = decimal.NewFromString(target.ReferenceMid)
	}
	if target.FloorLossPct != "" {
		res.FloorLossPct, _ = decimal.NewFromString(target.FloorLossPct)
	}
	if target.WorstLossPct != "" {
		res.WorstLossPct, _ = decimal.NewFromString(target.WorstLossPct)
	}
	if target.Recommended != nil {
		d, _ := decimal.NewFromString(*target.Recommended)
		res.Recommended = &d
	}

	return res, nil
}
