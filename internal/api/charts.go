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

	tracks := collectListItemTracks(root)

	// Find a chart playlist (e.g. "Trending", "Top songs") and pull its songs.
	if pid := findChartPlaylist(root); pid != "" {
		pp := c.clientCtx()
		pp["browseId"] = pid
		if pbody, perr := c.post("browse", pp); perr == nil {
			if proot, perr := parseJSON(pbody); perr == nil {
				tracks = append(tracks, collectListItemTracks(proot)...)
			}
		}
	}

	// Fallback: also include playable carousel items (music videos).
	tracks = append(tracks, collectTwoRowTracks(root)...)

	return dedupeTracks(tracks), nil
}

// NewReleases returns newly released songs from YouTube Music via the Innertube
// browse endpoint (browseId FEmusic_new_releases). The shelf is built from
// musicTwoRowItemRenderer cards; only the playable ones (with a watch endpoint)
// become tracks.
func (c *Client) NewReleases() ([]Track, error) {
	p := c.clientCtx()
	p["browseId"] = "FEmusic_new_releases"

	body, err := c.post("browse", p)
	if err != nil {
		return nil, err
	}

	root, err := parseJSON(body)
	if err != nil {
		return nil, err
	}

	tracks := collectTwoRowTracks(root)
	tracks = append(tracks, collectListItemTracks(root)...)

	return dedupeTracks(tracks), nil
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
	walkRenderers(node, "musicResponsiveListItemRenderer", func(r map[string]any) {
		t := extractTrack(r)
		if t.ID != "" && t.Title != "" {
			out = append(out, t)
		}
	})
	return out
}

// collectTwoRowTracks walks the response for every musicTwoRowItemRenderer and
// extracts the ones that point at a playable video.
func collectTwoRowTracks(node any) []Track {
	var out []Track
	walkRenderers(node, "musicTwoRowItemRenderer", func(r map[string]any) {
		t := extractTwoRowTrack(r)
		if t.ID != "" && t.Title != "" {
			out = append(out, t)
		}
	})
	return out
}

// findChartPlaylist scans carousel cards for a chart playlist browseId, favouring
// titles that look like a trending / top-songs chart.
func findChartPlaylist(node any) string {
	var fallback string
	var picked string

	walkRenderers(node, "musicTwoRowItemRenderer", func(r map[string]any) {
		if picked != "" {
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

		if fallback == "" {
			fallback = browseID
		}

		title := ""
		if runs := digSlice(dig(r, "title", "runs")); len(runs) > 0 {
			title = strings.ToLower(str(dig(runs[0], "text")))
		}
		if strings.Contains(title, "trending") || strings.Contains(title, "top song") ||
			strings.Contains(title, "top 100") {
			picked = browseID
		}
	})

	if picked != "" {
		return picked
	}
	return fallback
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
