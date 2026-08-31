package runstore

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileStore keeps one NDJSON file per corridor, opened for append.
//
// No database, deliberately. The access pattern is "append one record every
// few hours, read the last one or two" — a file per corridor serves that
// exactly, is inspectable with the tools anyone already has, and makes the
// hash chain something a reader can verify with sha256sum and a text editor
// rather than a client library.
//
// One file per corridor rather than one shared file because the chains are
// independent: a corridor's history should not be interleaved with another's,
// and Verify walks one chain at a time.
type FileStore struct {
	dir string

	mu sync.Mutex
	// tips indexes the last record of each corridor, so Latest is a map
	// read rather than a file scan. Built once at Open and maintained on
	// Append; #24 compares consecutive runs on every measurement, so this
	// is the read path that has to stay cheap.
	tips map[string]*Record
}

// FileExt is the extension of a corridor chain file.
const FileExt = ".ndjson"

// Open loads a store rooted at dir, creating it if absent.
//
// Every existing chain is verified during Open. A store that silently loaded a
// broken chain and then appended to it would bury the break under valid
// records, so the failure surfaces at startup instead.
func Open(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("runstore: creating %s: %w", dir, err)
	}
	s := &FileStore{dir: dir, tips: map[string]*Record{}}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("runstore: reading %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), FileExt) {
			continue
		}
		corridor := strings.ToUpper(strings.TrimSuffix(e.Name(), FileExt))
		records, err := s.readAll(corridor)
		if err != nil {
			return nil, err
		}
		if err := verifyChain(corridor, records); err != nil {
			return nil, err
		}
		if len(records) > 0 {
			s.tips[corridor] = records[len(records)-1]
		}
	}
	return s, nil
}

func (s *FileStore) path(corridor string) string {
	return filepath.Join(s.dir, strings.ToUpper(corridor)+FileExt)
}

// readAll parses a corridor's whole chain in order.
func (s *FileStore) readAll(corridor string) ([]*Record, error) {
	f, err := os.Open(s.path(corridor))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("runstore: opening %s: %w", corridor, err)
	}
	defer f.Close()

	var out []*Record
	sc := bufio.NewScanner(f)
	// Records carry a full ladder; the default 64KB token limit is not
	// enough for a wide one.
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
				"runstore: %s line %d has record version %d, this build understands %d; "+
					"refusing to guess at a schema it does not know",
				corridor, line, r.Version, Version)
		}
		out = append(out, &r)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("runstore: reading %s: %w", corridor, err)
	}
	return out, nil
}

// verifyChain checks every hash and every link in order.
func verifyChain(corridor string, records []*Record) error {
	prev := GenesisPrevHash
	for i, r := range records {
		if err := r.VerifySelf(); err != nil {
			return fmt.Errorf("runstore: %s: %w", corridor, err)
		}
		if r.PrevHash != prev {
			return fmt.Errorf(
				"runstore: %s: record seq %d expects prev_hash %s but the previous record hashes to %s; "+
					"the chain is broken at position %d",
				corridor, r.Seq, short(r.PrevHash), short(prev), i)
		}
		prev = r.Hash
	}
	return nil
}

// Append seals r against the current tip and writes it.
func (s *FileStore) Append(ctx context.Context, r *Record) error {
	if r == nil {
		return fmt.Errorf("runstore: refusing to append a nil record")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	corridor := strings.ToUpper(r.Corridor)
	if corridor == "" {
		return fmt.Errorf("runstore: record has no corridor")
	}

	r.Version = Version
	if r.RecordedAt.IsZero() {
		r.RecordedAt = time.Now().UTC()
	}
	r.RecordedAt = r.RecordedAt.UTC().Truncate(time.Second)

	// Nil slices and empty slices encode differently ([] vs null) and would
	// therefore hash differently. Normalising here means a record's hash
	// does not depend on how its caller happened to build it.
	if r.DependsOn == nil {
		r.DependsOn = []string{}
	}
	if r.Rungs == nil {
		r.Rungs = []Rung{}
	}

	tip := s.tips[corridor]
	if tip == nil {
		r.PrevHash = GenesisPrevHash
		r.Seq = 1
	} else {
		r.PrevHash = tip.Hash
		r.Seq = tip.Seq + 1
	}
	if err := r.Seal(); err != nil {
		return err
	}

	line, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("runstore: encoding record: %w", err)
	}

	f, err := os.OpenFile(s.path(corridor), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("runstore: opening %s for append: %w", corridor, err)
	}
	defer f.Close()

	// The chain file's last line must be newline-terminated before we
	// append. A kill between the record bytes and their newline — or a
	// tool that omits final newlines — leaves a complete, verifiable
	// record whose line simply lacks its terminator; appending straight
	// onto it would fuse the new record into the previous line and
	// corrupt the chain. The separator newline sits outside every
	// record's preimage, so writing it changes no hash and the chain
	// stays verifiable.
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("runstore: stat %s: %w", corridor, err)
	}
	if fi.Size() > 0 {
		var last [1]byte
		if _, err := f.ReadAt(last[:], fi.Size()-1); err != nil {
			return fmt.Errorf("runstore: reading the tail of %s: %w", corridor, err)
		}
		if last[0] != '\n' {
			if _, err := f.Write([]byte{'\n'}); err != nil {
				return fmt.Errorf("runstore: terminating the final line of %s: %w", corridor, err)
			}
		}
	}

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("runstore: appending to %s: %w", corridor, err)
	}
	// Durability matters more than throughput at one write per few hours:
	// a record acknowledged but lost in the page cache would leave the
	// in-memory tip ahead of the file, and the next Open would report a
	// broken chain for a record that was never really there.
	if err := f.Sync(); err != nil {
		return fmt.Errorf("runstore: syncing %s: %w", corridor, err)
	}

	s.tips[corridor] = r
	return nil
}

// Latest returns the newest record for a corridor, or nil if there is none.
func (s *FileStore) Latest(ctx context.Context, corridor string) (*Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tips[strings.ToUpper(corridor)], nil
}

// Recent returns up to n records, newest first.
func (s *FileStore) Recent(ctx context.Context, corridor string, n int) ([]*Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if n <= 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.readAll(strings.ToUpper(corridor))
	if err != nil {
		return nil, err
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	out := make([]*Record, 0, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		out = append(out, all[i])
	}
	return out, nil
}

// All returns the complete corridor history in chronological order
// (oldest first).
func (s *FileStore) All(ctx context.Context, corridor string) ([]*Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.readAll(strings.ToUpper(corridor))
	if err != nil {
		return nil, err
	}
	return all, nil
}

// Verify walks a corridor's whole chain.
func (s *FileStore) Verify(ctx context.Context, corridor string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	corridor = strings.ToUpper(corridor)
	records, err := s.readAll(corridor)
	if err != nil {
		return err
	}
	return verifyChain(corridor, records)
}

// Corridors lists the corridors with stored history, sorted.
func (s *FileStore) Corridors(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]string, 0, len(s.tips))
	for c := range s.tips {
		out = append(out, c)
	}
	sort.Strings(out)
	return out, nil
}

// VerifyAll walks every corridor's chain, reporting each failure.
func (s *FileStore) VerifyAll(ctx context.Context) error {
	corridors, err := s.Corridors(ctx)
	if err != nil {
		return err
	}
	var problems []string
	for _, c := range corridors {
		if err := s.Verify(ctx, c); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("runstore: %d corridor chain(s) failed verification:\n%s",
			len(problems), strings.Join(problems, "\n"))
	}
	return nil
}
