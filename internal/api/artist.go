package api

import (
	"encoding/json"
	"strings"
)

// ArtistByQuery finds the artist that best matches name and returns their
// landing page: a capped list of top songs plus their albums/singles. Like
// AlbumByQuery it works in two steps — (1) search to discover the artist's
// browse id (a channel "UC…" id whose page type is ARTIST), then (2) browse
// that page and parse its shelves.
func ArtistByQuery(c *Client, name string) (ArtistResult, error) {
	var res ArtistResult
	name = strings.TrimSpace(name)
	if name == "" {
		return res, nil
	}

	sp := c.clientCtx()
	sp["query"] = name
	sbody, err := c.post("search", sp)
	if err != nil {
		return res, err
	}
	var sroot map[string]any
	if err := json.Unmarshal(sbody, &sroot); err != nil {
		return res, err
	}

	browseID := firstArtistBrowseID(sroot)
	if browseID == "" {
		return res, nil
	}

	bp := c.clientCtx()
	bp["browseId"] = browseID
	bbody, err := c.post("browse", bp)
	if err != nil {
		return res, err
	}
	var broot map[string]any
	if err := json.Unmarshal(bbody, &broot); err != nil {
		return res, err
	}

	res.Name = artistName(broot)
	if res.Name == "" {
		res.Name = name
	}
	res.Songs = CleanTracks(parseArtistSongs(broot))
	res.Albums = parseArtistAlbums(broot)
	return res, nil
}

// firstArtistBrowseID finds the first browseEndpoint whose page type marks it as
// an artist channel.
func firstArtistBrowseID(node any) string {
	switch v := node.(type) {
	case map[string]any:
		if be, ok := v["browseEndpoint"].(map[string]any); ok {
			pageType := str(dig(be, "browseEndpointContextSupportedConfigs",
				"browseEndpointContextMusicConfig", "pageType"))
			if pageType == "MUSIC_PAGE_TYPE_ARTIST" {
				if id := str(be["browseId"]); id != "" {
					return id
				}
			}
		}
		for _, child := range v {
			if id := firstArtistBrowseID(child); id != "" {
				return id
			}
		}
	case []any:
		for _, child := range v {
			if id := firstArtistBrowseID(child); id != "" {
				return id
			}
		}
	}
	return ""
}

// artistName reads the artist's display name from whichever header renderer the
// browse response uses.
func artistName(root any) string {
	for _, key := range []string{"musicImmersiveHeaderRenderer", "musicResponsiveHeaderRenderer", "musicVisualHeaderRenderer", "musicDetailHeaderRenderer"} {
		if h := findRenderer(root, key); h != nil {
			runs := digSlice(dig(h, "title", "runs"))
			if len(runs) > 0 {
				if t := str(dig(runs[0], "text")); t != "" {
					return t
				}
			}
		}
	}
	return ""
}

// parseArtistSongs extracts the artist's top song rows (cap 20), deduped by id.
func parseArtistSongs(root any) []Track {
	const limit = 20
	var out []Track
	seen := map[string]bool{}
	walkRenderers(root, "musicResponsiveListItemRenderer", func(r map[string]any) {
		if len(out) >= limit {
			return
		}
		t := extractTrack(r)
		if t.ID == "" || t.Title == "" || seen[t.ID] {
			return
		}
		seen[t.ID] = true
		out = append(out, t)
	})
	return out
}

// CleanTracks strips byline separator artifacts from every track's artist (see
// cleanArtist). Applied to each track list the API returns so no view renders
// "Title • &".
func CleanTracks(ts []Track) []Track {
	for i := range ts {
		ts[i].Artist = cleanArtist(ts[i].Artist)
	}
	return ts
}

// cleanArtist removes byline artifacts left by multi-artist parsing — e.g. a
// collaboration row whose artist comes back as a lone separator like "&" — so
// rows don't render "Title • &". A genuine "A & B" is left intact.
func cleanArtist(s string) string {
	s = strings.Trim(strings.TrimSpace(s), " &,·•-")
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "feat", "feat.", "and", "x", "&amp;":
		return ""
	}
	return strings.TrimSpace(s)
}

// parseArtistAlbums extracts the artist's album/single cards (cap 20), deduped by
// browse id. Cards that don't point at an album (e.g. related-artist channels,
// playable videos) are skipped because their browse id lacks the MPREb prefix.
func parseArtistAlbums(root any) []AlbumRef {
	const limit = 20
	var out []AlbumRef
	seen := map[string]bool{}
	walkRenderers(root, "musicTwoRowItemRenderer", func(r map[string]any) {
		if len(out) >= limit {
			return
		}
		id := str(dig(r, "navigationEndpoint", "browseEndpoint", "browseId"))
		if !strings.HasPrefix(id, "MPREb") || seen[id] {
			return
		}
		var a AlbumRef
		a.ID = id
		if runs := digSlice(dig(r, "title", "runs")); len(runs) > 0 {
			a.Title = str(dig(runs[0], "text"))
		}
		if a.Title == "" {
			return
		}
		// Subtitle looks like "Album • 2021" or "Single • 2019"; pull the year.
		for _, t := range extractTexts(digSlice(dig(r, "subtitle", "runs"))) {
			if len(t) == 4 && t >= "1900" && t <= "2099" {
				a.Year = t
			}
		}
		seen[id] = true
		out = append(out, a)
	})
	return out
}
