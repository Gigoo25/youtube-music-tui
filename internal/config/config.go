package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/rob/ytmusic/internal/api"
)

type Config struct {
	Favorites []api.Track `json:"favorites"`
	Volume    float64     `json:"volume"`
	path      string
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
		return cfg, nil
	}
	cfg.path = path
	return cfg, nil
}

func (c *Config) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0644)
}

func (c *Config) IsFavorite(id string) bool {
	for _, f := range c.Favorites {
		if f.ID == id {
			return true
		}
	}
	return false
}

func (c *Config) ToggleFavorite(t api.Track) bool {
	for i, f := range c.Favorites {
		if f.ID == t.ID {
			c.Favorites = append(c.Favorites[:i], c.Favorites[i+1:]...)
			return false
		}
	}
	c.Favorites = append(c.Favorites, t)
	return true
}
