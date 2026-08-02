// Package mpris implements an in-process MPRIS (org.mpris.MediaPlayer2) D-Bus
// server so MPRIS-aware shells (noctalia, playerctl, GNOME/KDE) can show the
// now-playing track and drive playback. Unlike the mpv-mpris plugin, control
// actions (Next/Previous/Stop/…) are delivered straight to the app's queue via
// the Handlers callbacks, so media-key skip/previous map exactly to the TUI's
// own n/b behaviour.
package mpris

import (
	"fmt"
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const (
	objectPath  = "/org/mpris/MediaPlayer2"
	rootIface   = "org.mpris.MediaPlayer2"
	playerIface = "org.mpris.MediaPlayer2.Player"
	// busNameBase is suffixed with ".instance<pid>" per the MPRIS spec so multiple
	// running instances don't collide on the same well-known name (shells discover
	// any name under the org.mpris.MediaPlayer2.* prefix).
	busNameBase = "org.mpris.MediaPlayer2.ytmusic"
)

// Handlers are the control callbacks invoked from the D-Bus goroutine. They must
// be non-blocking; the app forwards them into the bubbletea loop via Program.Send.
type Handlers struct {
	Next      func()
	Previous  func()
	Play      func()
	Pause     func()
	PlayPause func()
	Stop      func()
	Raise     func()
	Quit      func()
	// Seek by a relative offset in microseconds (may be negative).
	Seek func(offsetUS int64)
	// SetPosition to an absolute offset in microseconds.
	SetPosition func(posUS int64)
	// SetVolume to a linear 0..1 level.
	SetVolume func(level float64)
}

// Now is a snapshot of player state pushed by the model each tick.
type Now struct {
	HasTrack   bool
	Title      string
	Artist     string
	Album      string
	LengthUS   int64
	Status     string // "Playing" | "Paused" | "Stopped"
	PositionUS int64
	Volume     float64 // linear 0..1
}

// Server holds the D-Bus connection and the exported properties.
type Server struct {
	conn    *dbus.Conn
	props   *prop.Properties
	busName string

	// Change-detection state, written only by Update (see its comment).
	trackSeq  int
	lastKey   string // change detection for Metadata
	lastStat  string
	lastVol   float64
	lastPos   int64
	trackPath dbus.ObjectPath
}

// obj carries the Handlers and implements the exported D-Bus methods.
type obj struct {
	h Handlers
}

func call(f func()) {
	if f != nil {
		f()
	}
}

func (o *obj) Raise() *dbus.Error     { call(o.h.Raise); return nil }
func (o *obj) Quit() *dbus.Error      { call(o.h.Quit); return nil }
func (o *obj) Next() *dbus.Error      { call(o.h.Next); return nil }
func (o *obj) Previous() *dbus.Error  { call(o.h.Previous); return nil }
func (o *obj) Pause() *dbus.Error     { call(o.h.Pause); return nil }
func (o *obj) PlayPause() *dbus.Error { call(o.h.PlayPause); return nil }
func (o *obj) Stop() *dbus.Error      { call(o.h.Stop); return nil }
func (o *obj) Play() *dbus.Error      { call(o.h.Play); return nil }

// seekRel is exported on the bus as "Seek" via the method table. It is not named
// Seek on the type so `go vet`'s stdmethods check doesn't mistake it for io.Seeker.
func (o *obj) seekRel(offsetUS int64) *dbus.Error {
	if o.h.Seek != nil {
		o.h.Seek(offsetUS)
	}
	return nil
}

// SetPosition takes the track id (ignored — we play one track at a time) and an
// absolute position in microseconds.
func (o *obj) SetPosition(_ dbus.ObjectPath, posUS int64) *dbus.Error {
	if o.h.SetPosition != nil {
		o.h.SetPosition(posUS)
	}
	return nil
}

func (o *obj) OpenUri(_ string) *dbus.Error { return nil } // unsupported

// New connects to the session bus, exports the MPRIS interfaces, and claims the
// well-known bus name. Returns an error (non-fatal to the app) if there is no
// session bus or the name is already taken.
func New(h Handlers) (*Server, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, fmt.Errorf("session bus: %w", err)
	}

	o := &obj{h: h}
	rootTable := map[string]any{
		"Raise": o.Raise,
		"Quit":  o.Quit,
	}
	playerTable := map[string]any{
		"Next":        o.Next,
		"Previous":    o.Previous,
		"Pause":       o.Pause,
		"PlayPause":   o.PlayPause,
		"Stop":        o.Stop,
		"Play":        o.Play,
		"Seek":        o.seekRel,
		"SetPosition": o.SetPosition,
		"OpenUri":     o.OpenUri,
	}
	if err := conn.ExportMethodTable(rootTable, objectPath, rootIface); err != nil {
		return nil, fmt.Errorf("export root: %w", err)
	}
	if err := conn.ExportMethodTable(playerTable, objectPath, playerIface); err != nil {
		return nil, fmt.Errorf("export player: %w", err)
	}

	s := &Server{conn: conn, lastVol: -1}

	// Runs with prop.Properties.mut held (see prop.Set), while SetVolume forwards
	// through bubbletea's Program.Send, which blocks on an unbuffered channel. The
	// Update goroutine holds nothing but wants that same mut in props.SetMust, so
	// calling SetVolume synchronously here deadlocks the app on any tick that races
	// a `playerctl volume` set. So the callback must hand off and return.
	//
	// One worker, not a goroutine per change: goroutines race each other into
	// Send, so dragging a volume slider (which emits a burst of sets) could apply
	// them out of order and leave mpv disagreeing with the level D-Bus reports.
	// Volume is an absolute level, so the worker coalesces — it always applies the
	// newest pending value and drops the ones overtaken while it was busy.
	volumeCb := func(*prop.Change) *dbus.Error { return nil }
	if h.SetVolume != nil {
		var (
			volMu   sync.Mutex
			pending float64
		)
		wake := make(chan struct{}, 1)
		go func() {
			for range wake {
				volMu.Lock()
				v := pending
				volMu.Unlock()
				h.SetVolume(v)
			}
		}()
		volumeCb = func(c *prop.Change) *dbus.Error {
			v, ok := c.Value.(float64)
			if !ok {
				return nil
			}
			volMu.Lock()
			pending = v
			volMu.Unlock()
			select {
			case wake <- struct{}{}:
			default: // already signalled; the worker will read the value above
			}
			return nil
		}
	}

	propsSpec := map[string]map[string]*prop.Prop{
		rootIface: {
			"CanQuit":             {Value: true, Emit: prop.EmitTrue},
			"CanRaise":            {Value: h.Raise != nil, Emit: prop.EmitTrue},
			"HasTrackList":        {Value: false, Emit: prop.EmitTrue},
			"Identity":            {Value: "YouTube Music TUI", Emit: prop.EmitTrue},
			"DesktopEntry":        {Value: "ytmusic", Emit: prop.EmitTrue},
			"SupportedUriSchemes": {Value: []string{}, Emit: prop.EmitTrue},
			"SupportedMimeTypes":  {Value: []string{}, Emit: prop.EmitTrue},
		},
		playerIface: {
			"PlaybackStatus": {Value: "Stopped", Emit: prop.EmitTrue},
			"Metadata":       {Value: map[string]dbus.Variant{}, Emit: prop.EmitTrue},
			"Volume":         {Value: 1.0, Writable: true, Emit: prop.EmitTrue, Callback: volumeCb},
			"Position":       {Value: int64(0), Emit: prop.EmitFalse},
			"Rate":           {Value: 1.0, Emit: prop.EmitTrue},
			"MinimumRate":    {Value: 1.0, Emit: prop.EmitTrue},
			"MaximumRate":    {Value: 1.0, Emit: prop.EmitTrue},
			"CanGoNext":      {Value: true, Emit: prop.EmitTrue},
			"CanGoPrevious":  {Value: true, Emit: prop.EmitTrue},
			"CanPlay":        {Value: true, Emit: prop.EmitTrue},
			"CanPause":       {Value: true, Emit: prop.EmitTrue},
			"CanSeek":        {Value: true, Emit: prop.EmitTrue},
			"CanControl":     {Value: true, Emit: prop.EmitFalse},
		},
	}

	props, err := prop.Export(conn, objectPath, propsSpec)
	if err != nil {
		return nil, fmt.Errorf("export props: %w", err)
	}
	s.props = props

	// Introspection so clients can discover the interfaces.
	inArg := func(name, typ string) introspect.Arg {
		return introspect.Arg{Name: name, Type: typ, Direction: "in"}
	}
	node := &introspect.Node{
		Name: objectPath,
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{
				Name: rootIface,
				Methods: []introspect.Method{
					{Name: "Raise"},
					{Name: "Quit"},
				},
				Properties: props.Introspection(rootIface),
			},
			{
				Name: playerIface,
				Methods: []introspect.Method{
					{Name: "Next"},
					{Name: "Previous"},
					{Name: "Pause"},
					{Name: "PlayPause"},
					{Name: "Stop"},
					{Name: "Play"},
					{Name: "Seek", Args: []introspect.Arg{inArg("Offset", "x")}},
					{Name: "SetPosition", Args: []introspect.Arg{inArg("TrackId", "o"), inArg("Position", "x")}},
					{Name: "OpenUri", Args: []introspect.Arg{inArg("Uri", "s")}},
				},
				Signals: []introspect.Signal{
					{Name: "Seeked", Args: []introspect.Arg{{Name: "Position", Type: "x"}}},
				},
				Properties: props.Introspection(playerIface),
			},
		},
	}
	if err := conn.Export(introspect.NewIntrospectable(node), objectPath,
		"org.freedesktop.DBus.Introspectable"); err != nil {
		return nil, fmt.Errorf("export introspect: %w", err)
	}

	s.busName = fmt.Sprintf("%s.instance%d", busNameBase, os.Getpid())
	reply, err := conn.RequestName(s.busName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("request name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("mpris name %q already taken", s.busName)
	}

	return s, nil
}

