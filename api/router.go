package api

import (
	"encoding/json"
	"net/http"
)

// Router handles API requests for corridors supporting multiple send assets (e.g. XLM, USDC, EURC).
type Router struct {
	SupportedSendAssets []string
}

func NewRouter() *Router {
	return &Router{
		SupportedSendAssets: []string{"USDC", "XLM", "EURC"},
	}
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"send_assets": r.SupportedSendAssets,
	})
}
