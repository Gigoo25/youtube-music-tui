package api

import (
	"context"
	"encoding/json"
	"strings"
)

// videoTypeATV marks an official song ("audio track version") in Innertube
// watch endpoints, as opposed to OMV (official music video) and UGC (user
// uploads). The app wants YouTube-Music-style audio: ATV only.
const videoTypeATV = "MUSIC_VIDEO_TYPE_ATV"

// rendererVideoType reads the musicVideoType a renderer's watch endpoint
// points at ("" when the renderer doesn't carry one).
func rendererVideoType(r map[string]any) string {
	return str(dig(r, "navigationEndpoint", "watchEndpoint",
		"watchEndpointMusicSupportedConfigs", "watchEndpointMusicConfig",
		"musicVideoType"))
}

// flexRuns returns the text runs of a list row's i-th flex column (nil when the
// row has no such column).
func flexRuns(r map[string]any, i int) []any {
	cols := digSlice(dig(r, "flexColumns"))
	if i >= len(cols) {
		return nil
	}
	return digSlice(dig(cols[i], "musicResponsiveListItemFlexColumnRenderer", "text", "runs"))
}

// firstAlbumID returns the album browse id (MPREb…) linked from a byline's runs,
// or "" when no run links an album page (see Track.AlbumID).
func firstAlbumID(runs []any) string {
	for _, run := range runs {
		if id := str(dig(run, "navigationEndpoint", "browseEndpoint", "browseId")); strings.HasPrefix(id, "MPREb") {
			return id
		}
	}
	return ""
}

// extractTwoRowTrack pulls a Track out of a musicTwoRowItemRenderer (carousel
// card). Only items whose navigation points at a watchEndpoint (i.e. a single
// song/video) are returned with an ID. Items that identify themselves as
// music videos (non-ATV) are dropped — their audio is the video soundtrack,
// not the song, and they carry no album metadata.
func extractTwoRowTrack(r map[string]any) Track {
	var t Track
	if vt := rendererVideoType(r); vt != "" && vt != videoTypeATV {
		return t
	}
	t.ID = str(dig(r, "navigationEndpoint", "watchEndpoint", "videoId"))

	titleRuns := digSlice(dig(r, "title", "runs"))
	if len(titleRuns) > 0 {
		t.Title = str(dig(titleRuns[0], "text"))
	}

	// The byline groups as "Artist • Album • Year" (or "Artist • N views" for a
	// video card): only the first group is the artist.
	groups := bylineGroups(digSlice(dig(r, "subtitle", "runs")))
	if len(groups) > 0 {
		t.Artist = groups[0]
	}
	return t
}

// bylineGroups splits a byline's runs on the "•" separator runs, joining the
// texts inside each group. A song byline groups as [artist(s), album, year];
// a video byline as [artist, views, likes].
func bylineGroups(runs []any) []string {
	var groups []string
	cur := ""
	for _, run := range runs {
		text := str(dig(run, "text"))
		if strings.TrimSpace(text) == "•" {
			// Empty groups (leading, doubled or trailing separator) are skipped
			// so group positions stay [artist, album, year].
			if cur != "" {
				groups = append(groups, cur)
			}
			cur = ""
			continue
		}
		cur += text
	}
	if cur != "" {
		groups = append(groups, cur)
	}
	return groups
}

// extractPanelTrack pulls a Track out of a playlistPanelVideoRenderer.
func extractPanelTrack(r map[string]any) Track {
	var t Track
	t.ID = str(dig(r, "videoId"))

	titleRuns := digSlice(dig(r, "title", "runs"))
	if len(titleRuns) > 0 {
		t.Title = str(dig(titleRuns[0], "text"))
	}

	byline := digSlice(dig(r, "longBylineText", "runs"))
	// json.Unmarshal turns "runs": [] into a non-nil empty slice, so a nil check
	// left the shortBylineText fallback dead.
	if len(byline) == 0 {
		byline = digSlice(dig(r, "shortBylineText", "runs"))
	}
	// The album run links the album page; keep its browse id (see Track.AlbumID).
	t.AlbumID = firstAlbumID(byline)
	groups := bylineGroups(byline)
	if len(groups) > 0 {
		t.Artist = groups[0]
	}
	// Song bylines are "Artist • Album • Year"; video bylines are
	// "Artist • N views • N likes" and contribute no album/year.
	if len(groups) >= 3 && isYear(groups[len(groups)-1]) {
		t.Year = groups[len(groups)-1]
		t.Album = strings.Join(groups[1:len(groups)-1], " ")
	} else if len(groups) == 2 && !looksLikeStat(groups[1]) {
		t.Album = groups[1]
	}

	durRuns := digSlice(dig(r, "lengthText", "runs"))
	if len(durRuns) > 0 {
		t.Duration = str(dig(durRuns[0], "text"))
	}
	return t
}

