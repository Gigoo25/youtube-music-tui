package player

import (
	"encoding/json"
	"testing"
)

// TestClampVolumeBounds: clampVolume enforces mpv's hard limits — 0 minimum,
// 150 maximum. The TUI's volume bar is painted from the returned value; if
// clamp ever lets a value outside [0,150] escape, the bar lies to the user.
// The 150 ceiling in particular is mpv's own cap — setting volume=200 through
// the IPC channel still results in mpv running at 150, so showing 200 on the
// bar would be a silent desync.
func TestClampVolumeBounds(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{-10, 0},
		{0, 0},
		{50, 50},
		{100, 100},
		{150, 150},
		{200, 150},
	}
	for _, c := range cases {
		if got := clampVolume(c.in); got != c.want {
			t.Errorf("clampVolume(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestVolumeUpClampsAtCeiling: VolumeUp returns the *new* level so the TUI can
// paint the shortcuts bar without waiting for mpv's property observer. If the
// return ever exceeds mpv's clamp, the bar shows a number mpv is not at — the
// user adjusts volume and the display jumps.
func TestVolumeUpClampsAtCeiling(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.mu.Lock()
	p.state.Volume = 148
	p.mu.Unlock()

	if got := p.VolumeUp(); got != 150 {
		t.Fatalf("VolumeUp from 148 = %v, want 150 (clamped at mpv ceiling)", got)
	}
	if got := p.VolumeUp(); got != 150 {
		t.Fatalf("VolumeUp from 150 = %v, want 150 (already at ceiling)", got)
	}
}

// TestVolumeDownClampsAtFloor mirrors the ceiling test at the low end. A
// VolumeDown that returns a negative number would paint a minus sign the user
// can't actually achieve.
func TestVolumeDownClampsAtFloor(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.mu.Lock()
	p.state.Volume = 2
	p.mu.Unlock()

	if got := p.VolumeDown(); got != 0 {
		t.Fatalf("VolumeDown from 2 = %v, want 0 (clamped at floor)", got)
	}
	if got := p.VolumeDown(); got != 0 {
		t.Fatalf("VolumeDown from 0 = %v, want 0 (already at floor)", got)
	}
}

// TestVolumeUpNudgesCorrectly: VolumeUp must add exactly 5 to the current
// level (before clamping). The TUI's shortcuts bar uses the return value as
// the displayed number — if the nudge is wrong the bar drifts from reality.
func TestVolumeUpNudgesCorrectly(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.mu.Lock()
	p.state.Volume = 50
	p.mu.Unlock()

	if got := p.VolumeUp(); got != 55 {
		t.Fatalf("VolumeUp from 50 = %v, want 55", got)
	}
}

// TestVolumeDownNudgesCorrectly: mirror of the VolumeUp nudge test.
func TestVolumeDownNudgesCorrectly(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.mu.Lock()
	p.state.Volume = 50
	p.mu.Unlock()

	if got := p.VolumeDown(); got != 45 {
		t.Fatalf("VolumeDown from 50 = %v, want 45", got)
	}
}

// TestSetVolumeSendsToMpv: SetVolume must queue a command to mpv so the actual
// hardware volume tracks what the TUI displays. A SetVolume that only updates
// the local state would make the bar lie the moment the user touches the
// slider.
func TestSetVolumeSendsToMpv(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.SetVolume(75)

	if len(p.sendCh) == 0 {
		t.Fatal("SetVolume must queue a command to mpv")
	}
}

// TestSetVolumeClampsBeforeSending: SetVolume must clamp the value *before*
// sending it to mpv. If it sent an unclamped value, mpv would ignore it (it
// clamps to 150 internally) and the TUI would display a number mpv is not at.
func TestSetVolumeClampsBeforeSending(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.SetVolume(200)

	if len(p.sendCh) == 0 {
		t.Fatal("SetVolume must still queue a command at clamped values")
		return
	}
	b := <-p.sendCh
	var cmd ipcCmd
	if err := unmarshalCmd(b, &cmd); err != nil {
		t.Fatalf("SetVolume output is not valid IPC JSON: %v", err)
	}
	// The volume value is the second element of the command array.
	if len(cmd.Command) < 3 {
		t.Fatalf("command = %v, want at least 3 elements", cmd.Command)
	}
	vol, ok := cmd.Command[2].(float64)
	if !ok {
		t.Fatalf("volume element = %v (%T), want float64", cmd.Command[2], cmd.Command[2])
	}
	if vol != 150 {
		t.Fatalf("volume sent to mpv = %v, want 150 (clamped)", vol)
	}
}

// unmarshalCmd decodes a raw IPC JSON line into an ipcCmd. Helper so the test
// bodies stay focused on what they're asserting.
func unmarshalCmd(b []byte, cmd *ipcCmd) error {
	// send appends a newline; trim it first.
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return json.Unmarshal(b, cmd)
}
