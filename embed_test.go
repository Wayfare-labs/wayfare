package wayfare

import (
	"context"
	"testing"

	"github.com/Wayfare-labs/wayfare/runstore"
)

func TestEmbeddedHistoryVerifies(t *testing.T) {
	s, err := runstore.OpenFS(History, "data")
	if err != nil {
		t.Fatalf("embedded history does not load: %v", err)
	}
	cs, _ := s.Corridors(context.Background())
	if len(cs) == 0 {
		t.Fatal("embedded history is empty")
	}
	for _, c := range cs {
		if err := s.Verify(context.Background(), c); err != nil {
			t.Errorf("%s: %v", c, err)
		}
		r, _ := s.Latest(context.Background(), c)
		t.Logf("%-10s seq=%d integrity=%s", c, r.Seq, r.Integrity)
	}
}
