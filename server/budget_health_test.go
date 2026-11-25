package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wayfare-labs/wayfare/transport"
)

func healthzBody(t *testing.T, s *Server) map[string]any {
	t.Helper()
	api := httptest.NewServer(s.Handler())
	t.Cleanup(api.Close)

	resp, err := http.Get(api.URL + "/healthz")
	if err != nil {
		t.Fatalf("get /healthz: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /healthz: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz status = %d", resp.StatusCode)
	}
	return body
}

func TestHealthzReportsTheOutboundBudget(t *testing.T) {
	budget := transport.NewBudget(6, 12)
	s := &Server{Budget: budget}

	body := healthzBody(t, s)

	raw, ok := body["budget"]
	if !ok || raw == nil {
		t.Fatalf("budget missing from /healthz: %v", body["budget"])
	}
	stats, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("budget is %T, want an object", raw)
	}
	if got := stats["capacity"]; got != float64(12) {
		t.Errorf("capacity = %v, want 12", got)
	}
	if got := stats["refill_per_second"]; got != float64(6) {
		t.Errorf("refill_per_second = %v, want 6", got)
	}
}

func TestHealthzOmitsTheBudgetWhenThereIsNone(t *testing.T) {
	s := &Server{}

	body := healthzBody(t, s)

	if raw, ok := body["budget"]; !ok {
		t.Fatal("budget key missing: absent must be explicit, not an empty object")
	} else if raw != nil {
		t.Fatalf("budget = %v, want null when the client has no budget", raw)
	}
}
