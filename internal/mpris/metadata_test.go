package mpris

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

// metaNow reads the Metadata dict straight out of the property table, which is
// what a client's Get/GetAll returns.
func metaNow(t *testing.T, s *Server) map[string]dbus.Variant {
	t.Helper()
	md, ok := s.props.GetMust(playerIface, "Metadata").(map[string]dbus.Variant)
	if !ok {
		t.Fatalf("Metadata is not a{sv}")
	}
	return md
}

// TestMetadataDoesNotLeakPreviousTrack: prop.Properties merges every Set into one
// long-lived map and never deletes keys (see storeMapIntoMap), so any field this
// server omits keeps the previous track's value. Playing a track with no album
// after one with an album used to show the old album and the old artist.
func TestMetadataDoesNotLeakPreviousTrack(t *testing.T) {
	if _, err := dbus.SessionBus(); err != nil {
		t.Skip("no session bus:", err)
	}
	s, err := New(Handlers{})
	if err != nil {
		t.Fatal("New:", err)
	}
	defer s.Close()

	s.Update(Now{HasTrack: true, Title: "A", Artist: "Art", Album: "Alb", Status: "Playing", LengthUS: 1000})
	s.Update(Now{HasTrack: true, Title: "B", Status: "Playing", LengthUS: 2000})

	md := metaNow(t, s)
	if got := md["xesam:title"].Value(); got != "B" {
		t.Fatalf("xesam:title = %v, want B", got)
	}
	if got := md["xesam:album"].Value(); got != "" {
		t.Errorf("xesam:album = %q, want empty: the previous track's album leaked", got)
	}
	if got, ok := md["xesam:artist"].Value().([]string); !ok || len(got) != 0 {
		t.Errorf("xesam:artist = %v, want empty: the previous track's artist leaked", got)
	}
}

// TestMetadataClearsWhenNothingPlays: stopping must not leave the last track on
// screen in every MPRIS shell. mpris:trackid still has to be a valid object path,
// so it carries the spec's NoTrack sentinel rather than being dropped.
func TestMetadataClearsWhenNothingPlays(t *testing.T) {
	if _, err := dbus.SessionBus(); err != nil {
		t.Skip("no session bus:", err)
	}
	s, err := New(Handlers{})
	if err != nil {
		t.Fatal("New:", err)
	}
	defer s.Close()

	s.Update(Now{HasTrack: true, Title: "A", Artist: "Art", Album: "Alb", Status: "Playing", LengthUS: 1000})
	s.Update(Now{HasTrack: false, Status: "Stopped"})

	md := metaNow(t, s)
	if got := md["xesam:title"].Value(); got != "" {
		t.Errorf("xesam:title = %q, want empty after the track stopped", got)
	}
	if got := md["xesam:album"].Value(); got != "" {
		t.Errorf("xesam:album = %q, want empty after the track stopped", got)
	}
	if got := md["mpris:length"].Value(); got != int64(0) {
		t.Errorf("mpris:length = %v, want 0 after the track stopped", got)
	}
	if got := md["mpris:trackid"].Value(); got != dbus.ObjectPath(noTrackPath) {
		t.Errorf("mpris:trackid = %v, want the NoTrack sentinel", got)
	}
}

// TestMetadataReusesTrackidForLateDuration guards F28: the duration lands a beat
// after the track, and publishing it must update the existing trackid. A fresh
// trackid reads as a new song and makes every track notify twice.
func TestMetadataReusesTrackidForLateDuration(t *testing.T) {
	if _, err := dbus.SessionBus(); err != nil {
		t.Skip("no session bus:", err)
	}
	s, err := New(Handlers{})
	if err != nil {
		t.Fatal("New:", err)
	}
	defer s.Close()

	s.Update(Now{HasTrack: true, Title: "A", Status: "Playing"})
	first := metaNow(t, s)["mpris:trackid"].Value()
	s.Update(Now{HasTrack: true, Title: "A", Status: "Playing", LengthUS: 42000})
	second := metaNow(t, s)

	if second["mpris:trackid"].Value() != first {
		t.Errorf("trackid changed when only the duration arrived: %v -> %v", first, second["mpris:trackid"].Value())
	}
	if got := second["mpris:length"].Value(); got != int64(42000) {
		t.Errorf("mpris:length = %v, want 42000", got)
	}
	// A genuinely different track must still mint a new id.
	s.Update(Now{HasTrack: true, Title: "B", Status: "Playing"})
	if metaNow(t, s)["mpris:trackid"].Value() == first {
		t.Error("a new track reused the previous trackid")
	}
}
