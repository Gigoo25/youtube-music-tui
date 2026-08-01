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

	res.Name = sanitizeDisplay(headerTitle(broot, "musicImmersiveHeaderRenderer",
		"musicResponsiveHeaderRenderer", "musicVisualHeaderRenderer", "musicDetailHeaderRenderer"))
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
	res.Albums = c.withDiscography(res.Albums, broot)
	return res, nil
}

// discographyBrowseID finds the artist's full-discography browse id (MPAD…)
// from the albums shelf's "More" endpoint, or "" when the page has none.
func discographyBrowseID(node any) string {
	return findBrowseID(node, func(be map[string]any) bool {
		return browsePageType(be) == "MUSIC_PAGE_TYPE_ARTIST_DISCOGRAPHY" &&
			strings.HasPrefix(str(be["browseId"]), "MPAD")
	})
}

// browsePageType reads the music page type a browseEndpoint points at.
func browsePageType(be map[string]any) string {
	return str(dig(be, "browseEndpointContextSupportedConfigs",
		"browseEndpointContextMusicConfig", "pageType"))
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

// withDiscography merges the artist's full discography (the releases behind the
// albums shelf's "More" button) into base, keeping base's order first.
// Best-effort: a failed browse leaves base unchanged.
func (c *Client) withDiscography(base []AlbumRef, root any) []AlbumRef {
	discoID := discographyBrowseID(root)
	if discoID == "" {
		return base
	}
	dp := c.clientCtx()
	dp["browseId"] = discoID
	dbody, err := c.post("browse", dp)
	if err != nil {
		return base
	}
	droot, err := parseJSON(dbody)
	if err != nil {
		return base
	}
	return mergeAlbumRefs(base, parseArtistAlbums(droot, 60))
}

// firstArtistBrowseID finds the first browseEndpoint whose page type marks it as
// an artist channel.
func firstArtistBrowseID(node any) string {
	return findBrowseID(node, func(be map[string]any) bool {
		return browsePageType(be) == "MUSIC_PAGE_TYPE_ARTIST" && str(be["browseId"]) != ""
	})
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
		addTrack(extractTrack(r), &out, seen)
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
		// Subtitle is "Album • 2021" / "Single • 2019", or "Artist • 2021" on
		// discography pages: pull the year, plus the artist when the leading
		// group is neither a year nor the release-type word.
		groups := bylineGroups(digSlice(dig(r, "subtitle", "runs")))
		for _, g := range groups {
			if isYear(g) {
				a.Year = g
			}
		}
		if len(groups) > 0 {
			g := groups[0]
			if !isYear(g) && g != "Album" && g != "Single" && g != "EP" {
				a.Artist = sanitizeDisplay(g)
			}
		}
		seen[id] = true
		out = append(out, a)
	})
	return out
}
