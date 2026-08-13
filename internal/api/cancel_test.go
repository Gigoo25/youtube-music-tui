package api

import (
	"context"
	"errors"
	"testing"
)

// TestCancelledContextAbortsRequest proves the ctx reaches the HTTP request: a
// call whose ctx is already cancelled must fail immediately instead of dialling
// out and holding the socket for RequestTimeout. Every exported entry point is
// checked because each threads its own call chain down to post.
func TestCancelledContextAbortsRequest(t *testing.T) {
	c := NewClient()
	c.SetTimeout(RequestTimeout)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := map[string]func() error{
		"Search":          func() error { _, err := c.Search(ctx, "q"); return err },
		"SearchSongs":     func() error { _, err := c.SearchSongs(ctx, "q"); return err },
		"SearchSongsPage": func() error { _, _, err := c.SearchSongsPage(ctx, "q", ""); return err },
		"Trending":        func() error { _, err := c.Trending(ctx); return err },
		"Related":         func() error { _, err := c.Related(ctx, "vid"); return err },
		"AlbumByID":       func() error { _, _, err := AlbumByID(ctx, c, "MPREb1"); return err },
		"AlbumByQuery":    func() error { _, _, err := AlbumByQuery(ctx, c, "album", "artist"); return err },
		"ArtistByQuery":   func() error { _, err := ArtistByQuery(ctx, c, "artist"); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			// net/http wraps the cause in a *url.Error, so match with errors.Is.
			if err := call(); !errors.Is(err, context.Canceled) {
				t.Fatalf("%s with a cancelled ctx: err = %v, want context.Canceled", name, err)
			}
		})
	}
}
