package config

import (
	"slices"
	"testing"

	"github.com/Gigoo25/youtube-music-tui/internal/api"
)

// TestIsFavoriteReturnsTrueForPresentID: IsFavorite must report accurately
// after a ToggleFavorite. A false negative would make the heart button vanish
// on a track the user just favorited — the most basic feature, broken.
func TestIsFavoriteReturnsTrueForPresentID(t *testing.T) {
	c := newTestConfig(t)

	c.ToggleFavorite(api.Track{ID: "abc", Title: "Song"})

	if !c.IsFavorite("abc") {
		t.Fatal("IsFavorite must be true after ToggleFavorite")
	}
}

// TestIsFavoriteReturnsFalseForAbsentID: a track that was never favorited must
// not report as favorite. A false positive would make the heart button show
// filled for a track the user hasn't touched.
func TestIsFavoriteReturnsFalseForAbsentID(t *testing.T) {
	c := newTestConfig(t)

	if c.IsFavorite("never-favorited") {
		t.Fatal("IsFavorite must be false for an unfavorited track")
	}
}

// TestToggleFavoriteIsInvolution: toggling the same track twice must restore
// the original state exactly (same length, same order). Without this, the
// favorites list would grow or shrink on its own after a toggle pair — the
// user's intent is "add then remove" = no change.
func TestToggleFavoriteIsInvolution(t *testing.T) {
	initial := []api.Track{
		{ID: "existing", Title: "Already Favorite"},
	}
	c := &Config{
		Favorites: slices.Clone(initial),
		path:      t.TempDir() + "/config.json",
	}
	c.rebuildFavSet()

	toggle := api.Track{ID: "new", Title: "New Track"}

	// First toggle: add.
	c.ToggleFavorite(toggle)
	if !c.IsFavorite("new") {
		t.Fatal("first toggle must add the track")
	}

	// Second toggle: remove. Must restore exact original state.
	c.ToggleFavorite(toggle)
	if c.IsFavorite("new") {
		t.Fatal("second toggle must remove the track")
	}
	if len(c.Favorites) != len(initial) {
		t.Fatalf("len(Favorites) = %d, want %d (involution)", len(c.Favorites), len(initial))
	}
	if len(c.Favorites) > 0 && c.Favorites[0].ID != "existing" {
		t.Fatalf("Favorites[0].ID = %q, want 'existing' (order must be preserved)", c.Favorites[0].ID)
	}
}

// TestToggleFavoriteReturnsTrueWhenAdding: ToggleFavorite returns true when it
// added the track, false when it removed it. The TUI uses the return value to
// flip the heart icon — without it, the icon would never change.
func TestToggleFavoriteReturnsTrueWhenAdding(t *testing.T) {
	c := newTestConfig(t)

	if !c.ToggleFavorite(api.Track{ID: "a", Title: "A"}) {
		t.Fatal("ToggleFavorite must return true when adding")
	}
	if c.ToggleFavorite(api.Track{ID: "a", Title: "A"}) {
		t.Fatal("ToggleFavorite must return false when removing")
	}
}

// TestSavePlaylistReplacesExisting: saving a playlist with a name that already
// exists must replace the old one, not append a duplicate. Without this, every
// save would grow the playlists list by one — the user's playlist picker would
// show duplicates after a single save.
func TestSavePlaylistReplacesExisting(t *testing.T) {
	c := newTestConfig(t)

	c.SavePlaylist("My Playlist", []api.Track{{ID: "a", Title: "A"}})
	c.SavePlaylist("My Playlist", []api.Track{{ID: "b", Title: "B"}})

	if len(c.Playlists) != 1 {
		t.Fatalf("len(Playlists) = %d, want 1 (save must replace)", len(c.Playlists))
	}
	if len(c.Playlists[0].Tracks) != 1 || c.Playlists[0].Tracks[0].ID != "b" {
		t.Fatal("playlist tracks must be replaced, not appended to")
	}
}

// TestSavePlaylistCopiesInput: the track slice passed to SavePlaylist must be
// copied. Without this, a later queue mutation would silently change the saved
// playlist — the user's saved data would drift from what they actually saved.
func TestSavePlaylistCopiesInput(t *testing.T) {
	c := newTestConfig(t)

	tracks := []api.Track{{ID: "a", Title: "A"}}
	c.SavePlaylist("P", tracks)

	// Mutate the original slice.
	tracks[0].Title = "MUTATED"

	if c.Playlists[0].Tracks[0].Title == "MUTATED" {
		t.Fatal("SavePlaylist must copy the input slice (mutation must not bleed in)")
	}
}

// TestDeletePlaylistRemovesNamed: DeletePlaylist must remove the named playlist.
// A no-op on a known name would leave stale playlists in the picker forever.
func TestDeletePlaylistRemovesNamed(t *testing.T) {
	c := newTestConfig(t)

	c.SavePlaylist("To Delete", []api.Track{{ID: "a", Title: "A"}})
	c.DeletePlaylist("To Delete")

	if p := c.PlaylistByName("To Delete"); p != nil {
		t.Fatal("DeletePlaylist must remove the named playlist")
	}
}

