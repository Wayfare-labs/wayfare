package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)


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
func TestContentAddressedStoreAndMigration(t *testing.T) {
	tmpDir := t.TempDir()

	storeDir := filepath.Join(tmpDir, "shared-store")
	store := NewStore(storeDir)

	body1 := []byte(`{"status":"ok"}`)
	body2 := []byte(`{"status":"ok"}`)
	body3 := []byte(`{"status":"different"}`)

	hash1, err := store.StoreBody(body1)
	if err != nil {
		t.Fatalf("StoreBody 1: %v", err)
	}
	hash2, err := store.StoreBody(body2)
	if err != nil {
		t.Fatalf("StoreBody 2: %v", err)
	}
	hash3, err := store.StoreBody(body3)
if err != nil {
		t.Fatalf("StoreBody 3: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("expected identical hashes for identical bodies, got %s vs %s", hash1, hash2)
	}
	if hash1 == hash3 {
		t.Errorf("expected different hashes for different bodies, got %s", hash1)
	}

	retrieved, err := store.GetBody(hash1)
	if err != nil {
		t.Fatalf("GetBody: %v", err)
	}
	if string(retrieved) != string(body1) {
		t.Errorf("retrieved body = %s, want %s", string(retrieved), string(body1))
	}

	snapDir := filepath.Join(tmpDir, "snap")
	if err := os.MkdirAll(filepath.Join(snapDir, "responses"), 0755); err != nil {
		t.Fatal(err)
	}

	manifestJSON := `{
		"format_version": 2,
		"corridor": "usdc-ngnc",
		"interactions": [
			{
				"method": "GET",
				"url": "/test",
				"body_file": "responses/test.json"
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(snapDir, "manifest.json"), []byte(manifestJSON), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, "responses", "test.json"), body1, 0644); err != nil {
		t.Fatal(err)
	}

	if err := MigrateSnapshot(snapDir, store); err != nil {
		t.Fatalf("MigrateSnapshot: %v", err)
	}

	mNew, err := Load(snapDir)
	if err != nil {
		t.Fatalf("Load migrated snapshot: %v", err)
	}
	if len(mNew.Interactions) != 1 {
		t.Fatalf("expected 1 interaction, got %d", len(mNew.Interactions))
	}
}
