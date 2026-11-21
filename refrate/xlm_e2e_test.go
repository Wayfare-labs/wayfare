package refrate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
)

func TestXLMProviderEndToEnd(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"base":"XLM","currency":"USD","amount":"0.123456"}}`))
	}))
	defer ts.Close()

	p := &XLMProvider{
		Client: ts.Client(),
		URL:    ts.URL,
	}
	ctx := context.Background()
	rate, err := p.FetchMid(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate.Base != "XLM" || rate.Quote != "USD" {
		t.Errorf("unexpected rate pair: %s/%s", rate.Base, rate.Quote)
	}
	if !rate.Mid.Equal(decimal.RequireFromString("0.123456")) {
		t.Errorf("unexpected mid: %s", rate.Mid)
	}
}
