// Package route ranks the ways money can travel a corridor, and says plainly
// when none of them are worth taking.
//
// Every quote is scored against an independent reference rate and carries a
// verdict. A result whose best option is unusable says so instead of
// presenting a winner merely because it ranked first.
package route
