package route_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Wayfare-labs/wayfare/route"
)

func ExampleCorridorJSON() {
	body, err := os.ReadFile(filepath.Join("..", "cmd", "ladder", "testdata", "golden", "ladder-usdc-ngnc.json"))
	if err != nil {
		panic(err)
	}
	var corridor route.CorridorJSON
	if err := json.Unmarshal(body, &corridor); err != nil {
		panic(err)
	}
	recommendation := "none"
	if corridor.Recommended != nil {
		recommendation = corridor.RecommendedSize
	}
	fmt.Println(corridor.Live, corridor.Scored, recommendation)
	// Output: true true none
}
