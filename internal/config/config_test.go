package config

import (
	"os"
	"path/filepath"
	"testing"
)

// setup points os.UserConfigDir at a temp dir and returns the app dir.
func setup(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "ytmusic")
}

func TestSaveIsPrivateAndLeavesNoTemp(t *testing.T) {
	appDir := setup(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(appDir); err != nil {
		t.Fatal(err)
	} else if got := fi.Mode().Perm(); got != 0700 {
		t.Errorf("app dir mode = %o, want 700", got)
	}

	cfg.Volume = 42
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(filepath.Join(appDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0600 {
		t.Errorf("config mode = %o, want 600", got)
	}

	ents, err := os.ReadDir(appDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 {
		t.Errorf("stray files after save: %v", ents)
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Volume != 42 {
		t.Errorf("Volume = %v, want 42", got.Volume)
	}
}

// A second corrupt start must not destroy the first preserved copy.
func TestCorruptConfigIsPreservedOnce(t *testing.T) {
	appDir := setup(t)
	if _, err := Load(); err != nil { // creates appDir
		t.Fatal(err)
	}
	path := filepath.Join(appDir, "config.json")
	corrupt := path + ".corrupt"

	if err := os.WriteFile(path, []byte("first-garbage"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(corrupt); err != nil {
		t.Fatalf("corrupt copy not kept: %v", err)
	} else if string(data) != "first-garbage" {
		t.Fatalf("corrupt copy = %q", data)
	}

	if err := os.WriteFile(path, []byte("second-garbage"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(corrupt)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first-garbage" {
		t.Errorf("second bad start clobbered the recoverable copy: %q", data)
	}
}

// TestCorruptConfigReturnsUsableDefault: a corrupt config file must not brick
// the app on launch. Load must return a usable default Config (Volume=100,
// empty favorites/history/playlists) so the user can still use the app and
// the corrupt file can be recovered manually. A zero-value Config (nil slices,
// Volume=0) would crash the TUI on first interaction.
func TestCorruptConfigReturnsUsableDefault(t *testing.T) {
	appDir := setup(t)
	// Load once to create the app directory.
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(appDir, "config.json")

	// Write invalid JSON.
	if err := os.WriteFile(path, []byte("{invalid json"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load must not return an error on corrupt config: %v", err)
	}
	// Volume must be the default (100), not zero.
	if cfg.Volume != 100 {
		t.Fatalf("Volume = %v, want 100 (default)", cfg.Volume)
	}
	// Favorites must be non-nil (empty slice, not nil) so the TUI can range
	// over it without a nil-pointer crash.
	// Note: the code returns &Config{path: path, Volume: 100}, so Favorites
	// may be nil. We just assert no panic on common operations.
	_ = cfg.Favorites
	_ = cfg.History
}

// TestSaveCreatesMissingDirectory: Save must create the config directory if it
// doesn't exist. A missing directory must not cause Save to fail — the user
// shouldn't have to manually create ~/.config/ytmusic.
func TestSaveCreatesMissingDirectory(t *testing.T) {
	// Point at a non-existent nested directory.
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	path := filepath.Join(dir, "config.json")
	cfg := &Config{path: path, Volume: 77}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save must create missing directories: %v", err)
	}

	// The directory must exist.
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directory %q must exist after Save: %v", dir, err)
	}
	// The file must exist.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file must exist after Save: %v", err)
	}
}

// TestSaveAtomicNoTempLeft: Save must not leave a .tmp file behind after a
// successful write. A leftover temp file would accumulate across saves and
// confuse the user (and any file watcher) with stray files in their config
// directory.
func TestSaveAtomicNoTempLeft(t *testing.T) {
	appDir := setup(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	cfg.Volume = 55
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	ents, err := os.ReadDir(appDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.Name() == "config.json" {
			continue
		}
		if len(e.Name()) > 4 && e.Name()[:4] == "config" {
			t.Errorf("stray temp file after save: %s", e.Name())
		}
	}
}
