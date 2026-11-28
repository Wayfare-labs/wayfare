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
		store_fatal(t, err)
	}

	bodyContent := []byte("hello replay")
	bodyFile := "body1.bin"
	if err := os.WriteFile(filepath.Join(respDir, bodyFile), bodyContent, 0644); err != nil {
		store_fatal(t, err)
	}

	manifestJSON := `{
		"format": "wayfare.snapshot",
		"version": 1,
		"corridor": {
			"send": {"code": "USDC", "issuer": "GA5ZSEJY67GSD7W2JCGB6W52V3CBQ2K7D35E4E37D3C72F2X2Z5C3G5D"},
			"receive": {"code": "NGNC", "issuer": "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"}
		},
		"interactions": [
			{
					"method": "GET",
				"url": "http://example.com/test",
					"body_file": "responses/body1.bin"
				}
		]
	}`
	if err := os.WriteFile(filepath.Join(snapDir, "manifest.json"), []byte(manifestJSON), 0644); err != nil {
		store_fatal(t, err)
	}

	storeDir := filepath.Join(tmpDir, "store")
	store := NewStore(storeDir)

	// Update manifest interactions with correct body_sha256 matching file content to avoid Load validation errors
	hashStr, err := store.Put(bodyContent)
	if err != nil {
		store_fatal(t, err)
	}

	manifestJSONWithHash := `{
		"format": "wayfare.snapshot",
		"version": 1,
		"corridor": {
			"send": {"code": "USDC", "issuer": "GA5ZSEJY67GSD7W2JCGB6W52V3CBQ2K7D35E4E37D3C72F2X2Z5C3G5D"},
			"receive": {"code": "NGNC", "issuer": "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"}
		},
		"interactions": [
			{
				"method": "GET",
				"url": "http://example.com/test",
				"body_file": "responses/body1.bin",
				"body_sha256": "sha256:` + hashStr + `"
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(snapDir, "manifest.json"), []byte(manifestJSONWithHash), 0644); err != nil {
		store_fatal(t, err)
	}

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
		store_fatal_msg(t, "expected BodySHA256 to be set after migration")
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

	hash1, err := store.Put(body1)
	if err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	hash2, err := store.Put(body2)
	if err != nil {
		t.Fatalf("Put 2: %v", err)
	}
	hash3, err := store.Put(body3)
	if err != nil {
		t.Fatalf("Put 3: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("expected identical hashes for identical bodies, got %s vs %s", hash1, hash2)
	}
	if hash1 == hash3 {
		t.Errorf("expected different hashes for different bodies, got %s", hash1)
	}

	retrieved, err := store.Get(hash1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(retrieved) != string(body1) {
		t.Errorf("retrieved body = %s, want %s", string(retrieved), string(body1))
	}

	snapDir := filepath.Join(tmpDir, "snap")
	if err := os.MkdirAll(filepath.Join(snapDir, "responses"), 0755); err != nil {
		store_fatal(t, err)
	}

	manifestJSON := `{
		"format": "wayfare.snapshot",
		"version": 1,
		"corridor": {
			"send": {"code": "USDC", "issuer": "GA5ZSEJY67GSD7W2JCGB6W52V3CBQ2K7D35E4E37D3C72F2X2Z5C3G5D"},
			"receive": {"code": "NGNC", "issuer": "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"}
		},
		"interactions": [
			{
				"method": "GET",
				"url": "/test",
				"body_file": "responses/test.json",
				"body_sha256": "sha256:` + hash1 + `"
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(snapDir, "manifest.json"), []byte(manifestJSON), 0644); err != nil {
		store_fatal(t, err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, "responses", "test.json"), body1, 0644); err != nil {
		store_fatal(t, err)
	}

	if err := MigrateSnapshotDir(snapDir, store); err != nil {
		t.Fatalf("MigrateSnapshotDir: %v", err)
	}

	mNew, err := Load(snapDir)
	if err != nil {
		t.Fatalf("Load migrated snapshot: %v", err)
	}
	if len(mNew.Interactions) != 1 {
		t.Fatalf("expected 1 interaction, got %d", len(mNew.Interactions))
	}
}

func store_fatal(t *testing.T, err error) {
	t.Helper()
	t.Fatal(err)
}

func store_fatal_msg(t *testing.T, msg string) {
	t.Helper()
	t.Fatal(msg)
}
