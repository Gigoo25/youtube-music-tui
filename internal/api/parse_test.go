package api

import (
	"testing"
)

// TestIsYear: isYear guards the album/year split in byline parsing. Without it,
// a group like "219" (a room number, a track count) would be read as a year
// and assigned to Track.Year — which the TUI then renders as if it were the
// release date.
func TestIsYear(t *testing.T) {
	cases := []struct {
		in  string
		yes bool
	}{
		{"2019", true},
		{"1999", true},
		{"2002", true},
		{"219", false},   // too short
		{"20199", false}, // too long
		{"abc", false},   // non-numeric
		{"201a", false},  // mixed
		{"", false},      // empty
		{"12345", false}, // five digits
	}
	for _, c := range cases {
		if got := isYear(c.in); got != c.yes {
			t.Errorf("isYear(%q) = %v, want %v", c.in, got, c.yes)
		}
	}
}

// TestLooksLikeStat: a byline group that reads as a view/like counter is not
// an album name. Without this guard, "Daft Punk • 1.2M views" would assign
// "1.2M views" to Track.Album.
func TestLooksLikeStat(t *testing.T) {
	cases := []struct {
		in  string
		yes bool
	}{
		{"1.2M views", true},
		{"500 likes", true},
		{"0 views", true},
		{"Daft Punk", false},
		{"2019", false},
		{"Remaster", false},
	}
	for _, c := range cases {
		if got := looksLikeStat(c.in); got != c.yes {
			t.Errorf("looksLikeStat(%q) = %v, want %v", c.in, got, c.yes)
		}
	}
}

// TestExtractTwoRowTrackSkipsNonATV: a musicTwoRowItemRenderer whose watch
// endpoint points at a music video (OMV) must be dropped. OMVs carry the
// video soundtrack, not the clean song — playing one would give the user the
// wrong audio and no album metadata.
func TestExtractTwoRowTrackSkipsNonATV(t *testing.T) {
	r := map[string]any{
		"navigationEndpoint": map[string]any{
			"watchEndpoint": map[string]any{
				"videoId": "abc123",
				"watchEndpointMusicSupportedConfigs": map[string]any{
					"watchEndpointMusicConfig": map[string]any{
						"musicVideoType": "MUSIC_VIDEO_TYPE_OMV",
					},
				},
			},
		},
		"title": map[string]any{
			"runs": []any{map[string]any{"text": "Song Title"}},
		},
	}

	tt := extractTwoRowTrack(r)
	if tt.ID != "" {
		t.Fatalf("extractTwoRowTrack must drop non-ATV renderers (got id %q)", tt.ID)
	}
}

// TestExtractTwoRowTrackExtractsATV: an ATV renderer must yield a Track with
// id, title and artist. This is the common carousel-card path.
func TestExtractTwoRowTrackExtractsATV(t *testing.T) {
	r := map[string]any{
		"navigationEndpoint": map[string]any{
			"watchEndpoint": map[string]any{
				"videoId": "dQw4w9WgXcQ",
			},
		},
		"title": map[string]any{
			"runs": []any{map[string]any{"text": "Never Gonna Give You Up"}},
		},
		"subtitle": map[string]any{
			"runs": []any{
				map[string]any{"text": "Rick Astley"},
				map[string]any{"text": " • "},
				map[string]any{"text": "Whenever You Need Somebody"},
			},
		},
	}

	tt := extractTwoRowTrack(r)
	if tt.ID != "dQw4w9WgXcQ" {
		t.Fatalf("ID = %q, want dQw4w9WgXcQ", tt.ID)
	}
	if tt.Title != "Never Gonna Give You Up" {
		t.Fatalf("Title = %q, want 'Never Gonna Give You Up'", tt.Title)
	}
	if tt.Artist != "Rick Astley" {
		t.Fatalf("Artist = %q, want 'Rick Astley'", tt.Artist)
	}
}

// TestExtractTwoRowTrackMissingVideoId: a renderer without a videoId in its
// watch endpoint yields an empty Track. The caller filters these out — a
// non-empty track with no ID would crash the queue on access.
func TestExtractTwoRowTrackMissingVideoId(t *testing.T) {
	r := map[string]any{
		"title": map[string]any{
			"runs": []any{map[string]any{"text": "No ID"}},
		},
	}

	tt := extractTwoRowTrack(r)
	if tt.ID != "" {
		t.Fatalf("ID = %q, want empty (missing watchEndpoint)", tt.ID)
	}
}

