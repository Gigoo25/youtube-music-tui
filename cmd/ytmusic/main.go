package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rob/ytmusic/internal/config"
	"github.com/rob/ytmusic/internal/player"
	"github.com/rob/ytmusic/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run owns setup/teardown so cleanup (mpv shutdown, config save) always happens
// — even on error — instead of being skipped by os.Exit.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	p, err := player.New()
	if err != nil {
		return fmt.Errorf("player: %w\n  (requires mpv + yt-dlp)", err)
	}
	defer p.Close()

	m := tui.New(p, cfg)
	prog := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		return fmt.Errorf("error: %w", err)
	}

	cfg.Volume = p.State().Volume
	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "save config: %v\n", err)
	}
	return nil
}
