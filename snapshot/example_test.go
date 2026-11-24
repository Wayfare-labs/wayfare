package snapshot_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	"github.com/Wayfare-labs/wayfare/snapshot"
)

func ExampleManifest_HTTPClient() {
	manifest, err := snapshot.Load(filepath.Join("..", "testdata", "snapshots", "usdc-ngnc-20260821T223040Z"))
	if err != nil {
		panic(err)
	}
	interaction, ok := manifest.FirstOfKind(snapshot.KindHorizon)
	if !ok {
		panic("snapshot has no Horizon interaction")
	}
	request, err := http.NewRequest(http.MethodGet, interaction.URL, nil)
	if err != nil {
		panic(err)
	}
	response, err := manifest.HTTPClient().Do(request)
	if err != nil {
		panic(err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}
	response.Body.Close()
	fmt.Println(manifest.Name(), response.StatusCode, len(body))
	_, err = manifest.HTTPClient().Get("https://horizon.stellar.org/not-recorded")
	var missing *snapshot.ErrNotRecorded
	fmt.Println(errors.As(err, &missing))
	// Output: usdc-ngnc-20260821T223040Z 200 1099
	// true
}
