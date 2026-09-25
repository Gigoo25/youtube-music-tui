package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func writeColors(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "colors.json")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadThemeFileFull(t *testing.T) {
	path := writeColors(t, `{"primary":"#e5c799","secondary":"#cbaba0","text":"#dcd8cd",
		"accent":"#d99a89","dim":"#76695c","error":"#cf767c","success":"#9aa887",
		"warning":"#e5c799","background":"#161311"}`)
	got, ok := loadThemeFile(path)
	if !ok {
		t.Fatal("valid file not loaded")
	}
	want := theme{name: desktopThemeName, primary: "#e5c799", secondary: "#cbaba0", text: "#dcd8cd",
		accent: "#d99a89", dim: "#76695c", errc: "#cf767c", success: "#9aa887",
		warning: "#e5c799", bg: "#161311"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestLoadThemeFileFallsBackPerField(t *testing.T) {
	// text is missing, dim is malformed: both take the default theme's value.
	path := writeColors(t, `{"primary":"#123456","dim":"red"}`)
	got, ok := loadThemeFile(path)
	if !ok {
		t.Fatal("partial file not loaded")
	}
	base := defaultBuiltinTheme()
	if got.primary != "#123456" {
		t.Errorf("primary = %q, want #123456", got.primary)
	}
	if got.text != base.text || got.dim != base.dim {
		t.Errorf("fallbacks = (%q, %q), want (%q, %q)", got.text, got.dim, base.text, base.dim)
	}
}

func TestLoadThemeFileMissingOrInvalid(t *testing.T) {
	if _, ok := loadThemeFile(filepath.Join(t.TempDir(), "absent.json")); ok {
		t.Error("missing file reported as loaded")
	}
	if _, ok := loadThemeFile(writeColors(t, `{not json`)); ok {
		t.Error("invalid JSON reported as loaded")
	}
}

func TestWithDesktopThemeDoesNotStack(t *testing.T) {
	d := theme{name: desktopThemeName, primary: "#000000"}
	list := withDesktopTheme(withDesktopTheme(themes, d), d)
	if list[0].name != desktopThemeName {
		t.Fatalf("first theme = %q, want %q", list[0].name, desktopThemeName)
	}
	n := 0
	for _, x := range list {
		if x.name == desktopThemeName {
			n++
		}
	}
	if n != 1 || len(list) != len(themes)+1 {
		t.Errorf("desktop copies = %d, len = %d (builtins %d)", n, len(list), len(themes))
	}
}
