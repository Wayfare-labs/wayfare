// Default per-client request bounds for wayfared (issue #320).
//
// The numbers are a judgement, stated here so a reviewer can argue with the
// judgement rather than hunt for magic numbers. A real reader of the UI makes
// a handful of requests per visit: the page itself, /api/assets to fill the
// selector, and then /api/corridor or /api/corridor/trend per interaction.
// Four per second with a burst of ten clears that with room to spare, while a
// scripted client holding ?live=1 down in a loop — the case this defends
// against, since each of those requests prices a ladder against Horizon — is
// slowed to four ladders a second per client instead of unbounded.
package main

const (
	// defaultRateLimit is the sustained per-client request rate.
	defaultRateLimit = 4.0
	// defaultRateBurst is the per-client instantaneous allowance.
	defaultRateBurst = 10
)
