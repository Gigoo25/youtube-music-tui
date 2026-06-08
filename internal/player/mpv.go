package player

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const socketPath = "/tmp/ytmusic-mpv.sock"

type State struct {
	Position float64
	Duration float64
	Volume   float64
	Paused   bool
	Idle     bool
}

type Player struct {
	cmd   *exec.Cmd
	conn  net.Conn
	mu    sync.Mutex
	reqID int
	state State
	done  chan struct{} // closed when end-file received
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
}

func New() (*Player, error) {
	os.Remove(socketPath)

	cmd := exec.Command("mpv",
		"--no-video",
		"--idle=yes",
		"--input-ipc-server="+socketPath,
		"--really-quiet",
		"--no-terminal",
	)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mpv: %w", err)
	}

	p := &Player{
		cmd:  cmd,
		done: make(chan struct{}, 1),
		state: State{
			Volume: 100,
			Idle:   true,
		},
	}

	var conn net.Conn
	var err error
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			break
		}
	}
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("connect mpv IPC: %w", err)
	}
	p.conn = conn

	p.send([]any{"observe_property", 1, "time-pos"})
	p.send([]any{"observe_property", 2, "duration"})
	p.send([]any{"observe_property", 3, "pause"})
	p.send([]any{"observe_property", 4, "volume"})
	p.send([]any{"observe_property", 5, "idle-active"})

	go p.readLoop()
	return p, nil
}

func (p *Player) Load(videoID string) error {
	p.mu.Lock()
	p.state.Position = 0
	p.state.Duration = 0
	p.state.Idle = false
	select {
	case <-p.done:
	default:
	}
	p.mu.Unlock()

	go func() {
		url, err := extractURL(videoID)
		if err != nil {
			return
		}
		p.send([]any{"loadfile", url, "replace"})
	}()
	return nil
}

func extractURL(videoID string) (string, error) {
	ytURL := "https://www.youtube.com/watch?v=" + videoID
	out, err := exec.Command("yt-dlp", "-f", "bestaudio", "-g", ytURL).Output()
	if err != nil {
		// fallback: pass the watch URL directly and hope mpv handles it
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
	conn := p.conn
	p.mu.Unlock()

	if conn == nil {
		return nil
	}
	msg := ipcCmd{Command: cmd, RequestID: id}
	b, _ := json.Marshal(msg)
	b = append(b, '\n')
	_, err := conn.Write(b)
	return err
}

func (p *Player) readLoop() {
	scanner := bufio.NewScanner(p.conn)
	for scanner.Scan() {
		var resp ipcResp
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			continue
		}

		if resp.Event == "end-file" {
			select {
			case p.done <- struct{}{}:
			default:
			}
			continue
		}

		if resp.Event != "property-change" {
			continue
		}

		p.mu.Lock()
		switch resp.ID {
		case 1:
			if v, ok := resp.Data.(float64); ok {
				p.state.Position = v
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

func (p *Player) Close() {
	if p.conn != nil {
		p.conn.Close()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
		p.cmd.Wait()
	}
	os.Remove(socketPath)
}
