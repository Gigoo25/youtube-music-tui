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

// textRuns builds the Innertube "runs" shape from raw run texts.
func textRuns(texts ...string) []any {
	runs := make([]any, 0, len(texts))
	for _, t := range texts {
		runs = append(runs, map[string]any{"text": t})
	}
	return runs
}

// TestBylineGroupsSkipsEmptyGroups: a byline that leads, doubles or trails with
// a separator must not shift the [artist, album, year] group positions.
func TestBylineGroupsSkipsEmptyGroups(t *testing.T) {
	cases := []struct {
		name string
		runs []string
		want []string
	}{
		{"plain", []string{"Artist", " • ", "Album", " • ", "2021"}, []string{"Artist", "Album", "2021"}},
		{"leading", []string{"•", "Artist", " • ", "Album"}, []string{"Artist", "Album"}},
		{"trailing", []string{"Artist", " • ", "Album", " • "}, []string{"Artist", "Album"}},
		{"doubled", []string{"Artist", " • ", " • ", "2021"}, []string{"Artist", "2021"}},
		{"multi-run group", []string{"A", ", ", "B", " • ", "Album"}, []string{"A, B", "Album"}},
	}
	for _, c := range cases {
		got := bylineGroups(textRuns(c.runs...))
		if len(got) != len(c.want) {
			t.Errorf("%s: bylineGroups = %q, want %q", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: bylineGroups = %q, want %q", c.name, got, c.want)
				break
			}
		}
	}
}

// TestExtractSongRowDurationPrecedence: the trailing byline text is a duration
// only when it reads as one — otherwise it is the album, and the duration column
// stays empty rather than showing the album name.
func TestExtractSongRowDurationPrecedence(t *testing.T) {
	cases := []struct {
		name                    string
		byline                  []string
		artist, album, duration string
	}{
		{"artist and duration", []string{"Artist", " • ", "3:12"}, "Artist", "", "3:12"},
		{"album, no duration", []string{"Artist", " • ", "Some Album"}, "Artist", "Some Album", ""},
		{"artist album duration", []string{"Artist", " • ", "Some Album", " • ", "3:12"}, "Artist", "Some Album", "3:12"},
		{"year tail, no duration", []string{"Artist", " • ", "Some Album", " • ", "2021"}, "Artist", "Some Album", ""},
		{"artist only", []string{"Artist"}, "Artist", "", ""},
	}
	for _, c := range cases {
		flexCol := func(runs []any) any {
			return map[string]any{"musicResponsiveListItemFlexColumnRenderer": map[string]any{
				"text": map[string]any{"runs": runs},
			}}
		}
		got := extractSongRow(map[string]any{
			"playlistItemData": map[string]any{"videoId": "vid"},
			"flexColumns": []any{
				flexCol(textRuns("Title")),
				flexCol(textRuns(c.byline...)),
			},
		})
		if got.Artist != c.artist || got.Album != c.album || got.Duration != c.duration {
			t.Errorf("%s: extractSongRow = artist %q, album %q, duration %q; want %q, %q, %q",
				c.name, got.Artist, got.Album, got.Duration, c.artist, c.album, c.duration)
		}
	}
}
