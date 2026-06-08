package api

// Explore returns a feed of explore/mood/genre songs from YouTube Music.
//
// STUB: implemented by a follow-up agent via the Innertube browse endpoint
// (explore browseId / mood categories). Returns empty until then.
func (c *Client) Explore() ([]Track, error) {
	return nil, nil
}

// Related returns songs related to the given video (for radio / autoplay).
//
// STUB: implemented by a follow-up agent via the Innertube "next" endpoint
// (watch playlist / radio). Returns empty until then.
func (c *Client) Related(videoID string) ([]Track, error) {
	return nil, nil
}
