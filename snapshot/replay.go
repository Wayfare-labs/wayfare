package snapshot

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrNotRecorded reports a request the snapshot has no answer for.
//
// It is deliberately not recoverable by falling through to the network. A
// replayer that reaches upstream on a miss turns a test that claims to be
// deterministic into one that is intermittently live — and it passes, so
// nobody finds out until a figure derived from it is wrong.
type ErrNotRecorded struct {
	Key      string
	Snapshot string
	Recorded []string
}

func (e *ErrNotRecorded) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "snapshot: %s has no recorded response for %q", e.Snapshot, e.Key)
	if len(e.Recorded) > 0 {
		fmt.Fprintf(&b, "; %d requests are recorded, including:", len(e.Recorded))
		for i, k := range e.Recorded {
			if i == 3 {
				fmt.Fprintf(&b, "\n\t... and %d more", len(e.Recorded)-i)
				break
			}
			fmt.Fprintf(&b, "\n\t%s", k)
		}
	}
	return b.String()
}

// Replayer serves recorded responses as an http.RoundTripper.
//
// It is the seam the whole package is built around: dex.Client and
// refrate.ExchangeRateAPI both take an *http.Client, so replacing the
// transport swaps every upstream call at once without either package growing
// a test-only interface.
type Replayer struct {
	m *Manifest
}

// Replay builds a Replayer over a loaded snapshot.
func (m *Manifest) Replay() *Replayer { return &Replayer{m: m} }

// HTTPClient is an *http.Client that answers only from this snapshot.
func (m *Manifest) HTTPClient() *http.Client {
	return &http.Client{Transport: m.Replay()}
}

// RoundTrip implements http.RoundTripper.
func (r *Replayer) RoundTrip(req *http.Request) (*http.Response, error) {
	key := Key(req.Method, req.URL)
	body, ok := r.m.bodies[key]
	if !ok {
		return nil, &ErrNotRecorded{
			Key:      key,
			Snapshot: r.m.Name(),
			Recorded: r.m.Keys(),
		}
	}

	status, contentType := http.StatusOK, "application/json"
	for _, in := range r.m.Interactions {
		if in.Key == key {
			status = in.Status
			if in.ContentType != "" {
				contentType = in.ContentType
			}
			break
		}
	}

	header := make(http.Header)
	header.Set("Content-Type", contentType)

	return &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode:    status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}, nil
}
