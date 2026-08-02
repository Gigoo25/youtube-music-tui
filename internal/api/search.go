package api

import "encoding/json"

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
	walkRenderers(root, "musicResponsiveListItemRenderer", func(r map[string]any) {
		addTrack(extractSongRow(r), &tracks, seen)
	})
	return CleanTracks(tracks), findSearchContinuation(root), nil
}

// findSearchContinuation locates the token for the next page of results. Modern
// Innertube responses carry it in a continuationCommand; older shelf layouts use
// nextContinuationData. Returns "" when there is no further page.
func findSearchContinuation(node any) string {
	var tok string
	walkFirst(node, func(m map[string]any) bool {
		if cc, ok := m["continuationCommand"].(map[string]any); ok {
			tok = str(cc["token"])
		}
		if tok == "" {
			if nc, ok := m["nextContinuationData"].(map[string]any); ok {
				tok = str(nc["continuation"])
			}
		}
		return tok != ""
	})
	return tok
}

// extractSongRow parses a search "Songs" row. flexColumn[0] is the title (and may
// carry the watch endpoint); flexColumn[1] is "Artist • Album • Duration" (some
// rows omit the album). The videoId is most reliably in playlistItemData.
func extractSongRow(r map[string]any) Track {
	var t Track
	t.ID = str(dig(r, "playlistItemData", "videoId"))

	byline := flexRuns(r, 1)

	if titleRuns := flexRuns(r, 0); len(titleRuns) > 0 {
		t.Title = str(dig(titleRuns[0], "text"))
		if t.ID == "" {
			t.ID = str(dig(titleRuns[0], "navigationEndpoint", "watchEndpoint", "videoId"))
		}
	}

	// The album run links the album page — keep its browse id so "open album"
	// can browse directly instead of relying on search (which can't find some
	// albums, e.g. deluxe editions, by name).
	t.AlbumID = firstAlbumID(byline)

	// Second column: meaningful runs are Artist [, Album][, Duration] (the " • "
	// separator runs are dropped by extractTexts). The trailing text is the
	// duration only when it reads as one; otherwise it is the album, so a row
	// with an album but no duration doesn't print the album name as a runtime.
	texts := extractTexts(byline)
	if len(texts) > 0 {
		t.Artist = texts[0]
	}
	if len(texts) > 1 {
		if last := texts[len(texts)-1]; durationRe.MatchString(last) {
			t.Duration = last
			if len(texts) > 2 {
				t.Album = texts[1]
			}
		} else {
			t.Album = texts[1]
		}
	}

	if t.ID == "" {
		t.ID = str(dig(r, "overlay", "musicItemThumbnailOverlayRenderer", "content",
			"musicPlayButtonRenderer", "playNavigationEndpoint", "watchEndpoint", "videoId"))
	}
	return t
}
