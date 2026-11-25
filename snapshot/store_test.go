package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContentAddressedStoreAndMigration(t *testing.T) {
	tmpDir := t.TempDir()
	storeDir := filepath.Join(tmpDir, "store")
	store := NewStore(storeDir)

	data := []byte(`{"status":"ok"}`)
	hashKey, err := store.Put(data)
	if err != nil {
		t.Fatalf("store Put failed: %v", err)
	}

	got, err := store.Get(hashKey)
	if err != nil {
		t.Fatalf("store Get failed: %v", err)
	}

	if string(got) != string(data) {
		t.Errorf("got %s, want %s", string(got), string(data))
	}
}

func TestReplayUnchangedWithStore(t *testing.T) {
	tmpDir := t.TempDir()
	snapDir := filepath.Join(tmpDir, "snapshot")
	respDir := filepath.Join(snapDir, "responses")
	if err := os.MkdirAll(respDir, 0755); err != nil {
		t.Fatal(err)
	}

	bodyContent := []byte("hello replay")
	bodyFile := "body1.bin"
	if err := os.WriteFile(filepath.Join(respDir, bodyFile), bodyContent, 0644); err != nil {
		t.Fatal(err)
	}

	manifestJSON := `{
		"interactions": [
			{
				"request": {
					"method": "GET",
					"url": "http://example.com/test"
				},
				"response": {
					"status_code": 200,
					"header": {},
					"body_file": "responses/body1.bin"
				}
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(snapDir, "manifest.json"), []byte(manifestJSON), 0644); err != nil {
		t.Fatal(err)
	}

	storeDir := filepath.Join(tmpDir, "store")
	store := NewStore(storeDir)

	if err := MigrateSnapshotDir(snapDir, store); err != nil {
		t.Fatalf("MigrateSnapshotDir failed: %v", err)
	}

	m, err := Load(snapDir)
	if err != nil {
		t.Fatalf("Load manifest failed: %v", err)
	}

	if len(m.Interactions) != 1 {
		t.Fatalf("expected 1 interaction, got %d", len(m.Interactions))
	}

	inter := m.Interactions[0]
	if inter.BodySHA256 == "" {
		t.Fatal("expected BodySHA256 to be set after migration")
	}

	reloadedBody, err := store.Get(inter.BodySHA256)
	if err != nil {
		t.Fatalf("store.Get failed for migrated body: %v", err)
	}

	if string(reloadedBody) != string(bodyContent) {
		t.Errorf("replayed body got %q, want %q", string(reloadedBody), string(bodyContent))
	}
}
