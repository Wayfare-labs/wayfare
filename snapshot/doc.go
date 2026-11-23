// Package snapshot records and replays the upstream responses behind one
// ladder run, so a measurement can be re-read rather than re-taken.
//
// A replayer serves only recorded responses. An unrecorded request is an error,
// never a fall-through to the network.
package snapshot
