package runstore

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// ReadOnly is a Store backed by an fs.FS.
//
// It exists because the published history is evidence, and evidence should
// travel with the thing that publishes it. An embedded chain can be served
// from a read-only filesystem — a serverless function, a scratch container, a
// binary handed to a reviewer — without needing a writable volume to exist
// first.
//
// Append always fails. A read-only store that silently discarded writes would
// be worse than one that refuses them: the caller would believe a measurement
// had been recorded.
type ReadOnly struct {
	records map[string][]*Record
}

// OpenFS loads and verifies every chain under dir within fsys.
//
// Verification happens at load, exactly as it does for the file store: a
// broken chain must never be served, because a reader has no way to tell a
// tampered record from a sound one by looking at it.
func OpenFS(fsys fs.FS, dir string) (*ReadOnly, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("runstore: reading %s: %w", dir, err)
	}

	s := &ReadOnly{records: map[string][]*Record{}}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), FileExt) {
			continue
		}
		corridor := strings.ToUpper(strings.TrimSuffix(e.Name(), FileExt))

		records, err := readChain(fsys, dir+"/"+e.Name(), corridor)
		if err != nil {
			return nil, err
		}
		if err := verifyChain(corridor, records); err != nil {
			return nil, err
		}
		s.records[corridor] = records
	}
	return s, nil
}

func readChain(fsys fs.FS, path, corridor string) ([]*Record, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, fmt.Errorf("runstore: opening %s: %w", path, err)
	}
	defer f.Close()

	var out []*Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		var r Record
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, fmt.Errorf("runstore: %s line %d: %w", corridor, line, err)
		}
		// Versions 1, 2 and 3 are all loadable: each migration added its
		// fields with omitempty after every earlier field, so older records
		// encode byte-for-byte as they did when they were written and
		// verify unchanged (see Record and docs/run-store.md). Any other
		// version is a schema this build does not understand and must be
		// refused, never guessed at.
		if r.Version != 1 && r.Version != 2 && r.Version != Version {
			return nil, fmt.Errorf(
				"runstore: %s line %d has record version %d, this build understands %d",
				corridor, line, r.Version, Version)
		}
		out = append(out, &r)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("runstore: reading %s: %w", corridor, err)
	}
	return out, nil
}

// Append always fails: this store is read-only by construction.
func (s *ReadOnly) Append(context.Context, *Record) error {
	return fmt.Errorf("runstore: this store is read-only; measurements cannot be recorded here")
}

// Latest returns the newest record for a corridor, or nil.
func (s *ReadOnly) Latest(_ context.Context, corridor string) (*Record, error) {
	rs := s.records[strings.ToUpper(corridor)]
	if len(rs) == 0 {
		return nil, nil
	}
	return rs[len(rs)-1], nil
}

// Recent returns up to n records, newest first.
func (s *ReadOnly) Recent(_ context.Context, corridor string, n int) ([]*Record, error) {
	if n <= 0 {
		return nil, nil
	}
	rs := s.records[strings.ToUpper(corridor)]
	if len(rs) > n {
		rs = rs[len(rs)-n:]
	}
	out := make([]*Record, 0, len(rs))
	for i := len(rs) - 1; i >= 0; i-- {
		out = append(out, rs[i])
	}
	return out, nil
}

// All returns the complete corridor history in chronological order
// (oldest first).
func (s *ReadOnly) All(_ context.Context, corridor string) ([]*Record, error) {
	return s.records[strings.ToUpper(corridor)], nil
}

// Verify re-walks a corridor's chain.
func (s *ReadOnly) Verify(_ context.Context, corridor string) error {
	corridor = strings.ToUpper(corridor)
	return verifyChain(corridor, s.records[corridor])
}

// Corridors lists the corridors with stored history, sorted.
func (s *ReadOnly) Corridors(context.Context) ([]string, error) {
	out := make([]string, 0, len(s.records))
	for c := range s.records {
		out = append(out, c)
	}
	sort.Strings(out)
	return out, nil
}
