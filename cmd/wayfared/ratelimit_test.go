package main

import (
	"testing"

	"github.com/Wayfare-labs/wayfare/server"
)

// TestRateLimitDefaultsAreSane pins the choices made for issue #320: the
// defaults must bound a scripted client (the point of the feature) while
// leaving a real reader of the UI — page, selector load, a corridor or trend
// per interaction — comfortably inside the burst. If either constant moves,
// this is where the reasoning gets re-checked rather than silently drifted.
func TestRateLimitDefaultsAreSane(t *testing.T) {
	if defaultRateLimit <= 0 {
		t.Errorf("defaultRateLimit = %v, want positive; 0 disables limiting and the default must not", defaultRateLimit)
	}
	if defaultRateBurst < 1 {
		t.Errorf("defaultRateBurst = %d, want at least 1", defaultRateBurst)
	}
	// A normal visit is a handful of requests; the burst must absorb them
	// without a 429 ever reaching a human.
	const requestsInANormalVisit = 6
	if defaultRateBurst < requestsInANormalVisit {
		t.Errorf("defaultRateBurst = %d, want >= %d to absorb a normal page visit without a 429",
			defaultRateBurst, requestsInANormalVisit)
	}
	// And the disabled path must construct: wayfared builds its limiter
	// through the same constructor when an operator passes 0.
	rl := server.NewRateLimiter(0, 0)
	if ok, _ := rl.Allow("client"); !ok {
		t.Error("a disabled limiter (rate 0) must allow every request")
	}
}
