package api

import "testing"

func TestCleanArtist(t *testing.T) {
	cases := map[string]string{
		"&":                           "",
		" & ":                         "",
		"feat.":                       "",
		"Ellie Goulding":              "Ellie Goulding",
		"Ellie Goulding & Juice WRLD": "Ellie Goulding & Juice WRLD",
		"& Juice WRLD":                "Juice WRLD",
		"":                            "",
	}
	for in, want := range cases {
		if got := cleanArtist(in); got != want {
			t.Errorf("cleanArtist(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSanitizeDisplayKeepsZWJ guards A7: U+200D joins emoji sequences. Stripping
// it with the rest of the zero-width range split "👨‍🎤" into two glyphs, which is
// exactly what sanitizeDisplay's contract says it must not do.
func TestSanitizeDisplayKeepsZWJ(t *testing.T) {
	const singer = "\U0001F468\u200D\U0001F3A4" // man singer
	if got := sanitizeDisplay("Artist " + singer); got != "Artist "+singer {
		t.Errorf("sanitizeDisplay stripped the ZWJ: got %q, want %q", got, "Artist "+singer)
	}
	// The rest of the range must still go.
	if got := sanitizeDisplay("a\u200bb\u200ec"); got != "abc" {
		t.Errorf("sanitizeDisplay(%q) = %q, want \"abc\"", "a\u200bb\u200ec", got)
	}
}