// Update pushes the latest player snapshot. Metadata/PlaybackStatus/Volume emit
// PropertiesChanged only when they actually change; Position is updated silently
// (MPRIS clients poll it, and the spec forbids signalling it).
func (s *Server) Update(n Now) {
	if s == nil || s.props == nil {
		return
	}
	// ponytail: no lock — Update is called only from the bubbletea Update goroutine.

	// Metadata: rebuild + emit only when the track identity changes.
	key := n.Title + "\x00" + n.Artist + "\x00" + n.Album + "\x00" + fmt.Sprint(n.LengthUS)
	if !n.HasTrack {
		key = ""
	}
	if key != s.lastKey {
		s.lastKey = key
		s.props.SetMust(playerIface, "Metadata", s.metadata(n))
	}

	if n.Status != s.lastStat {
		s.lastStat = n.Status
		s.props.SetMust(playerIface, "PlaybackStatus", n.Status)
	}

	if n.Volume != s.lastVol {
		s.lastVol = n.Volume
		s.props.SetMust(playerIface, "Volume", n.Volume)
	}

	// Position is read-on-demand by clients; update the stored value without a
	// PropertiesChanged signal. Skip when unchanged so a paused/stopped tick
	// (twice a second) doesn't take prop.mut to re-store an identical variant.
	if n.PositionUS != s.lastPos {
		s.lastPos = n.PositionUS
		s.props.SetMust(playerIface, "Position", n.PositionUS)
	}
}

