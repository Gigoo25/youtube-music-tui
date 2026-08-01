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