// TestDeletePlaylistNoOpOnAbsent: deleting a playlist that doesn't exist must
// not panic or change state. The TUI calls DeletePlaylist on every "delete"
// action, including from a picker that may not have the playlist loaded.
func TestDeletePlaylistNoOpOnAbsent(t *testing.T) {
	c := newTestConfig(t)

	c.SavePlaylist("Keep", []api.Track{{ID: "a", Title: "A"}})

	c.DeletePlaylist("Does Not Exist")

	if len(c.Playlists) != 1 {
		t.Fatalf("len(Playlists) = %d, want 1 (delete absent must be no-op)", len(c.Playlists))
	}
}

// TestAddToPlaylistCreatesOnAbsent: AddToPlaylist on a name that doesn't exist
// must create the playlist. Without this, the "add to playlist" action on a
// new name would silently drop the track.
func TestAddToPlaylistCreatesOnAbsent(t *testing.T) {
	c := newTestConfig(t)

	ok := c.AddToPlaylist("New Playlist", api.Track{ID: "a", Title: "A"})
	if !ok {
		t.Fatal("AddToPlaylist must return true when creating a new playlist")
	}
	p := c.PlaylistByName("New Playlist")
	if p == nil {
		t.Fatal("AddToPlaylist must create the playlist")
	}
	if len(p.Tracks) != 1 || p.Tracks[0].ID != "a" {
		t.Fatal("new playlist must contain the added track")
	}
}

// TestAddToPlaylistNoOpOnDuplicate: adding a track that's already in the
// playlist must be a no-op. Without this, rapid clicks on "add" would duplicate
// the track in the playlist.
func TestAddToPlaylistNoOpOnDuplicate(t *testing.T) {
	c := newTestConfig(t)

	c.AddToPlaylist("P", api.Track{ID: "a", Title: "A"})
	ok := c.AddToPlaylist("P", api.Track{ID: "a", Title: "A"})
	if ok {
		t.Fatal("AddToPlaylist must return false when track is already present")
	}
	if len(c.Playlists[0].Tracks) != 1 {
		t.Fatalf("len(tracks) = %d, want 1 (duplicate must be rejected)", len(c.Playlists[0].Tracks))
	}
}

// TestRemoveFromPlaylistNoOpOnAbsentIndex: removing a track at an index that
// doesn't exist must be a no-op — not a panic, not a silent wrong deletion.
// The TUI calls RemoveFromPlaylist with the cursor position, which can
// temporarily be out of range during list mutations.
func TestRemoveFromPlaylistNoOpOnAbsentIndex(t *testing.T) {
	c := newTestConfig(t)

	c.SavePlaylist("P", []api.Track{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}})

	// Out of range (positive).
	c.RemoveFromPlaylist("P", 99)
	if len(c.Playlists[0].Tracks) != 2 {
		t.Fatalf("len(tracks) = %d, want 2 (out-of-range positive index must be no-op)", len(c.Playlists[0].Tracks))
	}

	// Out of range (negative).
	c.RemoveFromPlaylist("P", -1)
	if len(c.Playlists[0].Tracks) != 2 {
		t.Fatalf("len(tracks) = %d, want 2 (negative index must be no-op)", len(c.Playlists[0].Tracks))
	}

	// Absent playlist name.
	c.RemoveFromPlaylist("Nope", 0)
	if len(c.Playlists) != 1 {
		t.Fatalf("len(playlists) = %d, want 1 (absent playlist name must be no-op)", len(c.Playlists))
	}
}

// TestRemoveFromPlaylistActuallyRemoves: removing a valid index must delete
// that track and preserve the others in order.
func TestRemoveFromPlaylistActuallyRemoves(t *testing.T) {
	c := newTestConfig(t)

	c.SavePlaylist("P", []api.Track{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}, {ID: "c", Title: "C"}})

	c.RemoveFromPlaylist("P", 1)

	if len(c.Playlists[0].Tracks) != 2 {
		t.Fatalf("len(tracks) = %d, want 2", len(c.Playlists[0].Tracks))
	}
	if c.Playlists[0].Tracks[0].ID != "a" || c.Playlists[0].Tracks[1].ID != "c" {
		t.Fatalf("tracks = [%s %s], want [a c]", c.Playlists[0].Tracks[0].ID, c.Playlists[0].Tracks[1].ID)
	}
}

// TestPlaylistByNameCaseSensitive: PlaylistByName must be case-sensitive.
// Reading the code: it does a direct string equality compare (==), which is
// case-sensitive in Go. Pinning this behavior prevents a future "fix" from
// silently making it case-insensitive and breaking the user's named playlists.
func TestPlaylistByNameCaseSensitive(t *testing.T) {
	c := newTestConfig(t)

	c.SavePlaylist("My Playlist", []api.Track{{ID: "a", Title: "A"}})

	if p := c.PlaylistByName("my playlist"); p != nil {
		t.Fatal("PlaylistByName must be case-sensitive (lowercase must not match)")
	}
	if p := c.PlaylistByName("My Playlist"); p == nil {
		t.Fatal("PlaylistByName must find the exact-case name")
	}
}

