package asset_test

import (
	"fmt"

	"github.com/Wayfare-labs/wayfare/asset"
)

func ExampleLookup() {
	ngnc, ok := asset.Lookup("ngnc")
	if !ok {
		panic("NGNC is not in the registry")
	}
	peg, ok := asset.FiatPeg(ngnc)
	if !ok {
		panic("NGNC has no fiat peg")
	}
	fmt.Println(ngnc.Code, peg, len(asset.Registry()))
	// Output: NGNC NGN 9
}
