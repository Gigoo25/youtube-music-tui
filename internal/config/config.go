package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Gigoo25/youtube-music-tui/internal/api"
)

const maxHistory = 500

// HistoryEntry records a single play with its timestamp (mirrors the original's
// history store: newest first, capped, duplicates allowed).
type HistoryEntry struct {
	Track    api.Track `json:"track"`
	PlayedAt time.Time `json:"playedAt"`
}

// Playlist is a user-named, ordered collection of tracks saved locally.
type Playlist struct {
	Name   string      `json:"name"`
	Tracks []api.Track `json:"tracks"`
}

type Config struct {
	Favorites    []api.Track    `json:"favorites"`
	History      []HistoryEntry `json:"history"`
	Playlists    []Playlist     `json:"playlists,omitempty"`
	Volume       float64        `json:"volume"`
	Theme        string         `json:"theme"`
	AutoContinue bool           `json:"autoContinue"` // keep playing radio-style when the queue ends

	// Session state, restored on next launch (see snapshotSession in the TUI).
	Queue    []api.Track `json:"queue,omitempty"`
	QueuePos int         `json:"queuePos,omitempty"`
	Shuffle  bool        `json:"shuffle,omitempty"`
	Repeat   int         `json:"repeat,omitempty"`

	path   string
	favSet map[string]struct{} // favorite IDs, for O(1) IsFavorite lookups
}

// rebuildFavSet repopulates the favorite-ID set from the Favorites slice. Called
// after load and lazily on first mutation.
func (c *Config) rebuildFavSet() {
	c.favSet = make(map[string]struct{}, len(c.Favorites))
	for _, f := range c.Favorites {
		c.favSet[f.ID] = struct{}{}
	}
}

func Load() (*Config, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}

	appDir := filepath.Join(dir, "ytmusic")
	// 0700 to match the 0600 config inside — this is private listening data.
	if err := os.MkdirAll(appDir, 0700); err != nil {
		return nil, err
	}

	path := filepath.Join(appDir, "config.json")
	cfg := &Config{path: path, Volume: 100}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		// Don't silently wipe a corrupt config — preserve it for recovery and
		// start fresh. Never clobber an earlier .corrupt: it may hold the only
		// recoverable favorites/history.
		corrupt := path + ".corrupt"
		if _, statErr := os.Stat(corrupt); statErr == nil || !os.IsNotExist(statErr) {
			corrupt += "." + time.Now().Format("20060102-150405.000000000")
		}
		if renameErr := os.Rename(path, corrupt); renameErr != nil {
			fmt.Fprintf(os.Stderr, "config: unreadable (%v) and could not be preserved (%v); starting fresh\n", err, renameErr)
		} else {
			fmt.Fprintf(os.Stderr, "config: unreadable (%v); moved to %s, starting fresh\n", err, corrupt)
		}
		return &Config{path: path, Volume: 100}, nil
	}
	cfg.path = path
	cfg.rebuildFavSet()
	return cfg, nil
}

// Save writes the config atomically (temp file + rename) so a crash mid-write
// can't truncate and lose the user's favorites/history.
func (c *Config) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// Ensure the parent directory exists — a direct Save() on an arbitrary
	// path (e.g. from tests or a custom config path) must not fail with
	// "no such file or directory" if the directory hasn't been created yet.
	if err := os.MkdirAll(filepath.Dir(c.path), 0700); err != nil {
		return err
	}
	// Unique temp name: instances coexist (mpris suffixes its bus name per pid),
	// and a shared .tmp lets one rename a half-written file over the config.
	// CreateTemp already creates 0600 — the config carries the user's full
	// listening history/favorites, no reason for it to be world-readable.
	f, err := os.CreateTemp(filepath.Dir(c.path), "config-*.json")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) //nolint:errcheck // no-op once the rename below succeeds
	if _, err := f.Write(data); err != nil {
		f.Close() //nolint:errcheck
		return err
	}
	// Sync before rename so the rename can never publish a half-written file.
	// The rename itself is not fsynced (no directory Sync), so a power cut right
	// after it can still leave the previous config — never a corrupt one.
	if err := f.Sync(); err != nil {
		f.Close() //nolint:errcheck
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

func (c *Config) IsFavorite(id string) bool {
	_, ok := c.favSet[id]
	return ok
}

func (c *Config) ToggleFavorite(t api.Track) bool {
	if c.favSet == nil {
		c.rebuildFavSet()
	}
	for i, f := range c.Favorites {
		if f.ID == t.ID {
			c.Favorites = append(c.Favorites[:i], c.Favorites[i+1:]...)
			delete(c.favSet, t.ID)
			return false
		}
	}
	c.Favorites = append(c.Favorites, t)
	c.favSet[t.ID] = struct{}{}
	return true
}

// SavePlaylist stores tracks under name, replacing an existing playlist of the
// same name. The track slice is copied so later queue mutations don't bleed in.
func (c *Config) SavePlaylist(name string, tracks []api.Track) {
	cp := append([]api.Track(nil), tracks...)
	for i := range c.Playlists {
		if c.Playlists[i].Name == name {
			c.Playlists[i].Tracks = cp
			return
		}
	}
	c.Playlists = append(c.Playlists, Playlist{Name: name, Tracks: cp})
}

// DeletePlaylist removes the named playlist (no-op if absent).
func (c *Config) DeletePlaylist(name string) {
	for i := range c.Playlists {
		if c.Playlists[i].Name == name {
			c.Playlists = append(c.Playlists[:i], c.Playlists[i+1:]...)
			return
		}
	}
}

// AddToPlaylist appends t to the named playlist, creating the playlist when it
// doesn't exist yet. Returns false when the track is already in it (no change).
func (c *Config) AddToPlaylist(name string, t api.Track) bool {
	for i := range c.Playlists {
		if c.Playlists[i].Name == name {
			for _, x := range c.Playlists[i].Tracks {
				if x.ID == t.ID {
					return false
				}
			}
			c.Playlists[i].Tracks = append(c.Playlists[i].Tracks, t)
			return true
		}
	}
	c.Playlists = append(c.Playlists, Playlist{Name: name, Tracks: []api.Track{t}})
	return true
}

// RemoveFromPlaylist deletes the track at index idx of the named playlist
// (no-op when the playlist or index doesn't exist).
func (c *Config) RemoveFromPlaylist(name string, idx int) {
	for i := range c.Playlists {
		if c.Playlists[i].Name == name {
			ts := c.Playlists[i].Tracks
			if idx < 0 || idx >= len(ts) {
				return
			}
			c.Playlists[i].Tracks = append(ts[:idx], ts[idx+1:]...)
			return
		}
	}
}

// PlaylistByName returns the named playlist, or nil when absent.
func (c *Config) PlaylistByName(name string) *Playlist {
	for i := range c.Playlists {
		if c.Playlists[i].Name == name {
			return &c.Playlists[i]
		}
	}
	return nil
}

// AddHistory records a play at the front of the history list (newest first),
// capped at maxHistory. Matches the original: each play is its own entry.
func (c *Config) AddHistory(t api.Track) {
	if t.ID == "" {
		return
	}
	entry := HistoryEntry{Track: t, PlayedAt: time.Now()}
	// Prepend (newest first). Insert shifts the existing entries right and grows
	// the slice, so at cap it allocates a fresh backing array — 500 entries once
	// per play isn't worth restructuring the (newest-first) read sites for.
	c.History = slices.Insert(c.History, 0, entry)
	if len(c.History) > maxHistory {
		c.History = c.History[:maxHistory]
	}
}