// isYear reports whether s is a 4-digit year.
func isYear(s string) bool {
	if len(s) != 4 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// looksLikeStat reports whether a byline group is a view/like counter rather
// than an album name.
func looksLikeStat(s string) bool {
	return strings.HasSuffix(s, " views") || strings.HasSuffix(s, " likes")
}

func addTrack(t Track, out *[]Track, seen map[string]bool) {
	if t.ID == "" || t.Title == "" || seen[t.ID] {
		return
	}
	seen[t.ID] = true
	*out = append(*out, t)
}

// Related returns songs related to the given video (for radio / autoplay),
// filtered to official song audio (ATV) — never music videos.
//
// The radio queue inherits the seed's type: a song seed yields songs, a video
// seed yields videos. When the seed turns out to be a video (history entries
// from older app versions, charts fallback items), the seed is remapped to its
// song version via a Songs-tab search and the radio re-fetched, so one video
// in the queue can't poison every radio chain after it.
func (c *Client) Related(ctx context.Context, videoID string) ([]Track, error) {
	return c.related(ctx, videoID, true)
}

func (c *Client) related(ctx context.Context, videoID string, allowReseed bool) ([]Track, error) {
	if videoID == "" {
		return nil, nil
	}

	payload := c.clientCtx()
	payload["videoId"] = videoID
	payload["playlistId"] = "RDAMVM" + videoID
	payload["isAudioOnly"] = true
	payload["params"] = "wAEB"

	body, err := c.post(ctx, "next", payload)
	if err != nil {
		return nil, err
	}

	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}

	tabs := digSlice(dig(root, "contents",
		"singleColumnMusicWatchNextResultsRenderer",
		"tabbedRenderer", "watchNextTabbedResultsRenderer", "tabs"))
	if len(tabs) == 0 {
		return nil, nil
	}

	contents := digSlice(dig(tabs[0], "tabRenderer", "content",
		"musicQueueRenderer", "content", "playlistPanelRenderer", "contents"))

	var tracks []Track
	seen := map[string]bool{}
	seedTitle, seedArtist := "", ""
	for _, item := range contents {
		r, ok := dig(item, "playlistPanelVideoRenderer").(map[string]any)
		if !ok {
			// Some queues wrap items (video primary + song counterpart). Prefer
			// the song counterpart; fall back to the primary renderer.
			if cp, ok2 := dig(item, "playlistPanelVideoWrapperRenderer",
				"counterpart").([]any); ok2 && len(cp) > 0 {
				r, _ = dig(cp[0], "counterpartRenderer", "playlistPanelVideoRenderer").(map[string]any)
			}
			if r == nil {
				r, _ = dig(item, "playlistPanelVideoWrapperRenderer",
					"primaryRenderer", "playlistPanelVideoRenderer").(map[string]any)
			}
			if r == nil {
				continue
			}
		}
		t := extractPanelTrack(r)
		if t.ID == videoID { // the seed itself; keep its identity for re-seeding
			if seedTitle == "" {
				seedTitle, seedArtist = t.Title, t.Artist
			}
			continue
		}
		if vt := rendererVideoType(r); vt != "" && vt != videoTypeATV {
			continue // music video — not the song
		}
		addTrack(t, &tracks, seen)
	}

	// A video seed yields an all-video queue (filtered to nothing above). Remap
	// the seed to its official song version and fetch the radio from that.
	if len(tracks) == 0 && allowReseed && seedTitle != "" {
		q := seedTitle
		if seedArtist != "" {
			q += " " + seedArtist
		}
		if songs, serr := c.SearchSongs(ctx, q); serr == nil && len(songs) > 0 && songs[0].ID != videoID {
			return c.related(ctx, songs[0].ID, false)
		}
	}

	return CleanTracks(dedupeTracks(tracks)), nil
}
