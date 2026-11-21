package snapshot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// CleanSnapshotDir cleans response headers in an existing snapshot directory's manifest.json
// while preserving the original bodies and their hashes.
func CleanSnapshotDir(dir string) error {
	manifestPath := filepath.Join(dir, ManifestFile)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("snapshot: reading manifest for migration: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("snapshot: parsing manifest for migration: %w", err)
	}

	modified := false
	for i := range m.Interactions {
		if m.Interactions[i].Header != nil {
			cleaned := CleanHeaders(httpHeaderFromMap(m.Interactions[i].Header))
			m.Interactions[i].Header = mapOfHeader(cleaned)
			modified = true
		}
	}

	if !modified {
		return nil
	}

	newRaw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("snapshot: marshaling cleaned manifest: %w", err)
	}
	newRaw = append(newRaw, '\n')

	return os.WriteFile(manifestPath, newRaw, 0644)
}

func httpHeaderFromMap(m map[string][]string) http.Header {
	h := make(http.Header)
	for k, v := range m {
		h[k] = v
	}
	return h
}

func mapOfHeader(h http.Header) map[string][]string {
	m := make(map[string][]string)
	for k, v := range h {
		m[k] = v
	}
	return m
}
