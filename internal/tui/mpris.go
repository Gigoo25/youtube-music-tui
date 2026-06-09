package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/rob/ytmusic/internal/mpris"
)

// MPRIS control messages, delivered from the D-Bus goroutine via Program.Send so
// they are handled on the single Update goroutine (same as keyboard input).
type mprisAction int

const (
	mprisNext mprisAction = iota
	mprisPrev
	mprisPlay
	mprisPause
	mprisPlayPause
	mprisStop
	mprisQuit
)

type mprisActionMsg struct{ action mprisAction }
type mprisSeekMsg struct{ offsetUS int64 }
type mprisSetPosMsg struct{ posUS int64 }
type mprisSetVolMsg struct{ level float64 }

// mprisServer aliases mpris.Server so model.go can hold the field without also
// importing the mpris package.
type mprisServer = mpris.Server

// SetMPRIS attaches the MPRIS server so the model can publish now-playing state.
func (m *model) SetMPRIS(s *mpris.Server) { m.mpris = s }

// MPRISHandlers builds the D-Bus control callbacks. Each forwards an internal
// message into the bubbletea loop via send (Program.Send), so control actions run
// on the Update goroutine exactly like the equivalent key presses.
func MPRISHandlers(send func(tea.Msg)) mpris.Handlers {
	return mpris.Handlers{
		Next:        func() { send(mprisActionMsg{mprisNext}) },
		Previous:    func() { send(mprisActionMsg{mprisPrev}) },
		Play:        func() { send(mprisActionMsg{mprisPlay}) },
		Pause:       func() { send(mprisActionMsg{mprisPause}) },
		PlayPause:   func() { send(mprisActionMsg{mprisPlayPause}) },
		Stop:        func() { send(mprisActionMsg{mprisStop}) },
		Quit:        func() { send(mprisActionMsg{mprisQuit}) },
		Seek:        func(off int64) { send(mprisSeekMsg{off}) },
		SetPosition: func(pos int64) { send(mprisSetPosMsg{pos}) },
		SetVolume:   func(l float64) { send(mprisSetVolMsg{l}) },
		// Raise is intentionally nil (terminal app) → CanRaise is reported false.
	}
}

// pushMPRIS publishes the current player snapshot to the MPRIS server. Cheap to
// call every tick: the server only emits PropertiesChanged when values change.
func (m *model) pushMPRIS() {
	if m.mpris == nil {
		return
	}
	st := m.playerState
	status := "Stopped"
	if m.hasCurrent {
		if st.Paused {
			status = "Paused"
		} else {
			status = "Playing"
		}
	}
	m.mpris.Update(mpris.Now{
		HasTrack:   m.hasCurrent,
		Title:      m.current.Title,
		Artist:     m.current.Artist,
		Album:      m.current.Album,
		LengthUS:   int64(st.Duration * 1e6),
		Status:     status,
		PositionUS: int64(st.Position * 1e6),
		Volume:     st.Volume / 100,
	})
}

// handleMPRISAction applies a control action received over D-Bus.
func (m *model) handleMPRISAction(a mprisAction) (tea.Model, tea.Cmd) {
	switch a {
	case mprisNext:
		return m, m.nextTrack()
	case mprisPrev:
		m.prevTrack()
	case mprisPlay:
		m.player.Play()
	case mprisPause:
		m.player.Pause()
	case mprisPlayPause:
		m.player.PlayPause()
	case mprisStop:
		m.player.Stop()
		m.hasCurrent = false
	case mprisQuit:
		return m, tea.Quit
	}
	return m, nil
}