// TestExtractPanelTrackFull: extractPanelTrack pulls a Track from a
// playlistPanelVideoRenderer. This is the queue view's row renderer — every
// track in the user's queue goes through it.
func TestExtractPanelTrackFull(t *testing.T) {
	r := map[string]any{
		"videoId": "abc123",
		"title": map[string]any{
			"runs": []any{map[string]any{"text": "Track One"}},
		},
		"longBylineText": map[string]any{
			"runs": []any{
				map[string]any{"text": "The Artist"},
				map[string]any{"text": " • "},
				map[string]any{"text": "The Album"},
				map[string]any{"text": " • "},
				map[string]any{"text": "2019"},
			},
		},
		"lengthText": map[string]any{
			"runs": []any{map[string]any{"text": "3:45"}},
		},
	}

	tt := extractPanelTrack(r)
	if tt.ID != "abc123" {
		t.Fatalf("ID = %q, want abc123", tt.ID)
	}
	if tt.Title != "Track One" {
		t.Fatalf("Title = %q, want 'Track One'", tt.Title)
	}
	if tt.Artist != "The Artist" {
		t.Fatalf("Artist = %q, want 'The Artist'", tt.Artist)
	}
	if tt.Album != "The Album" {
		t.Fatalf("Album = %q, want 'The Album'", tt.Album)
	}
	if tt.Year != "2019" {
		t.Fatalf("Year = %q, want '2019'", tt.Year)
	}
	if tt.Duration != "3:45" {
		t.Fatalf("Duration = %q, want '3:45'", tt.Duration)
	}
}

// TestExtractPanelTrackVideoByline: a video byline ("Artist • N views • N
// likes") must not assign the view count to Track.Album. Without looksLikeStat,
// "1.2M views" would be treated as an album name.
func TestExtractPanelTrackVideoByline(t *testing.T) {
	r := map[string]any{
		"videoId": "vid1",
		"title": map[string]any{
			"runs": []any{map[string]any{"text": "Video Track"}},
		},
		"longBylineText": map[string]any{
			"runs": []any{
				map[string]any{"text": "Artist Name"},
				map[string]any{"text": " • "},
				map[string]any{"text": "1.2M views"},
				map[string]any{"text": " • "},
				map[string]any{"text": "50K likes"},
			},
		},
	}

	tt := extractPanelTrack(r)
	if tt.Album != "" {
		t.Fatalf("Album = %q, want empty (view count must not be treated as album)", tt.Album)
	}
	if tt.Year != "" {
		t.Fatalf("Year = %q, want empty for video byline", tt.Year)
	}
	if tt.Artist != "Artist Name" {
		t.Fatalf("Artist = %q, want 'Artist Name'", tt.Artist)
	}
}

// TestExtractPanelTrackTwoGroupByline: a two-group byline ("Artist • Album")
// assigns the second group to Album only when it does not look like a stat.
// Without this guard, "Artist • 2019" would assign "2019" to Album instead of
// treating it as a year (which isYear then handles).
func TestExtractPanelTrackTwoGroupByline(t *testing.T) {
	r := map[string]any{
		"videoId": "vid2",
		"title": map[string]any{
			"runs": []any{map[string]any{"text": "Track Two"}},
		},
		"longBylineText": map[string]any{
			"runs": []any{
				map[string]any{"text": "Artist"},
				map[string]any{"text": " • "},
				map[string]any{"text": "Album Name"},
			},
		},
	}

	tt := extractPanelTrack(r)
	if tt.Album != "Album Name" {
		t.Fatalf("Album = %q, want 'Album Name'", tt.Album)
	}
}

// TestAddTrackSkipsEmptyId: a track with no ID must not be appended. The queue
// addresses tracks by ID — an empty-ID track would panic on any queue operation
// that indexes by position.
func TestAddTrackSkipsEmptyId(t *testing.T) {
	var tracks []Track
	seen := map[string]bool{}

	addTrack(Track{ID: "", Title: "No ID"}, &tracks, seen)

	if len(tracks) != 0 {
		t.Fatalf("len(tracks) = %d, want 0 (empty ID must be skipped)", len(tracks))
	}
}

// TestAddTrackSkipsDuplicate: a track whose ID is already in `seen` must not be
// appended. Without dedup, a search result that appears in multiple renderers
// (e.g. both a result row and a continuation) would duplicate in the queue.
func TestAddTrackSkipsDuplicate(t *testing.T) {
	var tracks []Track
	seen := map[string]bool{}

	addTrack(Track{ID: "abc", Title: "Same Track"}, &tracks, seen)
	addTrack(Track{ID: "abc", Title: "Same Track"}, &tracks, seen)

	if len(tracks) != 1 {
		t.Fatalf("len(tracks) = %d, want 1 (duplicate ID must be skipped)", len(tracks))
	}
}

