package api

import (
	"encoding/json"
	"strings"
)

// songsFilterParam restricts a YouTube Music search to the "Songs" tab — the
// official audio tracks (videoType ATV) rather than music videos (OMV), so the
// resolved stream is the clean song version, not a video edit/live/remix. This is
// the same opaque `params` value ytmusicapi uses for filter="songs".
const songsFilterParam = "EgWKAQIIAWoMEAMQBBAJEAUQChAV"

// SearchSongs is like Search but filters to the Songs tab, avoiding music-video
// results. Song rows are parsed here (rather than via extractTrack) because the
// Songs-tab flexColumn layout puts artist/album/duration in the second column.
func (c *Client) SearchSongs(query string) ([]Track, error) {
	tracks, _, err := c.SearchSongsPage(query, "")
	return tracks, err
}

// SearchSongsPage runs a Songs-tab search (continuation == "") or fetches the
// next page of an earlier one (continuation != "", the token from a prior call).
// It returns the page's tracks plus the token for the following page ("" when
// the results are exhausted), so callers can lazily load more.
func (c *Client) SearchSongsPage(query, continuation string) ([]Track, string, error) {
	payload := c.clientCtx()
	if continuation != "" {
		// The token already encodes the filtered search context, so query/params
		// are not resent.
		payload["continuation"] = continuation
	} else {
		payload["query"] = query
		payload["params"] = songsFilterParam
	}

	body, err := c.post("search", payload)
	if err != nil {
		return nil, "", err
	}

	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, "", err
	}

	var tracks []Track
	seen := map[string]bool{}
	collectSongRows(root, &tracks, seen)
	return CleanTracks(tracks), findSearchContinuation(root), nil
}

// findSearchContinuation locates the token for the next page of results. Modern
// Innertube responses carry it in a continuationCommand; older shelf layouts use
// nextContinuationData. Returns "" when there is no further page.
func findSearchContinuation(node any) string {
	switch v := node.(type) {
	case map[string]any:
		if cc, ok := v["continuationCommand"].(map[string]any); ok {
			if tok := str(cc["token"]); tok != "" {
				return tok
			}
		}
		if nc, ok := v["nextContinuationData"].(map[string]any); ok {
			if tok := str(nc["continuation"]); tok != "" {
				return tok
			}
		}
		for _, child := range v {
			if r := findSearchContinuation(child); r != "" {
				return r
			}
		}
	case []any:
		for _, child := range v {
			if r := findSearchContinuation(child); r != "" {
				return r
			}
		}
	}
	return ""
}

// collectSongRows walks the search response for musicResponsiveListItemRenderer
// rows and parses each as a song.
func collectSongRows(node any, out *[]Track, seen map[string]bool) {
	switch v := node.(type) {
	case map[string]any:
		if r, ok := v["musicResponsiveListItemRenderer"].(map[string]any); ok {
			addTrack(extractSongRow(r), out, seen)
		}
		for _, child := range v {
			collectSongRows(child, out, seen)
		}
	case []any:
		for _, child := range v {
			collectSongRows(child, out, seen)
		}
	}
}

// extractSongRow parses a search "Songs" row. flexColumn[0] is the title (and may
// carry the watch endpoint); flexColumn[1] is "Artist • Album • Duration" (some
// rows omit the album). The videoId is most reliably in playlistItemData.
func extractSongRow(r map[string]any) Track {
	var t Track
	t.ID = str(dig(r, "playlistItemData", "videoId"))

	flex := digSlice(dig(r, "flexColumns"))
	col := func(i int) []any {
		if i >= len(flex) {
			return nil
		}
		return digSlice(dig(flex[i], "musicResponsiveListItemFlexColumnRenderer", "text", "runs"))
	}

	if titleRuns := col(0); len(titleRuns) > 0 {
		t.Title = str(dig(titleRuns[0], "text"))
		if t.ID == "" {
			t.ID = str(dig(titleRuns[0], "navigationEndpoint", "watchEndpoint", "videoId"))
		}
	}

	// The album run links the album page — keep its browse id so "open album"
	// can browse directly instead of relying on search (which can't find some
	// albums, e.g. deluxe editions, by name).
	for _, run := range col(1) {
		if id := str(dig(run, "navigationEndpoint", "browseEndpoint", "browseId")); strings.HasPrefix(id, "MPREb") {
			t.AlbumID = id
			break
		}
	}

	// Second column: meaningful runs are Artist [, Album], Duration (the " • "
	// separator runs are dropped by extractTexts).
	texts := extractTexts(col(1))
	switch {
	case len(texts) >= 3:
		t.Artist = texts[0]
		t.Album = texts[1]
		t.Duration = texts[len(texts)-1]
	case len(texts) == 2:
		t.Artist = texts[0]
		t.Duration = texts[1]
	case len(texts) == 1:
		t.Artist = texts[0]
	}

	if t.ID == "" {
		t.ID = str(dig(r, "overlay", "musicItemThumbnailOverlayRenderer", "content",
			"musicPlayButtonRenderer", "playNavigationEndpoint", "watchEndpoint", "videoId"))
	}
	return t
}
