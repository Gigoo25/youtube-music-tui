package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/rob/ytmusic/internal/api"
)

const maxHistory = 500

// HistoryEntry records a single play with its timestamp (mirrors the original's
// history store: newest first, capped, duplicates allowed).
type HistoryEntry struct {
	Track    api.Track `json:"track"`
	PlayedAt time.Time `json:"playedAt"`
}

type Config struct {
	Favorites []api.Track    `json:"favorites"`
	History   []HistoryEntry `json:"history"`
	Volume    float64        `json:"volume"`
	Theme     string         `json:"theme"`
	path      string
	favSet    map[string]struct{} // favorite IDs, for O(1) IsFavorite lookups
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
	if err := os.MkdirAll(appDir, 0755); err != nil {
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
		// start fresh.
		os.Rename(path, path+".corrupt")
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
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
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

// AddHistory records a play at the front of the history list (newest first),
// capped at maxHistory. Matches the original: each play is its own entry.
func (c *Config) AddHistory(t api.Track) {
	if t.ID == "" {
		return
	}
	entry := HistoryEntry{Track: t, PlayedAt: time.Now()}
	// Insert at the front; once History reaches maxHistory this shifts in place
	// and reuses the backing array instead of allocating a fresh slice each play.
	c.History = slices.Insert(c.History, 0, entry)
	if len(c.History) > maxHistory {
		c.History = c.History[:maxHistory]
	}
}
