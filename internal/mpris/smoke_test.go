package mpris

import (
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// TestSmoke exercises the MPRIS server against the real session bus: it verifies
// the bus name registers, control methods invoke the handlers, and Metadata/
// PlaybackStatus are published. Skipped when there is no session bus.
func TestSmoke(t *testing.T) {
	probe, err := dbus.SessionBus()
	if err != nil {
		t.Skip("no session bus:", err)
	}
	probe.Close()

	var mu sync.Mutex
	got := map[string]int{}
	mark := func(k string) func() { return func() { mu.Lock(); got[k]++; mu.Unlock() } }

	s, err := New(Handlers{
		Next:      mark("next"),
		Previous:  mark("prev"),
		PlayPause: mark("playpause"),
		Stop:      mark("stop"),
	})
	if err != nil {
		t.Fatal("New:", err)
	}
	defer s.Close()

	s.Update(Now{
		HasTrack: true, Title: "Song", Artist: "Artist", Album: "Album",
		LengthUS: 180_000_000, Status: "Playing", PositionUS: 1_000_000, Volume: 0.5,
	})

	conn, err := dbus.SessionBus()
	if err != nil {
		t.Fatal(err)
	}
	obj := conn.Object(s.busName, objectPath)

	// Call Next / PlayPause / Stop over the bus.
	for _, method := range []string{"Next", "PlayPause", "Stop"} {
		if call := obj.Call(playerIface+"."+method, 0); call.Err != nil {
			t.Fatalf("%s: %v", method, call.Err)
		}
	}

	// Read PlaybackStatus + Metadata via org.freedesktop.DBus.Properties.
	v, err := obj.GetProperty(playerIface + ".PlaybackStatus")
	if err != nil {
		t.Fatal("get status:", err)
	}
	if v.Value() != "Playing" {
		t.Fatalf("PlaybackStatus = %v, want Playing", v.Value())
	}

	mv, err := obj.GetProperty(playerIface + ".Metadata")
	if err != nil {
		t.Fatal("get metadata:", err)
	}
	meta, ok := mv.Value().(map[string]dbus.Variant)
	if !ok {
		t.Fatalf("Metadata type = %T", mv.Value())
	}
	if title := meta["xesam:title"].Value(); title != "Song" {
		t.Fatalf("xesam:title = %v, want Song", title)
	}

	// Give async dispatch a moment, then assert handlers fired.
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for _, k := range []string{"next", "playpause", "stop"} {
		if got[k] == 0 {
			t.Errorf("handler %q never fired", k)
		}
	}
}
