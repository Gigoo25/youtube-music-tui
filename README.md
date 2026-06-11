# youtube-music-tui

A fast, low-dependency terminal client for YouTube Music, written in Go. Search,
build a queue, and play audio-only streams — with a sidebar-driven TUI, vim-style
navigation, and full MPRIS media-key integration on Linux.

A reimplementation of [involvex/youtube-music-cli](https://github.com/involvex/youtube-music-cli)
(Node/Ink) as a single static Go binary.

## Features

- **Search** YouTube Music (Songs filter — official audio, not music videos), with lazy "load more" as you scroll
- **Queue** with reorder, remove, shuffle, repeat (off / all / one), mute, and jump-to-now-playing
- **Local playlists** — save the queue as a named playlist, then play or append it later
- **Session restore** — your queue and playback toggles come back on next launch
- **Home** screen: Listen Again (your history) + Quick Picks (related to your last play)
- **Album & artist pages** — jump from any track to its album (`a`) or artist (`A`)
- **Radio** (`R`) and **auto-continue** (`C`): keep playing related tracks when the queue ends
- **Random song** by genre (`z`)
- **Favorites & history**, persisted locally (no account, no cookies, no tracking)
- **Local filtering** of any list with `/`
- **MPRIS**: media keys, `playerctl`, and desktop now-playing integration (in-process D-Bus server)
- **6 color themes** (everforest, tokyo-night, nord, gruvbox, catppuccin, dracula), cycle with `T`
- **Instant track advance**: upcoming stream URLs are prefetched in the background

## Requirements

- [mpv](https://mpv.io/) — audio playback
- [yt-dlp](https://github.com/yt-dlp/yt-dlp) — stream URL resolution
- A [Nerd Font](https://www.nerdfonts.com/) for the icons
- Linux with a D-Bus session bus for MPRIS (optional — the app runs fine without it)

On NixOS: `nix-shell -p mpv yt-dlp`.

## Install

```bash
go install github.com/Gigoo25/youtube-music-tui/cmd/ytmusic@latest
```

Or build from source:

```bash
git clone https://github.com/Gigoo25/youtube-music-tui
cd youtube-music-tui
make release        # builds ./ytmusic
```

## Usage

```bash
ytmusic              # launch
ytmusic --version    # print version
ytmusic --help       # usage
```

| Key | Action |
| --- | --- |
| `1`–`6` | Jump to Home / Search / Queue / Favorites / History / Playlists |
| `tab`, `h`, `esc` | Move between sidebar and panel |
| `j` / `k`, `gg` / `G`, `ctrl+d` / `ctrl+u` | Navigate lists (vim-style) |
| `enter` | Queue the selected track |
| `p` | Play the selected track now |
| `e` | Queue all (album / artist / search results / playlist) |
| `space` | Play / pause |
| `n` / `b` | Next / previous track |
| `<` / `>` | Seek ±10s |
| `+` / `-` / `m` | Volume up / down / mute |
| `s` / `r` | Shuffle / repeat mode |
| `R` / `C` | Radio from current track / auto-continue toggle |
| `f` | Toggle favorite |
| `a` / `A` | Open track's album / artist |
| `S` | Save the queue as a playlist |
| `.` | Jump to the now-playing track (in Queue) |
| `/` | Filter the current list |
| `z` | Random song (genre picker) |
| `T` | Cycle color theme |
| `?` | Help |
| `q` | Quit |

Full keybindings: press `?` in the app.

## Configuration

State (favorites, history, playlists, volume, theme, auto-continue, and the last
session's queue) is stored as JSON in `$XDG_CONFIG_HOME/ytmusic/config.json`
(usually `~/.config/ytmusic/config.json`), mode `0600`. Writes are atomic; a
corrupt file is preserved as `config.json.corrupt` instead of being overwritten.

## How it works

The app talks to the public YouTube Music Innertube API for search/browse
metadata, resolves an audio-only stream URL with `yt-dlp`, and plays it through
an `mpv` subprocess controlled over a JSON IPC socket. MPRIS is served
in-process over D-Bus, so media keys map directly to the app's own queue.

Go dependencies are intentionally minimal: bubbletea + lipgloss (TUI) and
godbus (MPRIS). Everything else is the standard library.

## Development

```bash
make build    # dev binary
make test     # go test ./...
make vet      # go vet ./...
```
