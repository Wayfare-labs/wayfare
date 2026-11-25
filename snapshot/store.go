package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"encoding/json"
	"fmt"
)

// StoreBody saves a response body into the content-addressed store by its SHA-256 hash.
func (s *Store) StoreBody(body []byte) (string, error) {
	hash := sha256.Sum256(body)
	hashStr := hex.EncodeToString(hash[:])

	if err := os.MkdirAll(filepath.Join(s.Dir, "blobs"), 0755); err != nil {
		return "", fmt.Errorf("snapshot: creating blobs dir: %w", err)
	}

	blobPath := filepath.Join(s.Dir, "blobs", hashStr)
	if _, err := os.Stat(blobPath); err == nil {
		return hashStr, nil // already exists
	}

	if err := os.WriteFile(blobPath, body, 0644); err != nil {
		return "", fmt.Errorf("snapshot: writing blob: %w", err)
	}

	return hashStr, nil
}

// GetBody retrieves a response body by its SHA-256 hash.
func (s *Store) GetBody(hashStr string) ([]byte, error) {
	blobPath := filepath.Join(s.Dir, "blobs", hashStr)
	data, err := os.ReadFile(blobPath)
	if err != nil {
		return nil, fmt.Errorf("snapshot: reading blob %s: %w", hashStr, err)
	}
	hash := sha256.Sum256(data)
	gotHash := hex.EncodeToString(hash[:])
	if gotHash != hashStr {
		return nil, fmt.Errorf("snapshot: blob hash mismatch for %s: got %s", hashStr, gotHash)
	}
	return data, nil
}

// MigrateSnapshot migrates a legacy snapshot directory to use content-addressed storage.
func MigrateSnapshot(snapDir string, store *Store) error {
	manifestPath := filepath.Join(snapDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("snapshot: reading manifest: %w", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw);
		err != nil {
		return fmt.Errorf(
			"snapshot: unmarshaling manifest for migration: %w",
			err,
		);
	}

	var interactions []map[string]interface{}
	if inters, ok := raw["interactions"].([]interface{}); ok {
		for _, inter := range inters {
			if m, ok := inter.(map[string]interface{}); ok {
				interactions = append(interactions, m)
			}
		}
	}

	for _, inter := range interactions {
		bodyFile, _ := inter["body_file"].(string)
		if bodyFile == "" {
			continue
		}

		fullBodyPath := filepath.Join(snapDir, bodyFile)
		bodyBytes, err := os.ReadFile(fullBodyPath)
		if err != nil {
			// If it's already using body_sha256 or missing, check alternative
			if sha, ok := inter["body_sha256"].(string); ok && sha != "" {
				continue
			}
			return fmt.Errorf("snapshot: reading body file %s: %w", fullBodyPath, err)
		}

		hashStr, err := store.StoreBody(bodyBytes)
		if err != nil {
			return err
		}

		inter["body_sha256"] = hashStr
		delete(inter, "body_file")
	}

	newManifestData, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("snapshot: marshaling migrated manifest: %w", err)
	}

	if err := os.WriteFile(manifestPath, newManifestData, 0644); err != nil {
		return fmt.Errorf(
			"snapshot: writing migrated manifest: %w",
			err,
		);
	}

	// Optionally remove responses directory if empty
	_ = os.RemoveAll(filepath.Join(snapDir, "responses"))
	return nil
}
// Store manages content-addressed storage of snapshot response bodies.
type Store struct {
	Dir string
}

// NewStore creates a new content-addressed store rooted at dir.
func NewStore(dir string) *Store {
	return &Store{Dir: dir}
}

// Put stores data by its SHA-256 hash in the content store under Dir/objects/<sha256>.
func (s *Store) Put(data []byte) (string, error) {
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])
	hashKey := "sha256:" + hashStr

	objDir := filepath.Join(s.Dir, "objects")
	if err := os.MkdirAll(objDir, 0755); err != nil {
		return "", err
	}

	objPath := filepath.Join(objDir, hashStr)
	if _, err := os.Stat(objPath); os.IsNotExist(err) {
		if err := os.WriteFile(objPath, data, 0644); err != nil {
			return "", err
		}
	}

	return hashKey, nil
}

// Get retrieves response body bytes given a SHA-256 hash or hashKey.
func (s *Store) Get(hashKey string) ([]byte, error) {
	hashStr := hashKey
	if len(hashStr) > 7 && hashStr[:7] == "sha256:" {
		hashStr = hashStr[7:]
	}

	objPath := filepath.Join(s.Dir, "objects", hashStr)
	return os.ReadFile(objPath)
}

// Reader returns an io.ReadCloser for the stored object.
func (s *Store) Reader(hashKey string) (io.ReadCloser, error) {
	hashStr := hashKey
	if len(hashStr) > 7 && hashStr[:7] == "sha256:" {
		hashStr = hashStr[7:]
	}

	objPath := filepath.Join(s.Dir, "objects", hashStr)
	return os.Open(objPath)
}