// TestAddTrackSkipsEmptyTitle: a track with no title must not be appended.
// An empty-title track would render as a blank line in the queue.
func TestAddTrackSkipsEmptyTitle(t *testing.T) {
	var tracks []Track
	seen := map[string]bool{}

	addTrack(Track{ID: "abc", Title: ""}, &tracks, seen)

	if len(tracks) != 0 {
		t.Fatalf("len(tracks) = %d, want 0 (empty title must be skipped)", len(tracks))
	}
}

// TestAddTrackAppendsValid: a track with both ID and Title must be appended and
// recorded in `seen`.
func TestAddTrackAppendsValid(t *testing.T) {
	var tracks []Track
	seen := map[string]bool{}

	addTrack(Track{ID: "abc", Title: "Valid Track"}, &tracks, seen)

	if len(tracks) != 1 {
		t.Fatalf("len(tracks) = %d, want 1", len(tracks))
	}
	if !seen["abc"] {
		t.Fatal("seen must record the appended ID")
	}
	if tracks[0].Title != "Valid Track" {
		t.Fatalf("Track[0].Title = %q, want 'Valid Track'", tracks[0].Title)
	}
}

// TestRendererVideoType: rendererVideoType reads the musicVideoType from a
// renderer's watch endpoint. An empty string means the renderer has no watch
// endpoint (e.g. it navigates to a browse page, not a video).
func TestRendererVideoType(t *testing.T) {
	r := map[string]any{
		"navigationEndpoint": map[string]any{
			"watchEndpoint": map[string]any{
				"watchEndpointMusicSupportedConfigs": map[string]any{
					"watchEndpointMusicConfig": map[string]any{
						"musicVideoType": "MUSIC_VIDEO_TYPE_OMV",
					},
				},
			},
		},
	}

	if got := rendererVideoType(r); got != "MUSIC_VIDEO_TYPE_OMV" {
		t.Fatalf("rendererVideoType = %q, want MUSIC_VIDEO_TYPE_OMV", got)
	}
}

// TestRendererVideoTypeMissingEndpoint: a renderer without a watch endpoint
// must return "". Without this, a browse-page renderer would return a stale
// value from a nested field and get misclassified as a video.
func TestRendererVideoTypeMissingEndpoint(t *testing.T) {
	r := map[string]any{
		"title": map[string]any{
			"runs": []any{map[string]any{"text": "Album"}},
		},
	}

	if got := rendererVideoType(r); got != "" {
		t.Fatalf("rendererVideoType = %q, want empty (no watchEndpoint)", got)
	}
}

