package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

// Store provides content-addressed storage for snapshot response bodies by SHA-256.
type Store struct {
	RootDir string
}

// NewStore initializes a content store rooted at the given directory.
func NewStore(rootDir string) *Store {
	return &Store{RootDir: rootDir}
}

// Put stores data by its SHA-256 hash in the content store under rootDir/objects/xx/yy/... or flat/hierarchical structure.
// Following project requirements, we store content in rootDir/objects/<sha256> or similar.
func (s *Store) Put(data []byte) (string, error) {
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])
	hashKey := "sha256:" + hashStr

	objDir := filepath.Join(s.RootDir, "objects")
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

	objPath := filepath.Join(s.RootDir, "objects", hashStr)
	return os.ReadFile(objPath)
}

// Reader returns an io.ReadCloser for the stored object.
func (s *Store) Reader(hashKey string) (io.ReadCloser, error) {
	hashStr := hashKey
	if len(hashStr) > 7 && hashStr[:7] == "sha256:" {
		hashStr = hashStr[7:]
	}

	objPath := filepath.Join(s.RootDir, "objects", hashStr)
	return os.Open(objPath)
}
