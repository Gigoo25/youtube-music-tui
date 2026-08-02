package api

import (
	"encoding/json"
	"strings"
	"sync"
)

// albumsFilterParam restricts a search to the "Albums" tab (the songs filter
// param with its filter byte flipped, same scheme ytmusicapi uses).
const albumsFilterParam = "EgWKAQIYAWoMEAMQBBAJEAUQChAV"

// AlbumByQuery finds the album that best matches the given name/artist and
// returns its full track list. Search ranking is unreliable here — it can rank
// a different album by the same artist first, or (for some albums) not return
// the wanted one at all — so the id is resolved in fallback steps, each
// requiring a *title match* against the requested album: (1) general search,
// (2) Albums-filtered search, (3) the artist's full discography. Only when no
// title matches anywhere does it fall back to search's first album hit.
func AlbumByQuery(c *Client, album, artist string) ([]Track, string, error) {
	query := strings.TrimSpace(album + " " + artist)
	if query == "" {
		return nil, "", nil
	}

	sroot, err := c.searchRaw(query, "")
	if err != nil {
		return nil, "", err
	}
	cands := albumSearchCandidates(sroot)
	if id, ok := pickAlbumCandidate(cands, album); ok {
		return c.albumByID(id, artist)
	}

	if froot, ferr := c.searchRaw(query, albumsFilterParam); ferr == nil {
		if id, ok := pickAlbumCandidate(albumSearchCandidates(froot), album); ok {
			return c.albumByID(id, artist)
		}
	}

	if artist != "" {
		if id := c.albumIDFromDiscography(album, artist); id != "" {
			return c.albumByID(id, artist)
		}
	}

	// No title match anywhere: take search's best guess (old behaviour).
	if len(cands) > 0 {
		return c.albumByID(cands[0].id, artist)
	}
	if id := findBrowseID(sroot, func(be map[string]any) bool {
		return strings.HasPrefix(str(be["browseId"]), "MPREb")
	}); id != "" {
		return c.albumByID(id, artist)
	}
	return nil, "", nil
}

// searchRaw runs a search (optionally filtered via params) and returns the
// decoded response.
func (c *Client) searchRaw(query, params string) (map[string]any, error) {
	sp := c.clientCtx()
	sp["query"] = query
	if params != "" {
		sp["params"] = params
	}
	body, err := c.post("search", sp)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	return root, nil
}

// albumIDFromDiscography resolves an album browse id by title-matching against
// the artist's full discography — the reliable path when search won't surface
// the album (it happens) but the artist page lists it.
func (c *Client) albumIDFromDiscography(album, artist string) string {
	sroot, err := c.searchRaw(artist, "")
	if err != nil {
		return ""
	}
	artistID := firstArtistBrowseID(sroot)
	if artistID == "" {
		return ""
	}
	bp := c.clientCtx()
	bp["browseId"] = artistID
	bbody, err := c.post("browse", bp)
	if err != nil {
		return ""
	}
	broot, err := parseJSON(bbody)
	if err != nil {
		return ""
	}
	albums := c.withDiscography(parseArtistAlbums(broot, 60), broot)
	cands := make([]albumCandidate, 0, len(albums))
	for _, a := range albums {
		cands = append(cands, albumCandidate{id: a.ID, title: a.Title})
	}
	if id, ok := pickAlbumCandidate(cands, album); ok {
		return id
	}
	return ""
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

	title := sanitizeDisplay(headerTitle(broot, "musicDetailHeaderRenderer", "musicResponsiveHeaderRenderer"))
	if title == "" {
		title = "Album"
	}
	albumArtist := albumHeaderArtist(broot)
	if albumArtist == "" {
		albumArtist = fallbackArtist
	}
	tracks, videoRows := parseAlbumTracks(broot, albumArtist)
	tracks = CleanTracks(tracks)
	tracks = c.remapVideoRows(tracks, videoRows, title)
	for i := range tracks {
		tracks[i].AlbumID = browseID // every row belongs to this album
	}
	return tracks, title, nil
}

// remapVideoRows swaps album rows whose id points at a music video (OMV/UGC)
// for the official song (ATV) found via a Songs-tab search. Some labels attach
// the music video to album pages even though an audio version exists — the row
// then shows the song's duration (e.g. 3:12) but plays the longer video. Rows
// with no findable song version keep the video id (its audio still plays).
// Lookups run a few at a time; a failed search just leaves that row unchanged.
// Returns the rows, with any duplicate id a remap introduced dropped.
func (c *Client) remapVideoRows(tracks []Track, rows []int, albumTitle string) []Track {
	if len(rows) == 0 {
		return tracks
	}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, idx := range rows {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			t := tracks[i]
			songs, err := c.SearchSongs(t.Title + " " + t.Artist)
			if err != nil {
				return
			}
			for _, s := range songs {
				if !strings.EqualFold(s.Title, t.Title) {
					continue
				}
				// Same album, or at least the same runtime as the album row —
				// two empty durations match each other but prove nothing.
				sameRuntime := t.Duration != "" && s.Duration == t.Duration
				if !strings.EqualFold(s.Album, albumTitle) && !sameRuntime {
					continue
				}
				tracks[i].ID = s.ID
				if s.Duration != "" {
					tracks[i].Duration = s.Duration
				}
				return
			}
		}(idx)
	}
	wg.Wait()

	// A remap can land on an id another row already holds (the same song listed
	// twice on the page); drop the duplicate so the album can't play it twice.
	out := make([]Track, 0, len(tracks))
	seen := make(map[string]bool, len(tracks))
	for _, t := range tracks {
		addTrack(t, &out, seen)
	}
	return out
}

