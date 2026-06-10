package api

import "testing"

// TestSanitizeDisplayStripsControlChars: YouTube-supplied titles must not carry
// terminal escape sequences or row-breaking characters into rendered rows.
func TestSanitizeDisplayStripsControlChars(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain title", "plain title"},
		{"esc\x1b]0;owned\x07seq", "esc]0;ownedseq"}, // OSC injection
		{"multi\nline", "multi line"},                // row break -> space
		{"del\x7fchar", "delchar"},                   // DEL
		{"c1\u009bCSI", "c1CSI"},                     // C1 control
		{"tab\tsep", "tab sep"},                      // C0 control -> space
		{"héllo • 日本語", "héllo • 日本語"},               // non-ASCII kept
	}
	for _, c := range cases {
		if got := sanitizeDisplay(c.in); got != c.want {
			t.Errorf("sanitizeDisplay(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestCleanTracksSanitizesAllFields: every text field passes through
// sanitizeDisplay, and artist separator-artifact cleanup still applies.
func TestCleanTracksSanitizesAllFields(t *testing.T) {
	ts := CleanTracks([]Track{{
		ID:       "x",
		Title:    "Ti\x1btle",
		Artist:   "Art\x1bist",
		Album:    "Al\x07bum",
		Duration: "3:\x1b45",
		Year:     "20\x0024",
	}, {
		ID:     "y",
		Artist: " & ", // lone separator artifact
	}})
	got := ts[0]
	if got.Title != "Title" || got.Artist != "Artist" || got.Album != "Album" ||
		got.Duration != "3:45" || got.Year != "2024" {
		t.Errorf("CleanTracks left control chars: %+v", got)
	}
	if ts[1].Artist != "" {
		t.Errorf("CleanTracks(%q) artist = %q, want empty", " & ", ts[1].Artist)
	}
}
