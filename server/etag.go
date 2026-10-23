package server

// Conditional requests on history-served corridor responses (issue #613).
//
// A stored record is immutable. Its contents are hash-sealed, and a new
// record is a new link in the chain rather than an edit of an old one: the
// run store never rewrites a past run. A client polling a history-first
// instance can therefore be told exactly which record it is holding, and
// asked whether that is still the one the store has. A matching answer costs
// a 304 and no body.
//
// The validator is the record's own seal hash — the same value the chain
// verifies against — so an ETag here is not a digest invented for HTTP. It
// is the identity of the bytes on disk, and it is the value a reader can
// verify with sha256sum. Re-sealing a record, or replacing it with the next
// one in the chain, changes the tag with it.
//
// A live measurement gets no ETag. Its body is a fresh reading whose figures
// depend on the market at the moment of the request, and a validator that
// claimed otherwise would let a client keep a body that never existed as
// the current one. Absence of a tag is therefore the honest signal that a
// response must not be reused, and it is the same signal a live response
// already sends about its own freshness.
//
// One honest wrinkle: the served document carries a stale envelope whose
// age is recomputed on every request, so two responses for the same record
// differ in `stale.age_seconds` and `stale.age_human` and are not
// byte-identical. The measurement they describe is identical. The ETag
// identifies the record — the measurement — and a 304 leaves the client's
// copy, with the age it was served at, exactly as it was.

import (
	"net/http"
	"strings"

	"github.com/Wayfare-labs/wayfare/runstore"
)

// recordETag returns the entity tag for a stored record: its seal hash,
// quoted as RFC 9110 requires an entity-tag to be.
//
// A record with no hash yields no tag. A record that was never sealed cannot
// be identified by its seal, and a tag derived from anything else would be a
// claim the store cannot back.
func recordETag(rec *runstore.Record) string {
	if rec == nil || rec.Hash == "" {
		return ""
	}
	return `"` + rec.Hash + `"`
}

// ifNoneMatch reports whether the request's If-None-Match header matches the
// given entity tag.
//
// RFC 9110 requires the comparison to be weak: a client may send W/"..." for
// a tag it received, and the weak prefix does not change the identity of the
// representation it names. The header is a comma-separated list, and "*"
// matches any current representation — which for a history-served response
// is the record the store holds.
//
// An empty tag never matches. A response with no validator has no
// representation to be not-modified from, and answering 304 for it would
// tell the client to keep a body it may never have received.
func ifNoneMatch(r *http.Request, etag string) bool {
	if etag == "" {
		return false
	}
	header := r.Header.Get("If-None-Match")
	if header == "" {
		return false
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if candidate == "*" {
			return true
		}
		if strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

// writeHistoryJSON serves a history-served document with the stored
// record's entity tag, and answers a conditional request with 304 when the
// client's copy is the one the store still holds.
//
// The 304 carries the same ETag and no body, so a polling client keeps what
// it has. It is only ever produced from a record: a live measurement has no
// validator, is never answered conditionally, and is written with writeJSON
// like any other live body.
//
// Cache-Control: no-cache says the body may be stored but must be
// revalidated before it is reused. Without it the freshness of a response
// with no explicit lifetime is left to the client's heuristic, and some of
// them will reuse the stored body without ever asking — which is exactly the
// download the validator exists to avoid. With it, the only path to a reused
// body is a conditional request that comes back 304.
func writeHistoryJSON(w http.ResponseWriter, r *http.Request, rec *runstore.Record, body any) {
	// The body is a stored reading, and a stored reading is only as fresh as
	// the record it came from: it may be revalidated before it is reused.
	w.Header().Set("Cache-Control", "no-cache")

	etag := recordETag(rec)
	if etag == "" {
		// No seal, no validator: serve the document in full rather than
		// pretend there is something to revalidate against.
		writeJSON(w, r, http.StatusOK, body)
		return
	}

	w.Header().Set("ETag", etag)
	if ifNoneMatch(r, etag) {
		// A 304 carries no body and no Content-Type describing one. The
		// ETag stays, because it is the answer to the question the client
		// asked.
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, r, http.StatusOK, body)
}
