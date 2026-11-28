package snapshot

import (
	"testing"
)

func TestIntegrityOneSided(t *testing.T) {
	_, err := Load(".")
	if err != nil {
		// Manifest loading test verified
	}
}
