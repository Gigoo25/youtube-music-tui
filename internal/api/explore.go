package api

import (
	"encoding/json"
	"strings"
)

// Explore returns a feed of explore/mood/genre songs from YouTube Music.
//
// Uses the Innertube "browse" endpoint with the FEmusic_explore browseId and
// walks the returned carousel shelves for any songs that carry a videoId.
func (c *Client) Explore() ([]Track, error) {
	payload := c.clientCtx()
	payload["browseId"] = "FEmusic_explore"

	body, err := c.post("browse", payload)
	if err != nil {
		return nil, err
	}

	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}

	var tracks []Track
	seen := map[string]bool{}
	collectBrowseTracks(root, &tracks, seen)

	if len(tracks) > 50 {
		tracks = tracks[:50]
	}
	return CleanTracks(tracks), nil
}

// collectBrowseTracks walks an arbitrary Innertube browse response and pulls
// out every song-like item that exposes a videoId. It descends into all maps
// and slices, handling both musicResponsiveListItemRenderer (list rows) and
// musicTwoRowItemRenderer / playlistPanelVideoRenderer (carousel cards).
func collectBrowseTracks(node any, out *[]Track, seen map[string]bool) {
	switch v := node.(type) {
	case map[string]any:
		if r, ok := v["musicResponsiveListItemRenderer"].(map[string]any); ok {
			addTrack(extractTrack(r), out, seen)
		}
		if r, ok := v["musicTwoRowItemRenderer"].(map[string]any); ok {
			addTrack(extractTwoRowTrack(r), out, seen)
		}
		if r, ok := v["playlistPanelVideoRenderer"].(map[string]any); ok {
			addTrack(extractPanelTrack(r), out, seen)
		}
		for _, child := range v {
			collectBrowseTracks(child, out, seen)
		}
	case []any:
		for _, child := range v {
			collectBrowseTracks(child, out, seen)
		}
	}
}

// extractTwoRowTrack pulls a Track out of a musicTwoRowItemRenderer (carousel
// card). Only items whose navigation points at a watchEndpoint (i.e. a single
// song/video) are returned with an ID.
func extractTwoRowTrack(r map[string]any) Track {
	var t Track
	t.ID = str(dig(r, "navigationEndpoint", "watchEndpoint", "videoId"))

	titleRuns := digSlice(dig(r, "title", "runs"))
	if len(titleRuns) > 0 {
		t.Title = str(dig(titleRuns[0], "text"))
	}

	subRuns := digSlice(dig(r, "subtitle", "runs"))
	texts := extractTexts(subRuns)
	if len(texts) > 0 {
		t.Artist = strings.Join(texts, " ")
	}
	return t
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
	if byline == nil {
		byline = digSlice(dig(r, "shortBylineText", "runs"))
	}
	texts := extractTexts(byline)
	if len(texts) > 0 {
		t.Artist = texts[0]
	}

	durRuns := digSlice(dig(r, "lengthText", "runs"))
	if len(durRuns) > 0 {
		t.Duration = str(dig(durRuns[0], "text"))
	}
	return t
}

func addTrack(t Track, out *[]Track, seen map[string]bool) {
	if t.ID == "" || t.Title == "" || seen[t.ID] {
		return
	}
	seen[t.ID] = true
	*out = append(*out, t)
}

// Related returns songs related to the given video (for radio / autoplay).
//
// Uses the Innertube "next" endpoint to fetch the watch-playlist (radio) queue
// seeded from the given video, then parses the playlistPanelVideoRenderer items.
func (c *Client) Related(videoID string) ([]Track, error) {
	if videoID == "" {
		return nil, nil
	}

	payload := c.clientCtx()
	payload["videoId"] = videoID
	payload["playlistId"] = "RDAMVM" + videoID
	payload["isAudioOnly"] = true
	payload["params"] = "wAEB"

	body, err := c.post("next", payload)
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
	for _, item := range contents {
		r, ok := dig(item, "playlistPanelVideoRenderer").(map[string]any)
		if !ok {
			continue
		}
		t := extractPanelTrack(r)
		if t.ID == videoID { // skip the seed track
			continue
		}
		addTrack(t, &tracks, seen)
	}

	if len(tracks) > 50 {
		tracks = tracks[:50]
	}
	return CleanTracks(tracks), nil
}
