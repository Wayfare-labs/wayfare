package runstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestAppendAfterCancelWritesNothing pins the shutdown guarantee: a record is
// either written whole or not at all, and a cancelled context is refused
// before any byte reaches the chain.
func TestAppendAfterCancelWritesNothing(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Append(ctx, &Record{Corridor: "USDC-NGNC"}); err == nil {
		t.Fatal("Append with a cancelled context succeeded")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if fi, _ := os.Stat(filepath.Join(dir, e.Name())); fi != nil && fi.Size() > 0 {
			t.Errorf("%s has %d bytes after a refused append", e.Name(), fi.Size())
		}
	}
	if err := s.Verify(context.Background(), "USDC-NGNC"); err != nil {
		t.Errorf("chain does not verify after refused append: %v", err)
	}
}