// listItemVideoType reads the musicVideoType a list row's title links to ("" if
// the row doesn't carry one).
func listItemVideoType(r map[string]any) string {
	runs := flexRuns(r, 0)
	if len(runs) == 0 {
		return ""
	}
	if run, ok := runs[0].(map[string]any); ok {
		return rendererVideoType(run)
	}
	return ""
}

// albumCandidate is an album search hit: browse id + display title.
type albumCandidate struct{ id, title string }

// albumSearchCandidates collects every album (MPREb) hit in a search response
// together with its display title, from both result rows and the top-result
// card. Song rows are excluded naturally: they navigate via a watchEndpoint,
// not a root-level album browseEndpoint.
func albumSearchCandidates(root any) []albumCandidate {
	var out []albumCandidate
	walkRenderersMulti(root, map[string]func(map[string]any){
		"musicResponsiveListItemRenderer": func(r map[string]any) {
			id := str(dig(r, "navigationEndpoint", "browseEndpoint", "browseId"))
			if !strings.HasPrefix(id, "MPREb") {
				return
			}
			runs := flexRuns(r, 0)
			if len(runs) == 0 {
				return
			}
			out = append(out, albumCandidate{id: id, title: str(dig(runs[0], "text"))})
		},
		"musicCardShelfRenderer": func(r map[string]any) {
			runs := digSlice(dig(r, "title", "runs"))
			if len(runs) == 0 {
				return
			}
			id := str(dig(runs[0], "navigationEndpoint", "browseEndpoint", "browseId"))
			if strings.HasPrefix(id, "MPREb") {
				out = append(out, albumCandidate{id: id, title: str(dig(runs[0], "text"))})
			}
		},
	})
	return out
}

// pickAlbumCandidate chooses the candidate whose title matches the wanted album
// name, in tiers: exact (case-insensitive), then candidate-prefix (covers
// "… (Deluxe Edition)" suffixes), then substring, then — last resort — the
// wanted name prefixing the candidate. ok is false when nothing matches — the
// caller decides what to fall back to.
func pickAlbumCandidate(cands []albumCandidate, want string) (id string, ok bool) {
	w := strings.ToLower(strings.TrimSpace(want))
	if w == "" || len(cands) == 0 {
		return "", false
	}
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	for _, c := range cands {
		if norm(c.title) == w {
			return c.id, true
		}
	}
	// Candidate extends the wanted name ("X (Deluxe Edition)" for "X" is fine).
	for _, c := range cands {
		if strings.HasPrefix(norm(c.title), w) {
			return c.id, true
		}
	}
	// Wanted name appears somewhere inside the candidate.
	for _, c := range cands {
		if strings.Contains(norm(c.title), w) {
			return c.id, true
		}
	}
	// Wanted name extends the candidate — last resort only: it covers an album
	// field that has the artist glued on, but would otherwise wrongly map
	// "X (Deluxe Edition)" onto plain "X".
	for _, c := range cands {
		if strings.HasPrefix(w, norm(c.title)) {
			return c.id, true
		}
	}
	return "", false
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
				if isYear(t) {
					continue
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
// videoRows lists the indices whose id links a music video rather than the song
// (candidates for remapVideoRows).
func parseAlbumTracks(root any, albumArtist string) (out []Track, videoRows []int) {
	seen := map[string]bool{}
	walkRenderers(root, "musicResponsiveListItemRenderer", func(r map[string]any) {
		t := extractTrack(r)
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
		row := len(out)
		addTrack(t, &out, seen)
		if len(out) == row {
			return // no id/title, or a row already seen
		}
		if vt := listItemVideoType(r); vt != "" && vt != videoTypeATV {
			videoRows = append(videoRows, row)
		}
	})
	return out, videoRows
}

// headerTitle pulls a page's display name from the title of whichever of the
// given header renderers the browse response uses (the layout has changed
// across API versions). "" when none of them carries a title.
func headerTitle(root any, keys ...string) string {
	for _, key := range keys {
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

// findRenderer returns the first object stored under the given key.
func findRenderer(node any, key string) map[string]any {
	var out map[string]any
	walkFirst(node, func(m map[string]any) bool {
		r, ok := m[key].(map[string]any)
		if ok {
			out = r
		}
		return ok
	})
	return out
}

// findBrowseID returns the browseId of the first browseEndpoint object
// satisfying want ("" when none does).
func findBrowseID(node any, want func(be map[string]any) bool) string {
	var id string
	walkFirst(node, func(m map[string]any) bool {
		be, ok := m["browseEndpoint"].(map[string]any)
		if !ok || !want(be) {
			return false
		}
		id = str(be["browseId"])
		return id != ""
	})
	return id
}
