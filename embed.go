// Package wayfare embeds the published measurement history.
//
// The chain is evidence, and evidence should travel with whatever publishes
// it. Embedding means a deployment can serve and verify its own history from a
// read-only filesystem, with no volume to mount and nothing to fetch — and a
// binary handed to a reviewer carries the records it is making claims about.
//
// The history is written by cmd/wayfared and committed by the measure
// workflow; nothing reads this variable to produce a measurement.
//
// What this embedding means in production — the deployed instance serves the
// committed history, so freshness depends on redeploys rather than on the
// scheduler — is documented in docs/embedded-history.md.
package wayfare

import "embed"

// History holds the hash-chained run store as committed, rooted at "data".
//
//go:embed all:data
var History embed.FS
