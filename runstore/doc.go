// Package runstore keeps a tamper-evident history of corridor measurements.
//
// Each record carries the hash of the one before it, so a stored history is a
// chain rather than a pile. Verify walks the chain and names the first record
// that does not reconcile.
package runstore
