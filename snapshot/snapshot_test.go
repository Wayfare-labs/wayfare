package snapshot_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_, err = m.HTTPClient().Get("https://horizon.stellar.org/paths/strict-send?source_amount=999")
	if err == nil {
		t.Fatal("a request the snapshot does not contain returned no error; " +
			"a silent passthrough would make replayed tests intermittently live")
	}

	var notRecorded *ErrNotRecorded
	if !errors.As(err, &notRecorded) {
		t.Fatalf("error was %T (%v), want *ErrNotRecorded", err, err)
	}
	if !strings.Contains(notRecorded.Key, "source_amount=999") {
		t.Errorf("error does not name the missing request: %v", notRecorded)
	}
}

// TestLoadRefusesHashMismatch verifies the negative path of body-hash
// verification. Each sub-case corrupts a body file or the manifest hash in a
// different way so that Load must refuse the snapshot. The cases are named so
// that a specific mutation can be traced to a specific failure, and each one
// can actually fail independently: removing the bodyHash comparison from Load
// would break every case here.
func TestLoadRefusesHashMismatch(t *testing.T) {
	const original = `{"destination_amount":"62890.83"}`

	tests := []struct {
		name        string
		corrupt     func(dir string)
		wantContain string // substring the error must contain
	}{
		{
			name: "body content replaced",
			corrupt: func(dir string) {
				body := filepath.Join(dir, "responses", "001-paths-strict-send-100.json")
				if err := os.WriteFile(body, []byte(`{"destination_amount":"92890.83"}`), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantContain: "does not match",
		},
		{
			name: "single byte appended",
			corrupt: func(dir string) {
				body := filepath.Join(dir, "responses", "001-paths-strict-send-100.json")
				b, err := os.ReadFile(body)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(body, append(b, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantContain: "does not match",
		},
		{
			name: "body truncated to empty",
			corrupt: func(dir string) {
				body := filepath.Join(dir, "responses", "001-paths-strict-send-100.json")
				if err := os.WriteFile(body, nil, 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantContain: "does not match",
		},
		{
			name: "manifest hash replaced with a wrong literal",
			corrupt: func(dir string) {
				mPath := filepath.Join(dir, ManifestFile)
				raw, err := os.ReadFile(mPath)
				if err != nil {
					t.Fatal(err)
				}
				var m map[string]any
				if err := json.Unmarshal(raw, &m); err != nil {
					t.Fatal(err)
				}
				interactions := m["interactions"].([]any)
				first := interactions[0].(map[string]any)
				first["body_sha256"] = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
				edited, _ := json.Marshal(m)
				if err := os.WriteFile(mPath, edited, 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantContain: "does not match",
		},
		{
			name: "body replaced with unrelated JSON",
			corrupt: func(dir string) {
				body := filepath.Join(dir, "responses", "001-paths-strict-send-100.json")
				if err := os.WriteFile(body, []byte(`{"unrelated":true}`), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantContain: "does not match",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, _ := recordAgainst(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, original)
			}, "/paths/strict-send?source_amount=100")

			tc.corrupt(dir)

			_, err := Load(dir)
			if err == nil {
				t.Fatalf("%s: Load succeeded; the hash pin is not enforced", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantContain) {
				t.Errorf("%s: error %q does not contain %q", tc.name, err, tc.wantContain)
			}
		})
	}
}

func TestUnknownVersionIsRefused(t *testing.T) {
	dir, _ := recordAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}, "/paths/strict-send?source_amount=100")

	path := filepath.Join(dir, ManifestFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	generic["version"] = Version + 1
	edited, _ := json.Marshal(generic)
	if err := os.WriteFile(path, edited, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = Load(dir)
	if err == nil {
		t.Fatal("a future format version loaded; old snapshots would misparse silently")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("error should name the version mismatch, got: %v", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("version %d", Version+1)) {
		t.Errorf("error should name the specific version found (%d), got: %v", Version+1, err)
	}
}

// TestSnapshotVersion0IsRefused guards against treating a zero-value version
// as a default to be filled in. An absent or zero version is unknown.
func TestSnapshotVersion0IsRefused(t *testing.T) {
	dir, _ := recordAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}, "/paths/strict-send?source_amount=100")

	path := filepath.Join(dir, ManifestFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	generic["version"] = 0
	edited, _ := json.Marshal(generic)
	if err := os.WriteFile(path, edited, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = Load(dir)
	if err == nil {
		t.Fatal("a snapshot with version 0 loaded; zero is not a known version")
	}
	if !strings.Contains(err.Error(), "version 0") {
		t.Errorf("error should name version 0, got: %v", err)
	}
}

func TestRepeatedRequestsAreRecordedOnce(t *testing.T) {
	// A ladder prices a size and then re-prices it as a slippage probe, so
	// the same key genuinely recurs within one run.
	_, rec := recordAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	},
		"/paths/strict-send?source_amount=100",
		"/paths/strict-send?source_amount=100",
		"/paths/strict-send?source_amount=10",
	)

	if got := rec.Count(); got != 2 {
		t.Errorf("recorded %d interactions, want 2 (the duplicate should collapse)", got)
	}
}

func TestSaveRefusesToOverwriteASnapshot(t *testing.T) {
	dir, rec := recordAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}, "/paths/strict-send?source_amount=100")

	if err := rec.Save(dir); err == nil {
		t.Fatal("Save overwrote an existing snapshot; provenance of anything " +
			"derived from the original would be destroyed")
	}
}

func TestManifestCarriesNoMoney(t *testing.T) {
	dir, _ := recordAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"destination_amount":"62890.83"}`)
	}, "/paths/strict-send?source_amount=100")

	raw, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	// Every figure must come from reparsing the bodies. A measured amount
	// cached in the manifest is a second source of truth that can drift.
	if strings.Contains(string(raw), "62890.83") {
		t.Error("manifest contains a measured amount; provenance only belongs there")
	}
}

func TestSizesSurviveAsDecimalStrings(t *testing.T) {
	dir, _ := recordAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}, "/paths/strict-send?source_amount=0.1")

func TestSnapshotsCoverage(t *testing.T) {
	s, err := snapshot.LoadAll("../testdata/snapshots")
	if err != nil {
		t.Fatalf("failed to load snapshots: %v", err)
	}
	if len(s) == 0 {
		t.Fatal("expected at least one snapshot fixture")
	}
}
