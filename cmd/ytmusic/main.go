package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/Gigoo25/youtube-music-tui/internal/config"
	"github.com/Gigoo25/youtube-music-tui/internal/mpris"
	"github.com/Gigoo25/youtube-music-tui/internal/player"
	"github.com/Gigoo25/youtube-music-tui/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// version is set at build time via -ldflags "-X main.version=…" (see the
// Makefile). Defaults to "dev" for plain `go build`/`go run`.
var version = "dev"

const usage = `ytmusic — a terminal client for YouTube Music

Usage:
  ytmusic            launch the player
  ytmusic --version  print the version and exit
  ytmusic --help     show this help

Requires the external binaries mpv and yt-dlp on PATH.
Keybindings are listed in-app under "?".`

func main() {
	for _, a := range os.Args[1:] {
		switch a {
		case "-v", "--version", "version":
			fmt.Println("ytmusic " + version)
			return
		case "-h", "--help", "help":
			fmt.Println(usage)
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown argument %q\n\n%s\n", a, usage)
			os.Exit(2)
		}
	}
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

	p, err := player.New(cfg.Volume)
	if err != nil {
		return fmt.Errorf("player: %w\n  (requires mpv + yt-dlp)", err)
	}
	defer p.Close()

	m := tui.New(p, cfg)
	prog := tea.NewProgram(m, tea.WithAltScreen())

	// Serve MPRIS in-process so media keys / shells (noctalia, playerctl) can
	// drive playback. Non-fatal if there's no session bus or the name is taken.
	if srv, err := mpris.New(tui.MPRISHandlers(prog.Send)); err != nil {
		fmt.Fprintf(os.Stderr, "mpris unavailable: %v\n", err)
	} else {
		m.SetMPRIS(srv)
		defer srv.Close()
	}

	// Run to completion, then always persist: bubbletea converts a panic inside
	// Update into a returned error, so an abnormal exit must still save.
	_, runErr := prog.Run()

	// Capture the final queue/playback state for next launch. The model owns the
	// authoritative session state (queue, position, volume).
	m.SnapshotSession()
	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "save config: %v\n", err)
	}
	// SIGINT is how a user quits a TUI — not a failure to report or exit 1 on.
	if runErr != nil && !errors.Is(runErr, tea.ErrInterrupted) {
		return fmt.Errorf("error: %w", runErr)
	}
	return nil
}
