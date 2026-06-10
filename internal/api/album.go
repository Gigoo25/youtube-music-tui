package api

import (
	"encoding/json"
	"strings"
)

// AlbumByQuery finds the album that best matches the given name/artist and
// returns its full track list. Because search-result rows don't expose an album
// browseId without deeper parsing, we (1) run a general search, (2) grab the
// first album browseId (MPREb…) it surfaces, and (3) browse that album.
func AlbumByQuery(c *Client, album, artist string) ([]Track, string, error) {
	query := strings.TrimSpace(album + " " + artist)
	if query == "" {
		return nil, "", nil
	}

	// Step 1+2: search and find an album browseId.
	sp := c.clientCtx()
	sp["query"] = query
	sbody, err := c.post("search", sp)
	if err != nil {
		return nil, "", err
	}
	var sroot map[string]any
	if err := json.Unmarshal(sbody, &sroot); err != nil {
		return nil, "", err
	}
	browseID := firstBrowseIDWithPrefix(sroot, "MPREb")
	if browseID == "" {
		return nil, "", nil
	}

	// Step 3: browse the album by its id, falling back to the search-supplied
	// artist when the album header doesn't name one.
	return c.albumByID(browseID, artist)
}

// AlbumByID browses an album directly by its browse id (MPREb…) — used when the
// id is already known (e.g. an album card on the artist page).
func AlbumByID(c *Client, browseID string) ([]Track, string, error) {
	return c.albumByID(browseID, "")
}

func (c *Client) albumByID(browseID, fallbackArtist string) ([]Track, string, error) {
	bp := c.clientCtx()
	bp["browseId"] = browseID
	bbody, err := c.post("browse", bp)
	if err != nil {
		return nil, "", err
	}
	var broot map[string]any
	if err := json.Unmarshal(bbody, &broot); err != nil {
		return nil, "", err
	}

	title := sanitizeDisplay(albumTitle(broot))
	albumArtist := albumHeaderArtist(broot)
	if albumArtist == "" {
		albumArtist = fallbackArtist
	}
	tracks := CleanTracks(parseAlbumTracks(broot, albumArtist))
	return tracks, title, nil
}

// albumHeaderArtist reads the album's primary artist from the header subtitle
// (typically "Album • Artist • Year").
func albumHeaderArtist(root any) string {
	for _, key := range []string{"musicDetailHeaderRenderer", "musicResponsiveHeaderRenderer"} {
		h := findRenderer(root, key)
		if h == nil {
			continue
		}
		for _, field := range []string{"subtitle", "straplineTextOne"} {
			texts := extractTexts(digSlice(dig(h, field, "runs")))
			for _, t := range texts {
				if t == "Album" || t == "Single" || t == "EP" {
					continue
				}
				if len(t) == 4 && t >= "1900" && t <= "2099" {
					continue // a year
				}
				return t
			}
		}
	}
	return ""
}

// parseAlbumTracks extracts an album's track rows. Album pages keep the duration
// in fixedColumns and omit the per-row artist (it's the album artist), so we
// fill those in rather than relying on the generic search-row extractor.
func parseAlbumTracks(root any, albumArtist string) []Track {
	var out []Track
	seen := map[string]bool{}
	walkRenderers(root, "musicResponsiveListItemRenderer", func(r map[string]any) {
		t := extractTrack(r)
		if t.ID == "" || t.Title == "" || seen[t.ID] {
			return
		}
		if t.Duration == "" {
			fc := digSlice(dig(r, "fixedColumns"))
			if len(fc) > 0 {
				runs := digSlice(dig(fc[0], "musicResponsiveListItemFixedColumnRenderer", "text", "runs"))
				if len(runs) > 0 {
					t.Duration = str(dig(runs[0], "text"))
				}
			}
		}
		if t.Artist == "" {
			t.Artist = albumArtist
		}
		seen[t.ID] = true
		out = append(out, t)
	})
	return out
}

// albumTitle pulls the album's display name from whichever header renderer the
// browse response uses (the layout has changed across API versions).
func albumTitle(root any) string {
	for _, key := range []string{"musicDetailHeaderRenderer", "musicResponsiveHeaderRenderer"} {
		if h := findRenderer(root, key); h != nil {
			runs := digSlice(dig(h, "title", "runs"))
			if len(runs) > 0 {
				if t := str(dig(runs[0], "text")); t != "" {
					return t
				}
			}
		}
	}
	return "Album"
}

// findRenderer recursively returns the first object stored under the given key.
func findRenderer(node any, key string) map[string]any {
	switch v := node.(type) {
	case map[string]any:
		if r, ok := v[key].(map[string]any); ok {
			return r
		}
		for _, child := range v {
			if r := findRenderer(child, key); r != nil {
				return r
			}
		}
	case []any:
		for _, child := range v {
			if r := findRenderer(child, key); r != nil {
				return r
			}
		}
	}
	return nil
}

// firstBrowseIDWithPrefix recursively finds the first browseEndpoint browseId
// that starts with the given prefix (e.g. "MPREb" for albums).
func firstBrowseIDWithPrefix(node any, prefix string) string {
	switch v := node.(type) {
	case map[string]any:
		if be, ok := v["browseEndpoint"].(map[string]any); ok {
			if id, ok := be["browseId"].(string); ok && strings.HasPrefix(id, prefix) {
				return id
			}
		}
		for _, child := range v {
			if r := firstBrowseIDWithPrefix(child, prefix); r != "" {
				return r
			}
		}
	case []any:
		for _, child := range v {
			if r := firstBrowseIDWithPrefix(child, prefix); r != "" {
				return r
			}
		}
	}
	return ""
}
