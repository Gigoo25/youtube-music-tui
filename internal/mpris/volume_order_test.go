package mpris

import (
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// TestVolumeSetsApplyInOrder: dragging a volume slider emits a burst of D-Bus
// Sets. The callback can't apply them synchronously (see the prop.mut deadlock
// in TestVolumeSetDoesNotBlockUpdate), but handing each one to its own goroutine
// lets them race into Program.Send — so they can land out of order and leave the
// player at a level D-Bus never reported last. Values may be coalesced away, but
// what does land must be in order, ending on the newest.
func TestVolumeSetsApplyInOrder(t *testing.T) {
	if _, err := dbus.SessionBus(); err != nil {
		t.Skip("no session bus:", err)
	}

	var (
		mu   sync.Mutex
		got  []float64
		last = make(chan float64, 32)
	)
	s, err := New(Handlers{SetVolume: func(v float64) {
		// Slow enough that the next Set arrives mid-apply, which is what makes a
		// goroutine-per-change implementation reorder.
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		got = append(got, v)
		mu.Unlock()
		last <- v
	}})
	if err != nil {
		t.Fatal("New:", err)
	}
	defer s.Close()

	conn, err := dbus.SessionBus()
	if err != nil {
		t.Fatal(err)
	}
	obj := conn.Object(s.busName, objectPath)

	const n = 10
	for i := 1; i <= n; i++ {
		v := float64(i) / n
		if call := obj.Call("org.freedesktop.DBus.Properties.Set", 0,
			playerIface, "Volume", dbus.MakeVariant(v)); call.Err != nil {
			t.Fatal("Set:", call.Err)
		}
	}

	// Wait for the burst to drain, i.e. for the newest value to be applied.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case v := <-last:
			if v != 1.0 {
				continue
			}
		case <-deadline:
			mu.Lock()
			t.Fatalf("final volume 1.0 never applied; applied %v", got)
		}
		break
	}

	mu.Lock()
	defer mu.Unlock()
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("volume applied out of order: %v", got)
		}
	}
	if got[len(got)-1] != 1.0 {
		t.Fatalf("last applied volume = %v, want the newest (1.0); applied %v", got[len(got)-1], got)
	}
}
