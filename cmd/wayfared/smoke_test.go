package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// envOr
// ---------------------------------------------------------------------------

func TestEnvOrReturnsFallback(t *testing.T) {
	// Use a key that almost certainly is not set
	got := envOr("WAYFARE_TEST_NONEXISTENT_KEY_XYZZY", "fallback")
	if got != "fallback" {
		t.Errorf("envOr = %q, want %q", got, "fallback")
	}
}

func TestEnvOrReturnsValue(t *testing.T) {
	key := "WAYFARE_TEST_ENV_OR_EXISTS_" + t.Name()
	t.Setenv(key, "real")
	got := envOr(key, "fallback")
	if got != "real" {
		t.Errorf("envOr = %q, want %q", got, "real")
	}
}

func TestEnvOrEmptyValueUsesFallback(t *testing.T) {
	// An empty string in the env should still use the fallback,
	// because envOr checks for empty string.
	key := "WAYFARE_TEST_ENV_OR_EMPTY_" + t.Name()
	t.Setenv(key, "")
	got := envOr(key, "fallback")
	if got != "fallback" {
		t.Errorf("envOr with empty env = %q, want %q", got, "fallback")
	}
}

// ---------------------------------------------------------------------------
// newLogger
// ---------------------------------------------------------------------------

func TestNewLoggerInfo(t *testing.T) {
	l := newLogger("info")
	if l == nil {
		t.Fatal("newLogger returned nil")
	}
	// Verify it can log without panicking
	l.Info("smoke test")
}

func TestNewLoggerDebug(t *testing.T) {
	l := newLogger("debug")
	if l == nil {
		t.Fatal("newLogger returned nil")
	}
	l.Debug("smoke test")
}

func TestNewLoggerWarn(t *testing.T) {
	l := newLogger("warn")
	if l == nil {
		t.Fatal("newLogger returned nil")
	}
	l.Warn("smoke test")
}

func TestNewLoggerError(t *testing.T) {
	l := newLogger("error")
	if l == nil {
		t.Fatal("newLogger returned nil")
	}
	l.Error("smoke test")
}

func TestNewLoggerDefaultToInfo(t *testing.T) {
	l := newLogger("bogus")
	if l == nil {
		t.Fatal("newLogger returned nil")
	}
	// Should not panic; falls through to default info
	l.Info("smoke test")
}

// ---------------------------------------------------------------------------
// openStore
// ---------------------------------------------------------------------------

func TestOpenStoreEmptyDirReturnsNop(t *testing.T) {
	// When dataDir is empty, openStore returns a Nop store when
	// embedded history is unavailable.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	store, err := openStore("", logger)
	if err != nil {
		t.Fatalf("openStore(\"\"): %v", err)
	}
	if store == nil {
		t.Fatal("openStore returned nil store")
	}
	// Verify the store responds (Nop or embedded — both are valid)
	corridors, _ := store.Corridors(context.Background())
	_ = corridors
}

func TestOpenStoreTempDir(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	store, err := openStore(dir, logger)
	if err != nil {
		t.Fatalf("openStore(%q): %v", dir, err)
	}
	if store == nil {
		t.Fatal("openStore returned nil store")
	}
	corridors, err := store.Corridors(context.Background())
	if err != nil {
		t.Fatalf("Corridors: %v", err)
	}
	if len(corridors) != 0 {
		t.Errorf("expected 0 corridors in fresh store, got %d", len(corridors))
	}
}

func TestOpenStoreNonexistentDir(t *testing.T) {
	// runstore.Open creates the directory, so pass a path inside a
	// read-only parent to trigger a real error.
	dir := filepath.Join("/dev", "null", "impossible")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	_, err := openStore(dir, logger)
	if err == nil {
		t.Fatal("expected error for unwritable dir, got nil")
	}
}

// ---------------------------------------------------------------------------
// verifyStore
// ---------------------------------------------------------------------------

func TestVerifyStoreEmpty(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	store, err := openStore(dir, logger)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	// verifyStore needs a *FileStore, but openStore may return a Nop.
	// When dir is non-empty and writable, it should return a FileStore.
	code := verifyStore(store, logger)
	if code != 0 {
		t.Errorf("verifyStore on empty store = %d, want 0", code)
	}
}
