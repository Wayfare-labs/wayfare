package runstore

import (
	"context"
	"strings"
	"time"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/route"
)

// CorridorSnapshot is one corridor's latest state in the static snapshot.
type CorridorSnapshot struct {
	Corridor     string            `json:"corridor"`
	SendAsset    route.AssetJSON   `json:"send_asset"`
	ReceiveAsset route.AssetJSON   `json:"receive_asset"`
	Integrity    string            `json:"integrity"`
	FloorLossPct string            `json:"floor_loss_pct"`
	FloorSize    string            `json:"floor_size"`
	WorstLossPct string            `json:"worst_loss_pct"`
	WorstSize    string            `json:"worst_size"`
	RecordedAt   string            `json:"recorded_at"`
	ChainHead    ChainHeadSnapshot `json:"chain_head"`
}

// ChainHeadSnapshot is the externally pinnable identity of a corridor's chain tip.
type ChainHeadSnapshot struct {
	Seq        int64  `json:"seq"`
	RecordedAt string `json:"recorded_at"`
	Hash       string `json:"hash"`
}

// StaticSnapshot is the top-level snapshot document.
type StaticSnapshot struct {
	GeneratedAt string             `json:"generated_at"`
	Corridors   []CorridorSnapshot `json:"corridors"`
}

// BuildStaticSnapshot builds the static snapshot from the store.
func BuildStaticSnapshot(store Store, ctx context.Context, now time.Time) (StaticSnapshot, error) {
	corridors, err := store.Corridors(ctx)
	if err != nil {
		return StaticSnapshot{}, err
	}

	snap := StaticSnapshot{
		GeneratedAt: now.UTC().Format(time.RFC3339),
		Corridors:   make([]CorridorSnapshot, 0, len(corridors)),
	}

	for _, corridor := range corridors {
		rec, err := store.Latest(ctx, corridor)
		if err != nil || rec == nil {
			continue
		}

		sendCode, recvCode := splitCorridorKey(corridor)
		sendAsset, _ := asset.Lookup(sendCode)
		recvAsset, _ := asset.Lookup(recvCode)

		snap.Corridors = append(snap.Corridors, CorridorSnapshot{
			Corridor:     corridor,
			SendAsset:    route.ToAssetJSON(sendAsset),
			ReceiveAsset: route.ToAssetJSON(recvAsset),
			Integrity:    rec.Integrity,
			FloorLossPct: rec.FloorLossPct,
			FloorSize:    rec.FloorSize,
			WorstLossPct: rec.WorstLossPct,
			WorstSize:    rec.WorstSize,
			RecordedAt:   rec.RecordedAt.UTC().Format(time.RFC3339),
			ChainHead: ChainHeadSnapshot{
				Seq:        rec.Seq,
				RecordedAt: rec.RecordedAt.UTC().Format(time.RFC3339),
				Hash:       rec.Hash,
			},
		})
	}

	return snap, nil
}

// splitCorridorKey splits a corridor key like "USDC-NGNC" into send and receive codes.
func splitCorridorKey(corridor string) (string, string) {
	parts := strings.SplitN(corridor, "-", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
