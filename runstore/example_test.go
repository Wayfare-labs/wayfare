package runstore_test

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Wayfare-labs/wayfare/runstore"
)

func ExampleFileStore() {
	dir, err := os.MkdirTemp("", "wayfare-runstore-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	store, err := runstore.Open(dir)
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	for i, at := range []time.Time{
		time.Date(2026, time.August, 21, 22, 30, 0, 0, time.UTC),
		time.Date(2026, time.August, 21, 22, 36, 0, 0, time.UTC),
	} {
		err = store.Append(ctx, &runstore.Record{
			Corridor: "USDC-NGNC", RecordedAt: at, Integrity: "DIRECT",
			Rungs: []runstore.Rung{{SendAmount: "1", Priced: true, Integrity: "DIRECT", ReceiveAmount: "100", EffectiveRate: "100", LossPct: "1", Verdict: "GOOD", Path: "USDC -> NGNC"}},
		})
		if err != nil {
			panic(err)
		}
		if i == 1 {
			if err := store.Verify(ctx, "USDC-NGNC"); err != nil {
				panic(err)
			}
		}
	}
	latest, err := store.Latest(ctx, "USDC-NGNC")
	if err != nil {
		panic(err)
	}
	fmt.Println(latest.Seq, latest.Corridor, latest.RecordedAt.Format(time.RFC3339))
	// Output: 2 USDC-NGNC 2026-08-21T22:36:00Z
}
