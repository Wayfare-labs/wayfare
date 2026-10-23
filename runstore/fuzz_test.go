package runstore

// Fuzzing the record decoder (issue #592).
//
// A committed chain is embedded into the deployed binary and served as
// measurement history, so the decoder is the boundary between bytes on disk and
// a figure a reader trusts. The hand-written corruption tests cover the shapes
// someone thought of; this covers the rest.
//
// The invariant is not "every input decodes". Refusing a malformed line is a
// correct outcome, and the version rules exist precisely so the decoder refuses
// what it cannot understand. The invariant is that what does decode can be
// served: a decoded record seals, verifies against the hash it computed, and
// survives encode-then-decode with its seal intact. A record that decoded into a
// partially populated struct but could not be re-read would be a measurement
// that exists only until the next process starts.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzDecodeRecord drives decodeLine with the committed chains as seeds, plus
// minimal Version 1 and Version 2 shapes so the fuzzer starts from every schema
// this build still loads.
func FuzzDecodeRecord(f *testing.F) {
	// Every committed line, so the seeds are the real thing rather than a
	// reconstruction of it. A missing data directory is not a reason to fail
	// here: the fuzz target still has the hand-written seeds below.
	if paths, err := filepath.Glob(filepath.Join("..", "data", "*.ndjson")); err == nil {
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(raw), "\n") {
				if line = strings.TrimSpace(line); line != "" {
					f.Add(line)
				}
			}
		}
	}

	// Version 1: the smallest record that predates checks, metrics and
	// snapshots.
	f.Add(`{"version":1,"seq":1,"recorded_at":"2026-08-22T12:09:59Z","corridor":"USDC-NGNC",` +
		`"integrity":"DIRECT","depends_on":[],` +
		`"reference":{"mid":"1349.669672","source":"exchangerate-api"},` +
		`"floor_loss_pct":"27.15","floor_size":"0.1","worst_loss_pct":"97.52","worst_size":"5000",` +
		`"recommended":null,"finding":"No usable size.","rungs":[],` +
		`"prev_hash":"sha256:0000000000000000000000000000000000000000000000000000000000000000",` +
		`"hash":"sha256:1111111111111111111111111111111111111111111111111111111111111111"}`)

	// Version 2: findings added, snapshot still absent.
	f.Add(`{"version":2,"seq":2,"recorded_at":"2026-08-22T13:09:59Z","corridor":"USDC-NGNC",` +
		`"integrity":"DIRECT","depends_on":[],` +
		`"reference":{"mid":"1349.669672","source":"exchangerate-api"},` +
		`"floor_loss_pct":"27.15","floor_size":"0.1","worst_loss_pct":"97.52","worst_size":"5000",` +
		`"recommended":null,"finding":"No usable size.","rungs":[],` +
		`"checks":[{"id":"sep10-endpoint","scope":"anchor","subject":"NGNC","severity":"notice",` +
		`"determined":true,"passed":true,"summary":"the endpoint answered","evidence":[],` +
		`"observed_at":"2026-08-22T13:09:59Z"}],` +
		`"metrics":[{"id":"spread.bid-ask","scope":"corridor","subject":"USDC -> NGNC",` +
		`"determined":true,"value":"128.8","unit":"percent","venue":"order-book",` +
		`"summary":"spread 128.8%","evidence":[],"observed_at":"2026-08-22T13:09:59Z"}],` +
		`"prev_hash":"sha256:1111111111111111111111111111111111111111111111111111111111111111",` +
		`"hash":"sha256:2222222222222222222222222222222222222222222222222222222222222222"}`)

	// Decode failures worth keeping in the corpus: a future schema and a
	// truncated line must stay refusals rather than becoming partial records.
	f.Add(`{"version":99,"seq":1}`)
	f.Add(`{"version":1,"seq":`)

	f.Fuzz(func(t *testing.T, line string) {
		rec, err := decodeLine("FUZZ", 1, []byte(line))
		if err != nil {
			// Refusing is a correct outcome; the assertions below are about
			// what a decoder accepted.
			return
		}

		// Decode-then-seal round-trips: an accepted record can always be
		// given a hash and verified against its own contents.
		if err := rec.Seal(); err != nil {
			t.Fatalf("a decoded record could not be sealed: %v", err)
		}
		if err := rec.VerifySelf(); err != nil {
			t.Fatalf("a decoded record does not verify after sealing: %v", err)
		}

		// And it survives the write path: encoding a sealed record and
		// decoding it again must produce a record that still verifies. This
		// is what a restart does, and a record that only decodes once cannot
		// be served twice.
		raw, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("a decoded record could not be encoded: %v", err)
		}
		again, err := decodeLine("FUZZ", 1, raw)
		if err != nil {
			t.Fatalf("a decoded record does not decode after encoding: %v", err)
		}
		if err := again.VerifySelf(); err != nil {
			t.Fatalf("a re-decoded record does not verify: %v", err)
		}
	})
}
