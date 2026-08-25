// Package snapshot records and replays the upstream responses behind one
// ladder run, so a measurement can be re-read rather than re-taken.
//
// # Why raw bytes and not parsed structs
//
// The project's invariant is not "always hit the network" — it is "never show
// a number whose origin you cannot name". A live call satisfies the first and
// fails the second, because it keeps no record of what it saw. A snapshot
// carries the exact bytes an endpoint returned and the instant it returned
// them, which names the origin better than the live call it replaces.
//
// That is also why what is stored is the verbatim response body rather than a
// decoded struct. A fixture derived from dex.wirePathRecord can only confirm
// that the parser agrees with itself; it will match on every field name the
// parser already gets right, which is precisely the class of bug it cannot
// catch. The bugs that matter are the ones where reality does not match the
// mental model — an amount arriving with more precision than expected,
// "native" where a code and issuer were assumed, an empty _embedded.records
// meaning no market rather than an error. Only the real bytes carry those.
//
// # The contract
//
// docs/snapshot-format.md is the specification this package implements. The
// two rules most easily broken by a well-meaning change:
//
//   - An unknown Version is refused loudly. A snapshot that misparses silently
//     would republish a wrong figure under the project's own name.
//   - An unrecorded request is an error, never a fall-through to the network.
//     A replayer that reaches upstream on a miss makes a "deterministic" test
//     intermittently live, which is worse than a flaky one because it passes.
package snapshot

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Format identifies the file format, so a directory of JSON that happens to
// have a manifest.json cannot be mistaken for a snapshot.
const Format = "wayfare.snapshot"

// Version is the format version this package reads and writes.
//
// It is an integer, and a loader refuses any value it does not know rather
// than parsing on a best-effort basis. The format will change; old snapshots
// must fail loudly instead of misparsing.
const Version = 1

// ManifestFile is the manifest's name within a snapshot directory.
const ManifestFile = "manifest.json"

// responsesDir holds the verbatim upstream bodies.
const responsesDir = "responses"

// Interaction kinds. These classify a recorded request by which upstream it
// went to, so a consumer can find the reference-rate body without parsing URLs.
const (
	KindHorizon   = "horizon"
	KindReference = "reference"
)

// AssetRef identifies an asset within a manifest.
//
// Code alone is never sufficient — anyone can issue a token called "USDC" —
// so the issuer travels with it, exactly as it does in package asset.
type AssetRef struct {
	Code   string `json:"code"`
	Issuer string `json:"issuer,omitempty"`
}

// Corridor names what was measured.
type Corridor struct {
	Send          AssetRef `json:"send"`
	Receive       AssetRef `json:"receive"`
	ReferencePair string   `json:"reference_pair"`
}

// Slug renders the corridor for a directory name, e.g. "usdc-ngnc".
func (c Corridor) Slug() string {
	return strings.ToLower(c.Send.Code + "-" + c.Receive.Code)
}

// SourceRef records where a class of response came from.
type SourceRef struct {
	Provider string `json:"provider,omitempty"`
	BaseURL  string `json:"base_url"`
}

// Sources records the upstreams behind a run.
type Sources struct {
	Horizon   SourceRef `json:"horizon"`
	Reference SourceRef `json:"reference"`
}

// Interaction is one recorded request/response pair.
//
// Body is not here: it lives in its own file, byte for byte as it arrived.
// BodySHA256 pins those bytes so an edited fixture fails to load rather than
// quietly changing a published figure.
type Interaction struct {
	Kind        string    `json:"kind"`
	Method      string    `json:"method"`
	Key         string    `json:"key"`
	URL         string    `json:"url"`
	Status      int       `json:"status"`
	ContentType string    `json:"content_type,omitempty"`
	RecordedAt  time.Time `json:"recorded_at"`
	BodyFile    string    `json:"body_file"`
	BodySHA256  string    `json:"body_sha256"`
}