// TestPlaylistByNameReturnsNilWhenAbsent: PlaylistByName must return nil for
// a name that doesn't exist. The TUI checks for nil to show "playlist not
// found" — a non-nil return with zero tracks would render a blank view.
func TestPlaylistByNameReturnsNilWhenAbsent(t *testing.T) {
	c := newTestConfig(t)

	if p := c.PlaylistByName("Nope"); p != nil {
		t.Fatal("PlaylistByName must return nil for an absent name")
	}
}

// TestAddHistoryPrependsNewest: AddHistory must put the newest entry at the
// front. The TUI renders history newest-first — a reverse order would show
// the user their oldest plays at the top.
func TestAddHistoryPrependsNewest(t *testing.T) {
	c := newTestConfig(t)

	c.AddHistory(api.Track{ID: "old", Title: "Old"})
	c.AddHistory(api.Track{ID: "new", Title: "New"})

	if len(c.History) != 2 {
		t.Fatalf("len(History) = %d, want 2", len(c.History))
	}
	if c.History[0].Track.ID != "new" {
		t.Fatalf("History[0].ID = %q, want 'new' (newest first)", c.History[0].Track.ID)
	}
	if c.History[1].Track.ID != "old" {
		t.Fatalf("History[1].ID = %q, want 'old'", c.History[1].Track.ID)
	}
}

// TestAddHistoryCapsAtMax: History must not grow beyond maxHistory entries.
// Without this cap, a long listening session would grow the config file
// unbounded — the user's config would consume megabytes after a few weeks.
func TestAddHistoryCapsAtMax(t *testing.T) {
	c := newTestConfig(t)

	for i := 0; i < maxHistory+10; i++ {
		c.AddHistory(api.Track{ID: string(rune('a' + i%26)), Title: "Track"})
	}

	if len(c.History) > maxHistory {
		t.Fatalf("len(History) = %d, want <= %d (cap)", len(c.History), maxHistory)
	}
}

// TestAddHistoryRepeatsOnReplay: AddHistory prepends a new entry on every
// call — it does NOT deduplicate by ID. The code (slices.Insert at 0, then
// cap) makes no check for existing IDs. This test pins that behavior so a
// future "fix" that adds dedup doesn't silently change the contract.
func TestAddHistoryRepeatsOnReplay(t *testing.T) {
	c := newTestConfig(t)

	c.AddHistory(api.Track{ID: "a", Title: "A"})
	c.AddHistory(api.Track{ID: "b", Title: "B"})
	c.AddHistory(api.Track{ID: "a", Title: "A"}) // re-play

	if len(c.History) != 3 {
		t.Fatalf("len(History) = %d, want 3 (re-play creates a new entry)", len(c.History))
	}
	if c.History[0].Track.ID != "a" {
		t.Fatalf("History[0].ID = %q, want 'a' (newest first)", c.History[0].Track.ID)
	}
}

// TestAddHistorySkipsEmptyID: a track with no ID must not be added to history.
// An empty-ID history entry would render as a blank line and pollute the list.
func TestAddHistorySkipsEmptyID(t *testing.T) {
	c := newTestConfig(t)

	c.AddHistory(api.Track{ID: "", Title: "No ID"})

	if len(c.History) != 0 {
		t.Fatalf("len(History) = %d, want 0 (empty ID must be skipped)", len(c.History))
	}
}

// TestAddHistoryPrependsOnReplay: re-adding a track prepends a new entry
// regardless of where the old one sits. The code makes no dedup check — it
// just inserts at position 0 and trims the tail. This test pins that behavior.
func TestAddHistoryPrependsOnReplay(t *testing.T) {
	c := newTestConfig(t)

	c.AddHistory(api.Track{ID: "a", Title: "A"})
	c.AddHistory(api.Track{ID: "b", Title: "B"})
	c.AddHistory(api.Track{ID: "c", Title: "C"})
	// History: [c, b, a]
	c.AddHistory(api.Track{ID: "a", Title: "A"})
	// Actual: [a, c, b, a]

	if len(c.History) != 4 {
		t.Fatalf("len(History) = %d, want 4 (re-play prepends, does not move)", len(c.History))
	}
	if c.History[0].Track.ID != "a" {
		t.Fatalf("History[0].ID = %q, want 'a'", c.History[0].Track.ID)
	}
	// The original "a" is now at index 3.
	if c.History[3].Track.ID != "a" {
		t.Fatalf("History[3].ID = %q, want 'a' (original entry preserved at old position)", c.History[3].Track.ID)
	}
}

// newTestConfig returns a Config with a temp-dir path and an initialized favSet.
// Using t.TempDir() ensures no test writes to the user's real config.
func newTestConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		path: t.TempDir() + "/config.json",
	}
}
