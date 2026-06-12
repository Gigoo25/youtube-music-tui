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

	res.Name = sanitizeDisplay(artistName(broot))
	if res.Name == "" {
		res.Name = name
	}
	res.Songs = CleanTracks(parseArtistSongs(broot))
	res.Albums = parseArtistAlbums(broot, 20)

	// The landing page shows only a curated subset of releases (~10 album cards
	// for big artists). The full discography lives behind the shelf's "More"
	// button — a MUSIC_PAGE_TYPE_ARTIST_DISCOGRAPHY browse. Merge it in (landing
	// order first, so the curated albums stay on top). Note: YouTube Music's
	// artist catalog itself omits some editions (deluxe/reissues are often
	// search-only), so this is best-effort completeness, not a guarantee.
	if discoID := discographyBrowseID(broot); discoID != "" {
		dp := c.clientCtx()
		dp["browseId"] = discoID
		if dbody, derr := c.post("browse", dp); derr == nil {
			if droot, perr := parseJSON(dbody); perr == nil {
				res.Albums = mergeAlbumRefs(res.Albums, parseArtistAlbums(droot, 60))
			}
		}
	}
	return res, nil
}

// discographyBrowseID finds the artist's full-discography browse id (MPAD…)
// from the albums shelf's "More" endpoint, or "" when the page has none.
func discographyBrowseID(node any) string {
	switch v := node.(type) {
	case map[string]any:
		if be, ok := v["browseEndpoint"].(map[string]any); ok {
			pageType := str(dig(be, "browseEndpointContextSupportedConfigs",
				"browseEndpointContextMusicConfig", "pageType"))
			if id := str(be["browseId"]); pageType == "MUSIC_PAGE_TYPE_ARTIST_DISCOGRAPHY" &&
				strings.HasPrefix(id, "MPAD") {
				return id
			}
		}
		for _, child := range v {
			if id := discographyBrowseID(child); id != "" {
				return id
			}
		}
	case []any:
		for _, child := range v {
			if id := discographyBrowseID(child); id != "" {
				return id
			}
		}
	}
	return ""
}

// mergeAlbumRefs appends extra albums not already present in base (by id).
func mergeAlbumRefs(base, extra []AlbumRef) []AlbumRef {
	seen := make(map[string]bool, len(base))
	for _, a := range base {
		seen[a.ID] = true
	}
	for _, a := range extra {
		if !seen[a.ID] {
			seen[a.ID] = true
			base = append(base, a)
		}
	}
	return base
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
// cleanArtist) and removes invisible width-distorting characters from all display
// fields (see sanitizeDisplay). Applied to each track list the API returns so no
// view renders "Title • &" and rows don't wrap from miscounted widths.
func CleanTracks(ts []Track) []Track {
	for i := range ts {
		ts[i].Title = sanitizeDisplay(ts[i].Title)
		ts[i].Artist = sanitizeDisplay(cleanArtist(ts[i].Artist))
		// cleanArtist also strips lone separator artifacts ("&", "feat") that leak
		// into the album field, so rows don't render "Title • Artist • &".
		ts[i].Album = sanitizeDisplay(cleanArtist(ts[i].Album))
		ts[i].Duration = sanitizeDisplay(ts[i].Duration)
		ts[i].Year = sanitizeDisplay(ts[i].Year)
	}
	return ts
}

// sanitizeDisplay removes characters that render with an unpredictable terminal
// width (so display-width math diverges from the real terminal and rows wrap,
// corrupting the layout): control chars, zero-width marks, emoji variation
// selectors, and bidirectional formatting. Visible letters/emoji are kept.
// Stripping C0/C1 controls also keeps YouTube-supplied text from injecting raw
// terminal escape sequences (ESC/CSI/OSC).
func sanitizeDisplay(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			out = append(out, ' ')
		case r < 0x20 || r == 0x7f: // C0 controls + DEL
			continue
		case r >= 0x80 && r <= 0x9f: // C1 controls
			continue
		case r >= 0x200b && r <= 0x200f: // zero-width + LRM/RLM
			continue
		case r >= 0x202a && r <= 0x202e: // bidi embeddings/overrides
			continue
		case r >= 0x2060 && r <= 0x2064: // word joiner + invisible ops
			continue
		case r >= 0x2066 && r <= 0x2069: // bidi isolates
			continue
		case r >= 0xfe00 && r <= 0xfe0f: // variation selectors
			continue
		case r == 0xfeff: // BOM / zero-width no-break space
			continue
		default:
			out = append(out, r)
		}
	}
	return strings.TrimSpace(string(out))
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

// parseArtistAlbums extracts the artist's album/single cards (capped at limit),
// deduped by browse id. Cards that don't point at an album (e.g. related-artist
// channels, playable videos) are skipped because their browse id lacks the
// MPREb prefix.
func parseArtistAlbums(root any, limit int) []AlbumRef {
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
			a.Title = sanitizeDisplay(str(dig(runs[0], "text")))
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
