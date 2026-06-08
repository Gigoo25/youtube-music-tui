package player

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// socketPath is per-process so multiple instances don't collide on /tmp.
func socketPathFor() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("ytmusic-mpv-%d.sock", os.Getpid()))
}

type State struct {
	Position float64
	Duration float64
	Volume   float64
	Paused   bool
	Idle     bool
	Loading  bool
}

type Player struct {
	cmd       *exec.Cmd
	sockPath  string
	conn      net.Conn
	mu        sync.Mutex    // protects state, conn, reqID, loadGen
	sendCh    chan []byte   // serialized writes
	reqID     int
	loadGen   int           // epoch token: cancels stale async loads
	state     State
	done      chan struct{} // signalled on natural end-of-file
	closed    chan struct{} // closed once on shutdown
	closeOnce sync.Once
}

type ipcCmd struct {
	Command   []any `json:"command"`
	RequestID int   `json:"request_id"`
}

type ipcResp struct {
	Error     string `json:"error"`
	Data      any    `json:"data"`
	RequestID int    `json:"request_id"`
	Event     string `json:"event"`
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
}

func New() (*Player, error) {
	sockPath := socketPathFor()
	os.Remove(sockPath)

	args := []string{
		"--no-video",
		"--idle=yes",
		"--input-ipc-server=" + sockPath,
		"--really-quiet",
		"--no-terminal",
	}
	// Load mpv-mpris plugin if available so MPRIS-aware shells (noctalia,
	// playerctl, GNOME, KDE) show the now-playing track and accept controls.
	if script := findMprisScript(); script != "" {
		args = append(args, "--script="+script)
	}

	cmd := exec.Command("mpv", args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mpv: %w", err)
	}

	p := &Player{
		cmd:      cmd,
		sockPath: sockPath,
		sendCh:   make(chan []byte, 64),
		done:     make(chan struct{}, 1),
		closed:   make(chan struct{}),
		state: State{
			Volume: 100,
			Idle:   true,
		},
	}

	conn, err := dialWithRetry(sockPath, 30, 100*time.Millisecond)
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("connect mpv IPC: %w", err)
	}
	p.conn = conn

	go p.writeLoop()
	go p.readLoop()

	p.send([]any{"observe_property", 1, "time-pos"})
	p.send([]any{"observe_property", 2, "duration"})
	p.send([]any{"observe_property", 3, "pause"})
	p.send([]any{"observe_property", 4, "volume"})
	p.send([]any{"observe_property", 5, "idle-active"})

	return p, nil
}

func dialWithRetry(path string, attempts int, delay time.Duration) (net.Conn, error) {
	var (
		conn net.Conn
		err  error
	)
	for i := 0; i < attempts; i++ {
		time.Sleep(delay)
		conn, err = net.Dial("unix", path)
		if err == nil {
			return conn, nil
		}
	}
	return nil, err
}

func (p *Player) writeLoop() {
	for {
		select {
		case <-p.closed:
			return
		case b := <-p.sendCh:
			p.mu.Lock()
			conn := p.conn
			p.mu.Unlock()
			if conn != nil {
				conn.Write(b) //nolint:errcheck
			}
		}
	}
}

// beginLoad resets playback state and returns the new load epoch. A later load
// (or skip) bumps the epoch so a slow yt-dlp extraction can detect it is stale.
func (p *Player) beginLoad() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loadGen++
	p.state.Position = 0
	p.state.Duration = 0
	p.state.Idle = false
	p.state.Loading = true
	select {
	case <-p.done:
	default:
	}
	return p.loadGen
}

