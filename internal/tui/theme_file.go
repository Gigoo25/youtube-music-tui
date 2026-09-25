package tui

import (
	"encoding/json"
	"os"
	"regexp"
)

// desktopThemeName is the theme built from colors.json.
const desktopThemeName = "desktop"

// themeFile is colors.json: a palette managed outside the app (e.g. generated
// by a dotfiles setup from the desktop's colors). Every field is optional and
// must be "#rrggbb"; a missing or malformed one falls back to the default
// built-in theme's value, so a bad file can never break the UI.
type themeFile struct {
	Primary    string `json:"primary"`
	Secondary  string `json:"secondary"`
	Text       string `json:"text"`
	Accent     string `json:"accent"`
	Dim        string `json:"dim"`
	Error      string `json:"error"`
	Success    string `json:"success"`
	Warning    string `json:"warning"`
	Background string `json:"background"`
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// defaultBuiltinTheme is the first built-in theme, skipping one loaded from a
// file, so fallbacks never come from the file itself.
func defaultBuiltinTheme() theme {
	for _, t := range themes {
		if t.name != desktopThemeName {
			return t
		}
	}
	return themes[0]
}

// loadThemeFile reads colors.json at path. ok is false when the file is
// absent or not valid JSON.
func loadThemeFile(path string) (theme, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return theme{}, false
	}
	var f themeFile
	if err := json.Unmarshal(data, &f); err != nil {
		return theme{}, false
	}
	base := defaultBuiltinTheme()
	pick := func(v, fallback string) string {
		if hexColor.MatchString(v) {
			return v
		}
		return fallback
	}
	return theme{
		name:      desktopThemeName,
		primary:   pick(f.Primary, base.primary),
		secondary: pick(f.Secondary, base.secondary),
		text:      pick(f.Text, base.text),
		accent:    pick(f.Accent, base.accent),
		dim:       pick(f.Dim, base.dim),
		errc:      pick(f.Error, base.errc),
		success:   pick(f.Success, base.success),
		warning:   pick(f.Warning, base.warning),
		bg:        pick(f.Background, base.bg),
	}, true
}

// withDesktopTheme returns list with t first, replacing any earlier desktop
// theme so repeated loads do not stack copies.
func withDesktopTheme(list []theme, t theme) []theme {
	out := []theme{t}
	for _, x := range list {
		if x.name != desktopThemeName {
			out = append(out, x)
		}
	}
	return out
}