// TestListItemVideoType: listItemVideoType reads the type from the first flex
// column's title runs. An empty column or non-map run must return "".
func TestListItemVideoType(t *testing.T) {
	t.Run("with type", func(t *testing.T) {
		r := map[string]any{
			"flexColumns": []any{
				map[string]any{
					"musicResponsiveListItemFlexColumnRenderer": map[string]any{
						"text": map[string]any{
							"runs": []any{
								map[string]any{
									"navigationEndpoint": map[string]any{
										"watchEndpoint": map[string]any{
											"watchEndpointMusicSupportedConfigs": map[string]any{
												"watchEndpointMusicConfig": map[string]any{
													"musicVideoType": "MUSIC_VIDEO_TYPE_ATV",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		if got := listItemVideoType(r); got != "MUSIC_VIDEO_TYPE_ATV" {
			t.Fatalf("listItemVideoType = %q, want ATV", got)
		}
	})

	t.Run("no flex columns", func(t *testing.T) {
		r := map[string]any{}
		if got := listItemVideoType(r); got != "" {
			t.Fatalf("listItemVideoType = %q, want empty", got)
		}
	})
}

// TestHeaderTitle: headerTitle reads the title from whichever of the given
// header renderers the response uses. It must try each key in order and return
// "" when none matches.
func TestHeaderTitle(t *testing.T) {
	root := map[string]any{
		"musicDetailHeaderRenderer": map[string]any{
			"title": map[string]any{
				"runs": []any{map[string]any{"text": "Album Name"}},
			},
		},
	}

	got := headerTitle(root, "musicDetailHeaderRenderer", "musicResponsiveHeaderRenderer")
	if got != "Album Name" {
		t.Fatalf("headerTitle = %q, want 'Album Name'", got)
	}
}

// TestHeaderTitleFallsBack: when the first key has no title, headerTitle must
// try the next key. YouTube's API has changed header layouts across versions —
// this fallback is what keeps old responses working on new clients.
func TestHeaderTitleFallsBack(t *testing.T) {
	root := map[string]any{
		"musicResponsiveHeaderRenderer": map[string]any{
			"title": map[string]any{
				"runs": []any{map[string]any{"text": "Fallback Title"}},
			},
		},
	}

	got := headerTitle(root, "musicDetailHeaderRenderer", "musicResponsiveHeaderRenderer")
	if got != "Fallback Title" {
		t.Fatalf("headerTitle = %q, want 'Fallback Title'", got)
	}
}

// TestHeaderTitleMissingAll: when none of the given keys match, headerTitle
// must return "". Without this, a missing header would return garbage from an
// unrelated renderer that happens to share a key.
func TestHeaderTitleMissingAll(t *testing.T) {
	root := map[string]any{}
	got := headerTitle(root, "nonexistentRenderer")
	if got != "" {
		t.Fatalf("headerTitle = %q, want empty", got)
	}
}

// TestAlbumHeaderArtist: albumHeaderArtist reads the primary artist from the
// album page header, skipping the "Album"/"Single"/"EP" label and years.
func TestAlbumHeaderArtist(t *testing.T) {
	root := map[string]any{
		"musicDetailHeaderRenderer": map[string]any{
			"subtitle": map[string]any{
				"runs": []any{
					map[string]any{"text": "Album"},
					map[string]any{"text": " • "},
					map[string]any{"text": "Daft Punk"},
					map[string]any{"text": " • "},
					map[string]any{"text": "2001"},
				},
			},
		},
	}

	got := albumHeaderArtist(root)
	if got != "Daft Punk" {
		t.Fatalf("albumHeaderArtist = %q, want 'Daft Punk'", got)
	}
}

// TestAlbumHeaderArtistSkipsYear: a four-digit year in the subtitle must not be
// treated as the artist name. Without isYear, "Daft Punk • 2001" would return
// "2001" as the artist.
func TestAlbumHeaderArtistSkipsYear(t *testing.T) {
	root := map[string]any{
		"musicDetailHeaderRenderer": map[string]any{
			"subtitle": map[string]any{
				"runs": []any{
					map[string]any{"text": "Album"},
					map[string]any{"text": " • "},
					map[string]any{"text": "2001"},
				},
			},
		},
	}

	got := albumHeaderArtist(root)
	if got == "2001" {
		t.Fatal("albumHeaderArtist must not return a year as the artist name")
	}
}

// TestAlbumHeaderArtistMissing: when no recognized header exists, the function
// must return "" rather than panic or return garbage.
func TestAlbumHeaderArtistMissing(t *testing.T) {
	root := map[string]any{}
	if got := albumHeaderArtist(root); got != "" {
		t.Fatalf("albumHeaderArtist = %q, want empty", got)
	}
}

// TestBylineGroupsSplitsOnSeparator: bylineGroups must split runs on "•"
// separators and join text within each group. This is the foundation that
// extractPanelTrack and extractTwoRowTrack build their artist/album/year
// parsing on.
func TestBylineGroupsSplitsOnSeparator(t *testing.T) {
	runs := []any{
		map[string]any{"text": "Artist"},
		map[string]any{"text": " • "},
		map[string]any{"text": "Album"},
		map[string]any{"text": " • "},
		map[string]any{"text": "2019"},
	}

	got := bylineGroups(runs)
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	if got[0] != "Artist" {
		t.Fatalf("got[0] = %q, want 'Artist'", got[0])
	}
	if got[1] != "Album" {
		t.Fatalf("got[1] = %q, want 'Album'", got[1])
	}
	if got[2] != "2019" {
		t.Fatalf("got[2] = %q, want '2019'", got[2])
	}
}

// TestBylineGroupsSkipsEmptyGroupsExtra: leading, doubled, or trailing separators
// must not produce empty groups. Without this, a byline like "• Artist • Album"
// would yield ["", "Artist", "Album"] and shift every field by one position.
func TestBylineGroupsSkipsEmptyGroupsExtra(t *testing.T) {
	runs := []any{
		map[string]any{"text": " • "},
		map[string]any{"text": "Artist"},
		map[string]any{"text": " • "},
		map[string]any{"text": "Album"},
		map[string]any{"text": " • "},
	}

	got := bylineGroups(runs)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (empty groups must be skipped)", len(got))
	}
	if got[0] != "Artist" {
		t.Fatalf("got[0] = %q, want 'Artist'", got[0])
	}
	if got[1] != "Album" {
		t.Fatalf("got[1] = %q, want 'Album'", got[1])
	}
}
