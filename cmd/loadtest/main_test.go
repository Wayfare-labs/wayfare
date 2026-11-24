package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunLoad(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	res := runLoad(srv.URL, 4, 20)
	if res.TotalRequests != 20 {
		t.Errorf("TotalRequests = %d, want 20", res.TotalRequests)
	}
	if res.Successful != 20 {
		t.Errorf("Successful = %d, want 20", res.Successful)
	}
	if res.Failed != 0 {
		t.Errorf("Failed = %d, want 0", res.Failed)
	}
	if res.RPS <= 0 {
		t.Errorf("RPS = %f, want > 0", res.RPS)
	}
}