// metadata builds the xesam/mpris metadata dict for the current track.
func (s *Server) metadata(n Now) map[string]dbus.Variant {
	if !n.HasTrack {
		s.trackPath = ""
		return map[string]dbus.Variant{}
	}
	s.trackSeq++
	s.trackPath = dbus.ObjectPath(fmt.Sprintf("/org/ytmusic/track/%d", s.trackSeq))
	m := map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(s.trackPath),
		"mpris:length":  dbus.MakeVariant(n.LengthUS),
		"xesam:title":   dbus.MakeVariant(n.Title),
	}
	if n.Artist != "" {
		m["xesam:artist"] = dbus.MakeVariant([]string{n.Artist})
	}
	if n.Album != "" {
		m["xesam:album"] = dbus.MakeVariant(n.Album)
	}
	return m
}

// Seeked emits the MPRIS Seeked signal with the new absolute position (µs).
func (s *Server) Seeked(posUS int64) {
	if s == nil || s.conn == nil {
		return
	}
	s.conn.Emit(objectPath, playerIface+".Seeked", posUS) //nolint:errcheck
}

// Close releases the bus name. The connection is the shared session bus, which
// dbus.Conn.Close must not be called on.
func (s *Server) Close() {
	if s == nil || s.conn == nil {
		return
	}
	s.conn.ReleaseName(s.busName) //nolint:errcheck
}
