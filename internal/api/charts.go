package api

import (
	"encoding/json"
	"strings"
)

// Trending returns the current trending/chart songs from YouTube Music via the
// Innertube browse endpoint (browseId FEmusic_charts).
//
// The charts page itself surfaces artist and playlist charts rather than a flat
// song list, so we (a) collect any song rows that appear directly and (b) drill
// into the first "trending"/"top songs" chart playlist and parse its tracks.
func (c *Client) Trending() ([]Track, error) {
	p := c.clientCtx()
	p["browseId"] = "FEmusic_charts"

	body, err := c.post("browse", p)
	if err != nil {
		return nil, err
	}

	root, err := parseJSON(body)
	if err != nil {
		return nil, err
	}

	// Single traversal of the (large) response: collect direct song rows and
	// carousel items, and detect a chart playlist, all in one pass instead of
	// three. Results are concatenated below in the original priority order
	// (list rows → chart playlist → carousel) so dedupe/cap behaviour is
	// unchanged.
	var listTracks, twoRowTracks []Track
	var picked, fallback string
	walkRenderersMulti(root, map[string]func(map[string]any){
		"musicResponsiveListItemRenderer": func(r map[string]any) {
			if t := extractTrack(r); t.ID != "" && t.Title != "" {
				listTracks = append(listTracks, t)
			}
		},
		"musicTwoRowItemRenderer": func(r map[string]any) {
			if t := extractTwoRowTrack(r); t.ID != "" && t.Title != "" {
				twoRowTracks = append(twoRowTracks, t)
			}
			considerChartPlaylist(r, &picked, &fallback)
		},
	})

	tracks := listTracks

	// Drill into the chart playlist (e.g. "Trending", "Top songs") for its songs.
	pid := picked
	if pid == "" {
		pid = fallback
	}
	if pid != "" {
		pp := c.clientCtx()
		pp["browseId"] = pid
		if pbody, perr := c.post("browse", pp); perr == nil {
			if proot, perr := parseJSON(pbody); perr == nil {
				tracks = append(tracks, collectListItemTracks(proot)...)
			}
		}
	}

	// Fallback: also include playable carousel items (music videos).
	tracks = append(tracks, twoRowTracks...)

	return CleanTracks(dedupeTracks(tracks)), nil
}

// parseJSON unmarshals an Innertube response body into a generic map.
func parseJSON(body []byte) (map[string]any, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	return root, nil
}

// collectListItemTracks walks the response for every
// musicResponsiveListItemRenderer and extracts valid song tracks.
func collectListItemTracks(node any) []Track {
	var out []Track
	seen := map[string]bool{}
	walkRenderers(node, "musicResponsiveListItemRenderer", func(r map[string]any) {
		addTrack(extractTrack(r), &out, seen)
	})
	return out
}

// considerChartPlaylist inspects a single musicTwoRowItemRenderer for a chart
// playlist browseId, updating *picked (a title that looks like a trending /
// top-songs chart) or *fallback (the first playlist seen). Called per renderer
// during the Trending traversal.
func considerChartPlaylist(r map[string]any, picked, fallback *string) {
	if *picked != "" {
		return
	}
	browseID := str(dig(r, "navigationEndpoint", "browseEndpoint", "browseId"))
	// VL prefix marks a playlist browse target.
	if !strings.HasPrefix(browseID, "VL") {
		return
	}
	pageType := str(dig(r, "navigationEndpoint", "browseEndpoint",
		"browseEndpointContextSupportedConfigs",
		"browseEndpointContextMusicConfig", "pageType"))
	if pageType != "" && pageType != "MUSIC_PAGE_TYPE_PLAYLIST" {
		return
	}

	if *fallback == "" {
		*fallback = browseID
	}

	title := ""
	if runs := digSlice(dig(r, "title", "runs")); len(runs) > 0 {
		title = strings.ToLower(str(dig(runs[0], "text")))
	}
	if strings.Contains(title, "trending") || strings.Contains(title, "top song") ||
		strings.Contains(title, "top 100") {
		*picked = browseID
	}
}

// walkRenderers recursively descends the decoded JSON and invokes fn for every
// object found under the given renderer key.
func walkRenderers(node any, key string, fn func(map[string]any)) {
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			if k == key {
				if m, ok := child.(map[string]any); ok {
					fn(m)
				}
			}
			walkRenderers(child, key, fn)
		}
	case []any:
		for _, child := range v {
			walkRenderers(child, key, fn)
		}
	}
}

// walkRenderersMulti is walkRenderers for several renderer keys at once: it
// descends the decoded JSON a single time and invokes the matching handler for
// each key it encounters. Avoids re-walking a large response once per key.
func walkRenderersMulti(node any, handlers map[string]func(map[string]any)) {
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			if fn, ok := handlers[k]; ok {
				if m, ok := child.(map[string]any); ok {
					fn(m)
				}
			}
			walkRenderersMulti(child, handlers)
		}
	case []any:
		for _, child := range v {
			walkRenderersMulti(child, handlers)
		}
	}
}

// dedupeTracks removes duplicate tracks by ID, keeps only valid ones, and caps
// the result at 50 entries.
func dedupeTracks(in []Track) []Track {
	const limit = 50
	seen := make(map[string]bool, len(in))
	out := make([]Track, 0, len(in))
	for _, t := range in {
		if t.ID == "" || t.Title == "" || seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		out = append(out, t)
		if len(out) >= limit {
			break
		}
	}
	return out
}