func (p *Player) Load(videoID string) error {
	gen := p.beginLoad()

	go func() {
		url, err := extractURL(videoID)

		p.mu.Lock()
		stale := gen != p.loadGen
		if stale {
			p.mu.Unlock()
			return // a newer load superseded this one
		}
		if err != nil {
			p.state.Loading = false
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()

		p.send([]any{"loadfile", url, "replace"})
	}()
	return nil
}

// LoadURL sends a pre-resolved URL directly to mpv without yt-dlp extraction.
func (p *Player) LoadURL(url string) error {
	p.beginLoad()
	return p.send([]any{"loadfile", url, "replace"})
}

func extractURL(videoID string) (string, error) {
	ytURL := "https://www.youtube.com/watch?v=" + videoID

	ytdlp, err := exec.LookPath("yt-dlp")
	if err != nil {
		// yt-dlp not in PATH; return raw URL, mpv may handle via its ytdl hook
		return ytURL, nil
	}

	out, err := exec.Command(ytdlp, "-f", "bestaudio", "-g", ytURL).Output()
	if err != nil {
		return ytURL, nil
	}
	url := strings.TrimSpace(string(bytes.TrimRight(out, "\n")))
	if url == "" {
		return ytURL, nil
	}
	return url, nil
}

func (p *Player) PlayPause() error {
	return p.send([]any{"cycle", "pause"})
}

func (p *Player) Seek(seconds float64) error {
	return p.send([]any{"seek", seconds, "relative"})
}

func (p *Player) SetVolume(vol float64) error {
	if vol > 150 {
		vol = 150
	}
	if vol < 0 {
		vol = 0
	}
	return p.send([]any{"set_property", "volume", vol})
}

func (p *Player) VolumeUp() error {
	p.mu.Lock()
	vol := p.state.Volume + 5
	p.mu.Unlock()
	return p.SetVolume(vol)
}

func (p *Player) VolumeDown() error {
	p.mu.Lock()
	vol := p.state.Volume - 5
	p.mu.Unlock()
	return p.SetVolume(vol)
}

func (p *Player) Stop() error {
	return p.send([]any{"stop"})
}

// SetTitle sets mpv's force-media-title so MPRIS metadata (xesam:title) shows
// a clean track name in shells like noctalia instead of the raw stream URL.
func (p *Player) SetTitle(title string) error {
	if title == "" {
		return nil
	}
	return p.send([]any{"set_property", "force-media-title", title})
}

// findMprisScript locates the mpv-mpris plugin (mpris.so). Returns "" if not
// found, in which case mpv runs without MPRIS support.
func findMprisScript() string {
	if p := os.Getenv("YTMUSIC_MPRIS_SCRIPT"); p != "" && fileExists(p) {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, ".config", "mpv", "scripts", "mpris.so"); fileExists(p) {
			return p
		}
	}
	candidates := []string{
		"/usr/lib/mpv-mpris/mpris.so",
		"/usr/lib/x86_64-linux-gnu/mpv-mpris/mpris.so",
		"/usr/lib/x86_64-linux-gnu/mpris.so",
		"/usr/local/lib/mpv-mpris/mpris.so",
		"/etc/mpv/scripts/mpris.so",
	}
	for _, p := range candidates {
		if fileExists(p) {
			return p
		}
	}
	// NixOS: mpvScripts.mpris lands in the nix store.
	for _, pat := range []string{
		"/nix/store/*mpv-mpris*/share/mpv/scripts/mpris.so",
		"/nix/store/*/share/mpv/scripts/mpris.so",
	} {
		if m, _ := filepath.Glob(pat); len(m) > 0 {
			return m[len(m)-1]
		}
	}
	return ""
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func (p *Player) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// TrackEnded returns true (and resets) if a track-end event was received since last call.
func (p *Player) TrackEnded() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *Player) send(cmd []any) error {
	p.mu.Lock()
	p.reqID++
	id := p.reqID
	p.mu.Unlock()

	msg := ipcCmd{Command: cmd, RequestID: id}
	b, _ := json.Marshal(msg)
	b = append(b, '\n')

	// sendCh is never closed; the closed channel guards against enqueueing after
	// shutdown (and unblocks if writeLoop has already exited with a full buffer).
	select {
	case <-p.closed:
	case p.sendCh <- b:
	default:
		// channel full; drop command
	}
	return nil
}

func (p *Player) readLoop() {
	const maxReconnects = 5

	for attempt := 0; attempt <= maxReconnects; attempt++ {
		p.mu.Lock()
		conn := p.conn
		p.mu.Unlock()

		if conn == nil {
			return
		}

		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			var resp ipcResp
			if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
				continue
			}

			switch resp.Event {
			case "end-file":
				// Only a natural end-of-file means the track finished. Loading a new
				// file emits end-file with reason "stop"/"redirect" for the previous
				// one — ignore those so we don't falsely advance the queue.
				if resp.Reason == "eof" {
					select {
					case p.done <- struct{}{}:
					default:
					}
				}
				continue

			case "start-file":
				p.mu.Lock()
				p.state.Loading = false
				p.mu.Unlock()
				continue

			case "property-change":
				p.mu.Lock()
				switch resp.ID {
				case 1:
					if v, ok := resp.Data.(float64); ok {
						p.state.Position = v
						if v > 0 {
							p.state.Loading = false
						}
					}
				case 2:
					if v, ok := resp.Data.(float64); ok {
						p.state.Duration = v
					}
				case 3:
					if v, ok := resp.Data.(bool); ok {
						p.state.Paused = v
					}
				case 4:
					if v, ok := resp.Data.(float64); ok {
						p.state.Volume = v
					}
				case 5:
					if v, ok := resp.Data.(bool); ok {
						p.state.Idle = v
					}
				}
				p.mu.Unlock()
			}
		}

		// scanner exited — connection lost. Stop if we're shutting down.
		select {
		case <-p.closed:
			return
		default:
		}
		if attempt == maxReconnects {
			break
		}
		time.Sleep(500 * time.Millisecond)
		select {
		case <-p.closed:
			return
		default:
		}

		newConn, err := net.Dial("unix", p.sockPath)
		if err != nil {
			continue
		}

		p.mu.Lock()
		if p.conn != nil {
			p.conn.Close()
		}
		p.conn = newConn
		p.mu.Unlock()

		// re-subscribe properties on new connection
		p.send([]any{"observe_property", 1, "time-pos"})
		p.send([]any{"observe_property", 2, "duration"})
		p.send([]any{"observe_property", 3, "pause"})
		p.send([]any{"observe_property", 4, "volume"})
		p.send([]any{"observe_property", 5, "idle-active"})
	}
}

// Close shuts the player down. Safe to call more than once.
func (p *Player) Close() {
	p.closeOnce.Do(func() {
		close(p.closed) // stops writeLoop and the reconnect loop; gates send()

		p.mu.Lock()
		conn := p.conn
		p.conn = nil
		p.mu.Unlock()

		if conn != nil {
			conn.Close()
		}
		if p.cmd != nil && p.cmd.Process != nil {
			p.cmd.Process.Kill()
			p.cmd.Wait()
		}
		os.Remove(p.sockPath)
	})
}