// Manifest is a snapshot's self-description: one ladder run, replayable with
// no network.
//
// No money appears here. Sizes are request inputs rather than measurements,
// and every figure a consumer reports must come from reparsing the recorded
// bodies — that is the whole point of keeping them.
type Manifest struct {
	Format      string    `json:"format"`
	Version     int       `json:"version"`
	RecordedAt  time.Time `json:"recorded_at"`
	GitRevision string    `json:"git_revision,omitempty"`

	// Dirty is true when the recording tree had uncommitted changes, so
	// GitRevision is approximate. Absent means the tree was clean; a
	// consumer treating a dirty snapshot as authoritative provenance is
	// reading a field that says otherwise.
	Dirty        bool          `json:"dirty,omitempty"`
	Corridor     Corridor      `json:"corridor"`
	Sizes        []string      `json:"sizes"`
	Sources      Sources       `json:"sources"`
	Notes        []string      `json:"notes,omitempty"`
	Interactions []Interaction `json:"interactions"`

	// dir and bodies are populated by Load and are not serialised.
	dir    string
	bodies map[string][]byte
}

// Name is the snapshot's directory name, which is its identity in output that
// must disclose a replay.
func (m *Manifest) Name() string { return filepath.Base(m.dir) }

// Dir is where the snapshot was loaded from.
func (m *Manifest) Dir() string { return m.dir }

// Key canonicalises a request into the form a snapshot is indexed by.
//
// The host is deliberately excluded. A snapshot recorded against
// horizon.stellar.org must replay unchanged against an httptest.Server, so
// the key covers method, path and a sorted, percent-encoded query and nothing
// else.
func Key(method string, u *url.URL) string {
	method = strings.ToUpper(method)
	if method == "" {
		method = http.MethodGet
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if q := u.Query().Encode(); q != "" {
		return method + " " + path + "?" + q
	}
	return method + " " + path
}

// bodyHash renders the pinning hash for a body.
func bodyHash(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Load reads a snapshot directory, verifying every recorded body against its
// hash.
//
// A version this package does not know, a missing body, or a body whose
// contents no longer match its hash are all hard errors. None of them are
// recoverable in a way that preserves the guarantee the snapshot exists to
// provide.
func Load(dir string) (*Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, fmt.Errorf("snapshot: reading manifest: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("snapshot: parsing manifest in %s: %w", dir, err)
	}
	if m.Format != Format {
		return nil, fmt.Errorf("snapshot: %s is not a %s directory (format %q)",
			dir, Format, m.Format)
	}
	if m.Version != Version {
		return nil, fmt.Errorf(
			"snapshot: %s is format version %d, but this build reads version %d; "+
				"refusing to parse it rather than risk misreading a recorded measurement",
			dir, m.Version, Version)
	}

	m.dir = dir
	m.bodies = make(map[string][]byte, len(m.Interactions))
	for _, in := range m.Interactions {
		body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(in.BodyFile)))
		if err != nil {
			return nil, fmt.Errorf("snapshot: reading body for %q: %w", in.Key, err)
		}
		// Normalize CRLF to LF so the hash matches snapshots captured on
		// Linux, where the Recorder originally wrote them.
		body = bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
		if got := bodyHash(body); got != in.BodySHA256 {
			return nil, fmt.Errorf(
				"snapshot: body %s does not match its recorded hash (manifest %s, file %s); "+
					"the fixture has been edited since it was captured",
				in.BodyFile, in.BodySHA256, got)
		}
		m.bodies[in.Key] = body
	}
	return &m, nil
}

// Body returns the verbatim recorded body for a key.
func (m *Manifest) Body(key string) ([]byte, bool) {
	b, ok := m.bodies[key]
	return b, ok
}

// Keys lists the recorded request keys, sorted. Useful in an error message
// when a replay misses.
func (m *Manifest) Keys() []string {
	out := make([]string, 0, len(m.bodies))
	for k := range m.bodies {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// FirstOfKind returns the first recorded interaction of a kind, which is how a
// caller finds the reference-rate response without reconstructing its URL.
func (m *Manifest) FirstOfKind(kind string) (Interaction, bool) {
	for _, in := range m.Interactions {
		if in.Kind == kind {
			return in, true
		}
	}
	return Interaction{}, false
}
