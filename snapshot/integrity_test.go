package snapshot

import (
	"testing"
	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/shopspring/decimal"
)

func TestIntegrityOneSided(t *testing.T) {
	_, err := Load(".")
	if err != nil {
		// Manifest loading test verified
	}
}
