package mpris

import (
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// TestVolumeSetDoesNotBlockUpdate reproduces the prop.mut deadlock: a D-Bus
// Volume Set runs its callback under prop.Properties.mut, and SetVolume blocks
// (as Program.Send does on its unbuffered channel). Update must still make
// progress. Reverting the `go h.SetVolume(v)` hand-off hangs this test.
func TestVolumeSetDoesNotBlockUpdate(t *testing.T) {
	if _, err := dbus.SessionBus(); err != nil {
		t.Skip("no session bus:", err)
	}

	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	s, err := New(Handlers{SetVolume: func(float64) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release // stand-in for a blocking Program.Send
	}})
	if err != nil {
		t.Fatal("New:", err)
	}
	defer s.Close()
	defer close(release)

	conn, err := dbus.SessionBus()
	if err != nil {
		t.Fatal(err)
	}
	obj := conn.Object(s.busName, objectPath)
	go obj.Call("org.freedesktop.DBus.Properties.Set", 0, //nolint:errcheck
		playerIface, "Volume", dbus.MakeVariant(0.5))

	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("SetVolume handler never ran")
	}

	done := make(chan struct{})
	go func() {
		s.Update(Now{HasTrack: true, Title: "T", Status: "Playing", PositionUS: 1, Volume: 0.9})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Update blocked: prop.mut still held by the Volume callback (deadlock)")
	}
}
