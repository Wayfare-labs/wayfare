package snapshot

import (
	"os"
	"path/filepath"
	"encoding/json"
	"fmt"
	"net/http"
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
func MigrateSnapshotDir(snapshotDir string, store *Store) error {
	manifestPath := filepath.Join(snapshotDir, "manifest.json")
	m, err := Load(snapshotDir)
	if err != nil {
		return fmt.Errorf("load manifest for migration: %w", err)
	}

	for i := range m.Interactions {
		inter := &m.Interactions[i]
		var bodyBytes []byte
		if inter.BodyFile != "" {
			bf := filepath.Join(snapshotDir, inter.BodyFile)
			var readErr error
			bodyBytes, readErr = os.ReadFile(bf)
			if readErr != nil {
				return fmt.Errorf("read body file %s: %w", bf, readErr)
			}
		} else if inter.BodySHA256 != "" {
			continue
		}

	hashStr, err := store.Put(bodyBytes)
		if err != nil {
			return fmt.Errorf("store put during migration: %w", err)
		}
		inter.BodySHA256 = hashStr
		inter.BodyFile = ""
	}

	if err := m.Save(manifestPath); err != nil {
		return fmt.Errorf("save migrated manifest: %w", err)
	}

	_ = os.RemoveAll(filepath.Join(snapshotDir, "responses"))
	return nil
}
