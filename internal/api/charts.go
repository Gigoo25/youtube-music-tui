package api

// Trending returns the current trending/chart songs from YouTube Music.
//
// STUB: implemented by a follow-up agent via the Innertube browse endpoint
// (charts browseId). Returns empty until then.
func (c *Client) Trending() ([]Track, error) {
	return nil, nil
}

// NewReleases returns newly released songs/albums from YouTube Music.
//
// STUB: implemented by a follow-up agent via the Innertube browse endpoint
// (new releases browseId). Returns empty until then.
func (c *Client) NewReleases() ([]Track, error) {
	return nil, nil
}
