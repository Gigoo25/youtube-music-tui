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
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	p, err := player.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "player: %v\n  (requires mpv + yt-dlp)\n", err)
		os.Exit(1)
	}
	defer p.Close()

	m := tui.New(p, cfg)
	prog := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	cfg.Volume = p.State().Volume
	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "save config: %v\n", err)
	}
}
