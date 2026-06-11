package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gigoo25/youtube-music-tui/internal/api"
	"github.com/Gigoo25/youtube-music-tui/internal/config"
	"github.com/Gigoo25/youtube-music-tui/internal/player"
)

type view int

const (
	viewHome view = iota
	viewSearch
	viewQueue
	viewFavorites
	viewHistory
	viewAlbum
	viewArtist
	viewGenres
	viewHelp
	viewPlaylists
)

type repeatMode int

const (
	repeatOff repeatMode = iota
	repeatAll
	repeatOne
)

type focusArea int

const (
	focusSidebar focusArea = iota
	focusPanel
)

// navEntry is a single Quick Links sidebar item.
type navEntry struct {
	label string
	view  view
}

// navEntries lists the sidebar Quick Links in display order. The selected
// entry stays in sync with activeView.
var navEntries = []navEntry{
	{"Home", viewHome},
	{"Search", viewSearch},
	{"Queue", viewQueue},
	{"Favorites", viewFavorites},
	{"History", viewHistory},
	{"Playlists", viewPlaylists},
	{"Help", viewHelp},
}

// navIndexOf returns the sidebar index for a view (0 if not found).
func navIndexOf(v view) int {
	for i, e := range navEntries {
		if e.view == v {
			return i
		}
	}
	return 0
}

type model struct {
	player *player.Player
	cfg    *config.Config
	api    *api.Client
	mpris  *mprisServer // in-process MPRIS server; nil if the session bus is unavailable

	activeView  view
	focus       focusArea
	navCursor   int
	width       int
	height      int
	viewportH   int  // visible list height (set during render), for page scrolling
	pendingG    bool // first 'g' of a 'gg' (go-to-top) chord was pressed
	themeIdx    int  // index into themes of the active color theme
	genreCursor int  // selection in the random genre picker

	// home (drop-in view): Listen Again (from local history) + Quick Picks
	// (related to most-recent play, falling back to Trending). The cursor spans
	// both sections as one flat list.
	homeListenAgain []api.Track
	homeQuickPicks  []api.Track
	homeCursor      int
	homeQPLoading   bool
	homeQPErr       string
	homeLoaded      bool

	// artist view (top songs + albums for an artist) — contextual like album view
	artistName    string
	artistSongs   []api.Track
	artistAlbums  []api.AlbumRef
	artistCursor  int // spans songs then albums
	artistLoading bool
	artistErr     string

	// search (global YouTube Music search — its own view)
	searchInput        textinput.Model
	searchTyping       bool // true = editing query; false = browsing results
	searching          bool
	searchGen          int    // bumped per search; a slow older response must not clobber a newer one
	searchContinuation string // token for the next page of results ("" = none / exhausted)
	searchMoreLoading  bool   // a load-more page request is in flight
	searchResults      []api.Track
	searchCursor       int

	// playlists (saved locally; a Quick Links view)
	playlistCursor int

	// naming overlay: capturing a name for "save queue as playlist".
	naming        bool
	playlistInput textinput.Model

	// pending confirmation for a destructive action: while confirmFn is non-nil
	// the next key either confirms (y) or cancels (anything else).
	confirmPrompt string
	confirmFn     func()

	// local filter ("/"): narrows the CURRENT pane's list in-memory. filtering =
	// editing the query; filter != "" = an applied filter being browsed.
	filterInput textinput.Model
	filtering   bool
	filter      string

	// queue
	queue       []api.Track
	queueCursor int
	queuePos    int
	shuffle     bool
	repeat      repeatMode

	// favorites
	favCursor int

	// history
	historyCursor int

	// album view (the album a track belongs to)
	albumTracks  []api.Track
	albumCursor  int
	albumTitle   string
	albumLoading bool
	albumErr     string
	prevView     view // the view to return to from the album view

	// player snapshot (refreshed each tick)
	playerState player.State
	current     api.Track // the track currently loaded (value copy, not a slice ptr)
	hasCurrent  bool

	// transient status line (auto-clears after statusTTL)
	status    string
	statusErr bool
	statusAt  time.Time

	// render caches: rebuilt only when their input key changes (sidebar and
	// shortcuts bar are otherwise re-rendered from scratch every frame)
	sbKey   sidebarKey
	sbCache string
	scKey   shortcutsKey
	scCache string

	// debounced config persistence: mutations mark dirty; the tick flushes after
	// configSaveDelay so favoriting/queue churn doesn't re-marshal+write on every
	// action. main.go does a final Save() on exit, so nothing is lost on quit.
	cfgDirty   bool
	cfgDirtyAt time.Time
}

// configSaveDelay is how long config changes are batched before being written.
const configSaveDelay = 3 * time.Second

// markConfigDirty flags the in-memory config for a debounced disk write. The
// actual Save happens in the tick handler (the single Update goroutine).
func (m *model) markConfigDirty() {
	if !m.cfgDirty {
		m.cfgDirty = true
		m.cfgDirtyAt = time.Now()
	}
}

// statusTTL is how long a transient status message stays on screen.
const statusTTL = 5 * time.Second

type searchDoneMsg struct {
	tracks []api.Track
	next   string // continuation token for the next page
	gen    int    // searchGen at dispatch; stale responses are dropped
	err    error
}

// searchMoreMsg carries an appended page of search results (load-more).
type searchMoreMsg struct {
	tracks []api.Track
	next   string
	token  string // the token this page was fetched with, restored on error so retry works
	gen    int
	err    error
}

type tickMsg time.Time

type randomDoneMsg struct {
	tracks []api.Track
	err    error
}

type radioDoneMsg struct {
	tracks []api.Track
	err    error
}

// autoContinueMsg carries radio tracks fetched to keep playback going after the
// queue ran out (auto-continue). Unlike radioDoneMsg it resumes playback.
type autoContinueMsg struct {
	tracks []api.Track
	err    error
}

type albumDoneMsg struct {
	tracks []api.Track
	title  string
	err    error
}

type homeQuickPicksMsg struct {
	tracks []api.Track
	err    error
}

type artistDoneMsg struct {
	res api.ArtistResult
	err error
}

// randomSeeds drive the "play random" feature (z): a random seed is searched
// and a random result is played. No extra API endpoint required.
var randomSeeds = []string{
	"lofi", "rock", "jazz", "pop hits", "electronic", "hip hop",
	"classical", "indie", "chill", "80s", "90s", "metal", "r&b", "funk",
}

func New(p *player.Player, cfg *config.Config) *model {
	ti := textinput.New()
	ti.Placeholder = "Search YouTube Music..."
	ti.CharLimit = 200

	fi := textinput.New()
	fi.Placeholder = "filter…"
	fi.CharLimit = 100

	pi := textinput.New()
	pi.Placeholder = "playlist name"
	pi.CharLimit = 80

	client := api.NewClient()
	client.SetTimeout(api.RequestTimeout)

	// Restore the saved color theme (default to the first if unset/unknown).
	themeIdx := 0
	if i := themeIndex(cfg.Theme); i >= 0 {
		themeIdx = i
	}
	applyTheme(themes[themeIdx])

	m := &model{
		player:        p,
		cfg:           cfg,
		api:           client,
		activeView:    viewHome,
		focus:         focusPanel,
		navCursor:     navIndexOf(viewHome),
		searchInput:   ti,
		filterInput:   fi,
		playlistInput: pi,
		searchTyping:  false,
		themeIdx:      themeIdx,
		playerState:   player.State{Volume: cfg.Volume},
	}
	m.restoreSession()
	return m
}

// restoreSession reloads the queue and playback toggles saved on last exit. The
// queue is copied (not aliased) and playback is left stopped — the user resumes
// with Enter/p on the queue, so a launch never starts audio unprompted.
func (m *model) restoreSession() {
	if len(m.cfg.Queue) > 0 {
		m.queue = append([]api.Track(nil), m.cfg.Queue...)
		m.queuePos = m.cfg.QueuePos
		if m.queuePos < 0 || m.queuePos >= len(m.queue) {
			m.queuePos = 0
		}
		m.queueCursor = m.queuePos // land the cursor on the track that was playing
	}
	m.shuffle = m.cfg.Shuffle
	if m.cfg.Repeat >= int(repeatOff) && m.cfg.Repeat <= int(repeatOne) {
		m.repeat = repeatMode(m.cfg.Repeat)
	}
}

// SnapshotSession copies the live session state into the config so the next
// launch can restore it. Exported so main.go can call it on the final model
// before the exit Save (the model holds the only authoritative queue).
func (m *model) SnapshotSession() {
	m.cfg.Queue = m.queue
	m.cfg.QueuePos = m.queuePos
	m.cfg.Shuffle = m.shuffle
	m.cfg.Repeat = int(m.repeat)
}

// cycleTheme advances to the next color theme, persists it, and invalidates the
// render caches so re-themed styles take effect immediately.
func (m *model) cycleTheme() {
	m.themeIdx = (m.themeIdx + 1) % len(themes)
	t := themes[m.themeIdx]
	applyTheme(t)
	m.cfg.Theme = t.name
	m.markConfigDirty()
	m.sbCache, m.scCache = "", "" // cached sidebar/shortcuts use the old palette
	m.setStatus("theme: " + t.name)
}

func (m *model) Init() tea.Cmd {
	// Drop into Home: Listen Again is built synchronously from history; Quick
	// Picks loads asynchronously.
	m.refreshListenAgain()
	m.homeLoaded = true
	if len(m.queue) > 0 {
		m.setStatus(fmt.Sprintf("restored %d queued tracks — press space to resume", len(m.queue)))
	}
	return tea.Batch(
		textinput.Blink,
		m.nextTick(),
		m.loadHomeQuickPicks(),
	)
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// nextTick schedules the player-snapshot refresh. Fast cadence only while a track
// is actively playing (so the progress bar advances smoothly); backs off when idle
// or paused to cut steady-state wakeups ~4x.
func (m *model) nextTick() tea.Cmd {
	if m.hasCurrent && !m.playerState.Paused {
		return tickEvery(500 * time.Millisecond)
	}
	return tickEvery(2 * time.Second)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		m.playerState = m.player.State()
		var cmds []tea.Cmd
		if m.player.TrackEnded() {
			if c := m.nextTrack(); c != nil {
				cmds = append(cmds, c)
			}
		}
		// Surface stream-load/playback failures (otherwise a failed yt-dlp resolve
		// leaves the UI silently stuck looking like it's playing).
		if e := m.player.LoadError(); e != "" {
			m.setError(e)
		}
		// Auto-clear a transient status message once it has been shown long enough.
		if m.status != "" && time.Since(m.statusAt) >= statusTTL {
			m.status = ""
			m.statusErr = false
		}
		// Flush any debounced config changes (capturing the live queue too, so a
		// crash keeps a recent session, not just a clean exit).
		if m.cfgDirty && time.Since(m.cfgDirtyAt) >= configSaveDelay {
			m.SnapshotSession()
			if err := m.cfg.Save(); err != nil {
				m.setError("config save failed: " + err.Error())
			}
			m.cfgDirty = false
		}
		m.pushMPRIS()
		cmds = append(cmds, m.nextTick())
		return m, tea.Batch(cmds...)

	case mprisActionMsg:
		return m.handleMPRISAction(msg.action)

	case mprisSeekMsg:
		m.player.Seek(float64(msg.offsetUS) / 1e6)
		if m.mpris != nil {
			m.mpris.Seeked(int64((m.playerState.Position)*1e6) + msg.offsetUS)
		}
		return m, nil

	case mprisSetPosMsg:
		m.player.SeekAbs(float64(msg.posUS) / 1e6)
		if m.mpris != nil {
			m.mpris.Seeked(msg.posUS)
		}
		return m, nil

	case mprisSetVolMsg:
		m.player.SetVolume(msg.level * 100)
		return m, nil

	case autoContinueMsg:
		if msg.err != nil {
			m.setError("auto-continue failed: " + msg.err.Error())
			return m, nil
		}
		// Related lives in explore.go (not the secret-blocked ytmusic.go), so its
		// tracks are already clean. Drop the seed itself to avoid an instant repeat.
		var tracks []api.Track
		seed := ""
		if m.hasCurrent {
			seed = m.current.ID
		}
		for _, t := range msg.tracks {
			if t.ID != "" && t.ID != seed {
				tracks = append(tracks, t)
			}
		}
		if len(tracks) == 0 {
			m.setError("auto-continue: nothing found")
			return m, nil
		}
		start := len(m.queue)
		m.queue = append(m.queue, tracks...)
		m.playAt(start)
		m.setStatus(fmt.Sprintf("auto-continue: added %d tracks", len(tracks)))
		return m, m.nextTick()

	case randomDoneMsg:
		// Search (the random source) lives in the secret-blocked ytmusic.go, so its
		// results are cleaned here rather than at the API boundary.
		tracks := api.CleanTracks(msg.tracks)
		if msg.err != nil {
			m.setError("random failed: " + msg.err.Error())
		} else if len(tracks) == 0 {
			m.setError("no random track found")
		} else {
			m.playNow(tracks[rand.Intn(len(tracks))])
		}
		return m, nil

	case radioDoneMsg:
		if msg.err != nil {
			m.setError("radio failed: " + msg.err.Error())
		} else if len(msg.tracks) == 0 {
			m.setError("no related tracks found")
		} else {
			for _, t := range msg.tracks {
				m.queue = append(m.queue, t)
			}
			m.setStatus(fmt.Sprintf("radio: added %d tracks", len(msg.tracks)))
		}
		return m, nil

	case albumDoneMsg:
		m.albumLoading = false
		m.albumCursor = 0
		if msg.err != nil {
			m.albumErr = msg.err.Error()
		} else {
			m.albumErr = ""
			m.albumTracks = msg.tracks
			if msg.title != "" {
				m.albumTitle = msg.title
			}
			if len(msg.tracks) == 0 {
				m.setStatus("album not found")
			}
		}
		return m, nil

	case homeQuickPicksMsg:
		m.homeQPLoading = false
		if msg.err != nil {
			m.homeQPErr = msg.err.Error()
		} else {
			m.homeQPErr = ""
			m.homeQuickPicks = msg.tracks
		}
		if m.homeCursor >= m.homeLen() {
			m.homeCursor = max(0, m.homeLen()-1)
		}
		return m, nil

	case artistDoneMsg:
		m.artistLoading = false
		if msg.err != nil {
			m.artistErr = msg.err.Error()
		} else {
			m.artistErr = ""
			if msg.res.Name != "" {
				m.artistName = msg.res.Name
			}
			m.artistSongs = msg.res.Songs
			m.artistAlbums = msg.res.Albums
			if len(m.artistSongs) == 0 && len(m.artistAlbums) == 0 {
				m.setStatus("artist not found")
			}
		}
		m.artistCursor = 0
		return m, nil

	case searchDoneMsg:
		if msg.gen != m.searchGen {
			return m, nil // a newer search superseded this response
		}
		m.searching = false
		if msg.err != nil {
			m.setError("search failed: " + msg.err.Error())
		} else {
			m.searchResults = api.CleanTracks(msg.tracks)
			m.searchContinuation = msg.next
			m.searchCursor = 0
			if len(msg.tracks) == 0 {
				m.setStatus("no results")
			} else {
				m.setStatus(fmt.Sprintf("%d results", len(msg.tracks)))
			}
		}
		return m, nil

	case searchMoreMsg:
		if msg.gen != m.searchGen {
			return m, nil // a newer search replaced the list this page belonged to
		}
		m.searchMoreLoading = false
		if msg.err != nil {
			// Restore the consumed token so scrolling down retries the page —
			// otherwise one transient failure would end pagination for this search.
			m.searchContinuation = msg.token
			m.setError("load more failed: " + msg.err.Error())
			return m, nil
		}
		// Append the new page, skipping ids already shown.
		seen := make(map[string]bool, len(m.searchResults))
		for _, t := range m.searchResults {
			seen[t.ID] = true
		}
		for _, t := range api.CleanTracks(msg.tracks) {
			if t.ID != "" && !seen[t.ID] {
				m.searchResults = append(m.searchResults, t)
				seen[t.ID] = true
			}
		}
		m.searchContinuation = msg.next
		m.setStatus(fmt.Sprintf("%d results", len(m.searchResults)))
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Forward any other message to the active text input (e.g. paste, cursor
	// blink) so its cursor animates.
	if m.typing() {
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		return m, cmd
	}
	if m.naming {
		var cmd tea.Cmd
		m.playlistInput, cmd = m.playlistInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

// typing reports whether the user is actively editing the search query.
func (m *model) typing() bool {
	return m.activeView == viewSearch && m.searchTyping
}

// ── Local filter ──

// filterableView reports whether the active view has a list the "/" filter
// applies to.
func (m *model) filterableView() bool {
	switch m.activeView {
	case viewHome, viewQueue, viewFavorites, viewHistory,
		viewAlbum, viewArtist:
		return true
	}
	return false
}

// filterActive reports whether a filter query is currently shaping the list.
func (m *model) filterActive() bool {
	return m.filter != "" && m.filterableView()
}

func matchStr(s, q string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(q))
}

func matchTrack(t api.Track, q string) bool {
	return matchStr(t.Title, q) || matchStr(t.Artist, q)
}

// filt returns src filtered by the active query (or src unchanged when no filter).
func (m *model) filt(src []api.Track) []api.Track {
	if !m.filterActive() {
		return src
	}
	out := make([]api.Track, 0, len(src))
	for _, t := range src {
		if matchTrack(t, m.filter) {
			out = append(out, t)
		}
	}
	return out
}

// filtAlbums is filt for album refs (matches title/artist).
func (m *model) filtAlbums(src []api.AlbumRef) []api.AlbumRef {
	if !m.filterActive() {
		return src
	}
	out := make([]api.AlbumRef, 0, len(src))
	for _, a := range src {
		if matchStr(a.Title, m.filter) || matchStr(a.Artist, m.filter) {
			out = append(out, a)
		}
	}
	return out
}

// filtHistory is filt for history entries.
func (m *model) filtHistory(src []config.HistoryEntry) []config.HistoryEntry {
	if !m.filterActive() {
		return src
	}
	out := make([]config.HistoryEntry, 0, len(src))
	for _, e := range src {
		if matchTrack(e.Track, m.filter) {
			out = append(out, e)
		}
	}
	return out
}

// trackVisibleIndices maps filtered positions back to original indices in src —
// needed by views with positional semantics (Queue removal, Album/Artist
// "play from here").
func (m *model) trackVisibleIndices(src []api.Track) []int {
	out := make([]int, 0, len(src))
	for i, t := range src {
		if !m.filterActive() || matchTrack(t, m.filter) {
			out = append(out, i)
		}
	}
	return out
}

// activeCursorPtr returns a pointer to the active view's list cursor, or nil.
func (m *model) activeCursorPtr() *int {
	switch m.activeView {
	case viewHome:
		return &m.homeCursor
	case viewQueue:
		return &m.queueCursor
	case viewFavorites:
		return &m.favCursor
	case viewHistory:
		return &m.historyCursor
	case viewAlbum:
		return &m.albumCursor
	case viewArtist:
		return &m.artistCursor
	case viewPlaylists:
		return &m.playlistCursor
	}
	return nil
}

// activeFilteredLen is the number of rows currently selectable in the active view
// under the live filter.
func (m *model) activeFilteredLen() int {
	switch m.activeView {
	case viewHome:
		return len(m.filt(m.homeListenAgain)) + len(m.filt(m.homeQuickPicks))
	case viewQueue:
		return len(m.filt(m.queue))
	case viewFavorites:
		return len(m.filt(m.cfg.Favorites))
	case viewHistory:
		return len(m.filtHistory(m.cfg.History))
	case viewAlbum:
		return len(m.filt(m.albumTracks))
	case viewArtist:
		return len(m.filt(m.artistSongs)) + len(m.filtAlbums(m.artistAlbums))
	case viewPlaylists:
		return len(m.cfg.Playlists)
	}
	return 0
}

// clampActiveCursor keeps the active view's cursor within the filtered list.
func (m *model) clampActiveCursor() {
	cp := m.activeCursorPtr()
	if cp == nil {
		return
	}
	n := m.activeFilteredLen()
	if *cp >= n {
		*cp = max(0, n-1)
	}
	if *cp < 0 {
		*cp = 0
	}
}

// clearFilter drops any active filter (called when leaving a view).
func (m *model) clearFilter() {
	m.filter = ""
	m.filtering = false
	m.filterInput.SetValue("")
	m.filterInput.Blur()
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ctrl+c always quits, even while typing.
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	// A pending destructive-action confirmation captures the next key: y runs
	// the action, anything else cancels.
	if m.confirmFn != nil {
		fn, prompt := m.confirmFn, m.confirmPrompt
		m.confirmFn = nil
		m.confirmPrompt = ""
		if s := msg.String(); s == "y" || s == "Y" {
			fn()
		} else {
			m.setStatus("cancelled: " + prompt)
		}
		return m, nil
	}

	// Naming overlay (save queue as playlist) owns the keyboard until confirmed.
	if m.naming {
		switch msg.String() {
		case "enter":
			name := strings.TrimSpace(m.playlistInput.Value())
			m.naming = false
			m.playlistInput.Blur()
			if name == "" {
				m.setError("playlist name cannot be empty")
				return m, nil
			}
			m.cfg.SavePlaylist(name, m.queue)
			m.markConfigDirty()
			m.setStatus(fmt.Sprintf("saved playlist %q (%d tracks)", name, len(m.queue)))
			return m, nil
		case "esc":
			m.naming = false
			m.playlistInput.Blur()
			m.playlistInput.SetValue("")
			return m, nil
		}
		var cmd tea.Cmd
		m.playlistInput, cmd = m.playlistInput.Update(msg)
		return m, cmd
	}

	// While typing in the search box, the input owns the keyboard except for
	// the explicit control keys handled here.
	if m.typing() {
		switch msg.String() {
		case "enter":
			query := m.searchInput.Value()
			if query == "" {
				return m, nil
			}
			m.searchTyping = false
			m.searchInput.Blur()
			m.searching = true
			m.setStatus("searching...")
			return m, m.doSearch(query)
		case "esc":
			// Stop typing. If there are results, browse them; otherwise (e.g. on
			// first open with an empty query) move focus to the Quick Links sidebar.
			m.searchTyping = false
			m.searchInput.Blur()
			if len(m.searchResults) == 0 {
				m.focus = focusSidebar
				m.navCursor = navIndexOf(m.activeView)
			}
			return m, nil
		case "tab", "shift+tab":
			// Jump straight to the sidebar from the search box.
			m.searchTyping = false
			m.searchInput.Blur()
			m.focus = focusSidebar
			m.navCursor = navIndexOf(m.activeView)
			return m, nil
		}
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		return m, cmd
	}

	// While editing the local "/" filter, the filter input owns the keyboard
	// except for confirm/cancel.
	if m.filtering {
		switch msg.String() {
		case "enter":
			// Keep the filter applied; drop back to navigating the results.
			m.filtering = false
			m.filterInput.Blur()
			return m, nil
		case "esc":
			m.clearFilter()
			if cp := m.activeCursorPtr(); cp != nil {
				*cp = 0
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		m.filter = m.filterInput.Value()
		m.clampActiveCursor()
		return m, cmd
	}

	// gg chord: any key other than a second 'g' cancels a pending go-to-top.
	if msg.String() != "g" {
		m.pendingG = false
	}

	// ── Global keys (active in every view when not typing) ──

	// Playback: match Space by both string and key type so it fires reliably
	// regardless of how the terminal reports the event.
	if msg.Type == tea.KeySpace || msg.String() == " " {
		// Nothing loaded but a queue is present (a restored session, or the queue
		// ran out): space starts playback at the queue position rather than
		// toggling a paused mpv that has no file. So resuming is just "press play".
		if !m.hasCurrent && len(m.queue) > 0 {
			idx := m.queuePos
			if idx < 0 || idx >= len(m.queue) {
				idx = 0
			}
			m.playAt(idx)
		} else {
			m.player.PlayPause()
		}
		return m, nil
	}

	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "n":
		return m, m.nextTrack()
	case "b":
		m.prevTrack()
		return m, nil
	case ">", "shift+right":
		m.player.Seek(10)
		return m, nil
	case "<", "shift+left":
		m.player.Seek(-10)
		return m, nil
	case "+", "=":
		m.player.VolumeUp()
		return m, nil
	case "-":
		m.player.VolumeDown()
		return m, nil
	case "m":
		m.player.ToggleMute()
		if m.playerState.Muted {
			m.setStatus("unmuted")
		} else {
			m.setStatus("muted")
		}
		return m, nil
	case "s":
		m.shuffle = !m.shuffle
		if m.shuffle {
			m.setStatus("shuffle on")
		} else {
			m.setStatus("shuffle off")
		}
		return m, nil
	case "r":
		m.repeat = (m.repeat + 1) % 3
		m.setStatus([]string{"repeat off", "repeat all", "repeat one"}[m.repeat])
		return m, nil
	case "f":
		m.toggleFavoriteContext()
		return m, nil
	case "a":
		return m, m.goToAlbum()
	case "A":
		return m, m.goToArtist()
	case "/":
		// Local filter of the current pane. In the global Search view, "/" instead
		// re-focuses the query box (global search lives on the sidebar / key 2).
		if m.filterableView() {
			m.filtering = true
			m.filterInput.Focus()
			return m, textinput.Blink
		}
		if m.activeView == viewSearch {
			m.searchTyping = true
			m.searchInput.Focus()
			return m, textinput.Blink
		}
		return m, nil
	case "tab":
		// Toggle focus between sidebar and panel.
		if m.focus == focusSidebar {
			m.focus = focusPanel
		} else {
			m.focus = focusSidebar
		}
		return m, nil
	case "shift+tab":
		if m.focus == focusSidebar {
			m.focus = focusPanel
		} else {
			m.focus = focusSidebar
		}
		return m, nil
	case "1":
		m.activateView(viewHome)
		return m, nil
	case "2":
		m.activateView(viewSearch)
		return m, nil
	case "3":
		m.activateView(viewQueue)
		return m, nil
	case "4":
		m.activateView(viewFavorites)
		return m, nil
	case "5":
		m.activateView(viewHistory)
		return m, nil
	case "6":
		m.activateView(viewPlaylists)
		return m, nil
	case "S":
		// Save the current queue as a named playlist.
		if len(m.queue) == 0 {
			m.setError("queue is empty — nothing to save")
			return m, nil
		}
		m.naming = true
		m.playlistInput.SetValue("")
		m.playlistInput.Focus()
		return m, textinput.Blink
	case "z":
		m.openGenres()
		return m, nil
	case "R":
		return m, m.startRadio()
	case "C":
		m.cfg.AutoContinue = !m.cfg.AutoContinue
		m.markConfigDirty()
		if m.cfg.AutoContinue {
			m.setStatus("auto-continue on")
		} else {
			m.setStatus("auto-continue off")
		}
		return m, nil
	case "T":
		m.cycleTheme()
		return m, nil
	case "?":
		if m.activeView == viewHelp {
			m.activateView(viewHome)
		} else {
			m.activateView(viewHelp)
		}
		return m, nil
	}

	// ── Sidebar focus owns navigation ──
	if m.focus == focusSidebar {
		return m.handleSidebarKey(msg)
	}

	// ── Panel focus: return-to-sidebar keys ──
	switch msg.String() {
	case "h", "left":
		m.focus = focusSidebar
		m.navCursor = navIndexOf(m.activeView)
		return m, nil
	case "esc":
		// An applied local filter clears first, before any navigation.
		if m.filter != "" {
			m.clearFilter()
			if cp := m.activeCursorPtr(); cp != nil {
				*cp = 0
			}
			return m, nil
		}
		// The album, artist and genre-picker views are contextual: esc returns to
		// the screen they were opened from.
		if m.activeView == viewAlbum || m.activeView == viewArtist || m.activeView == viewGenres {
			m.clearFilter()
			m.activeView = m.prevView
			m.navCursor = navIndexOf(m.activeView)
			m.focus = focusPanel
			return m, nil
		}
		// In search results, esc clears results first; a second esc returns focus.
		if m.activeView == viewSearch && len(m.searchResults) > 0 {
			m.searchResults = nil
			m.searchCursor = 0
			m.searchInput.SetValue("")
			return m, nil
		}
		m.focus = focusSidebar
		m.navCursor = navIndexOf(m.activeView)
		return m, nil
	}

	// ── Panel focus: view-specific keys ──
	switch m.activeView {
	case viewHome:
		return m.handleHomeKey(msg)
	case viewSearch:
		return m.handleSearchKey(msg)
	case viewQueue:
		return m.handleQueueKey(msg)
	case viewFavorites:
		return m.handleFavKey(msg)
	case viewHistory:
		return m.handleHistoryKey(msg)
	case viewAlbum:
		return m.handleAlbumKey(msg)
	case viewArtist:
		return m.handleArtistKey(msg)
	case viewGenres:
		return m.handleGenresKey(msg)
	case viewPlaylists:
		return m.handlePlaylistsKey(msg)
	case viewHelp:
		return m, nil
	}
	return m, nil
}

// goToAlbum opens the album the selected/playing track belongs to and loads it.
func (m *model) goToAlbum() tea.Cmd {
	t := m.contextTrack()
	if t == nil {
		m.setError("no track selected")
		return nil
	}
	if m.activeView != viewAlbum {
		m.prevView = m.activeView
	}
	m.clearFilter()
	m.activeView = viewAlbum
	m.focus = focusPanel
	m.albumLoading = true
	m.albumErr = ""
	m.albumTracks = nil
	m.albumCursor = 0
	name := t.Album
	if name == "" {
		name = t.Title
	}
	m.albumTitle = name
	// The album panel shows its own "Loading…" — no transient status needed.

	album, artist := t.Album, t.Artist
	if album == "" {
		album = t.Title
	}
	client := m.api
	return func() tea.Msg {
		tracks, title, err := api.AlbumByQuery(client, album, artist)
		return albumDoneMsg{tracks: tracks, title: title, err: err}
	}
}

// pageSize returns the number of list rows that fit in the panel, used for
// half/full-page scrolling. Falls back to a sane minimum before the first render.
func (m *model) pageSize() int {
	n := m.viewportH - 1 // minus the list header row
	if n < 1 {
		n = 10
	}
	return n
}

// vimMove applies vim-style list navigation to a cursor over `total` items and
// reports whether the key was a navigation key. Handles j/k (and arrows), ctrl+d/
// ctrl+u (half page), ctrl+f/ctrl+b (full page), G (bottom), and the gg chord
// (top) via m.pendingG. The returned cursor is clamped to [0, total-1].
func (m *model) vimMove(key string, cursor, total int) (int, bool) {
	page := m.pageSize()
	half := page / 2
	if half < 1 {
		half = 1
	}
	switch key {
	case "j", "down":
		cursor++
	case "k", "up":
		cursor--
	case "ctrl+d":
		cursor += half
	case "ctrl+u":
		cursor -= half
	case "ctrl+f", "pgdown":
		cursor += page
	case "ctrl+b", "pgup":
		cursor -= page
	case "G", "end":
		cursor = total - 1
	case "home":
		cursor = 0
	case "g":
		if !m.pendingG {
			m.pendingG = true // first 'g'; wait for the second
			return cursor, true
		}
		cursor = 0
	default:
		return cursor, false
	}
	m.pendingG = false
	if cursor > total-1 {
		cursor = total - 1
	}
	if cursor < 0 {
		cursor = 0
	}
	return cursor, true
}

func (m *model) handleAlbumKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	vis := m.trackVisibleIndices(m.albumTracks)
	if nc, ok := m.vimMove(msg.String(), m.albumCursor, len(vis)); ok {
		m.albumCursor = nc
		return m, nil
	}
	switch msg.String() {
	case "enter":
		// Queue the selected track (consistent with every other list view).
		if m.albumCursor < len(vis) {
			m.enqueue(m.albumTrackAt(vis[m.albumCursor]))
		}
	case "p":
		// Play the selected track now.
		if m.albumCursor < len(vis) {
			m.playNow(m.albumTrackAt(vis[m.albumCursor]))
		}
	case "e":
		// Append the whole album to the queue without disturbing playback.
		m.enqueueAll(m.withAlbum(m.albumTracks))
	}
	return m, nil
}

// albumTrackAt returns the album track at idx with the (implied) album name
// filled in, so the now-playing bar shows it.
func (m *model) albumTrackAt(idx int) api.Track {
	t := m.albumTracks[idx]
	if t.Album == "" {
		t.Album = m.albumTitle
	}
	return t
}

// withAlbum returns a copy of ts with the (implied) album name filled in, so the
// now-playing bar shows it. Album rows carry the artist but not the album name.
func (m *model) withAlbum(ts []api.Track) []api.Track {
	out := append([]api.Track(nil), ts...)
	for i := range out {
		if out[i].Album == "" {
			out[i].Album = m.albumTitle
		}
	}
	return out
}

// enqueueAll appends every track to the queue. If nothing is playing it starts
// at the first appended track; otherwise playback is undisturbed and the next
// few tracks are warmed.
func (m *model) enqueueAll(ts []api.Track) {
	if len(ts) == 0 {
		m.setError("nothing to queue")
		return
	}
	start := len(m.queue)
	m.queue = append(m.queue, ts...)
	if !m.hasCurrent {
		m.playAt(start)
	} else {
		m.prefetchNext()
	}
	m.setStatus(fmt.Sprintf("queued %d tracks", len(ts)))
}

// ── Home view ──

// homeSections returns the (filter-aware) Listen Again and Quick Picks lists.
func (m *model) homeSections() (la, qp []api.Track) {
	return m.filt(m.homeListenAgain), m.filt(m.homeQuickPicks)
}

// homeLen is the number of selectable tracks across both home sections.
func (m *model) homeLen() int {
	la, qp := m.homeSections()
	return len(la) + len(qp)
}

// homeTrackAt resolves the track at flat index i (Listen Again then Quick Picks).
func (m *model) homeTrackAt(i int) (api.Track, bool) {
	la, qp := m.homeSections()
	if i < 0 {
		return api.Track{}, false
	}
	if i < len(la) {
		return la[i], true
	}
	i -= len(la)
	if i < len(qp) {
		return qp[i], true
	}
	return api.Track{}, false
}

// refreshListenAgain rebuilds the Listen Again section from recent history,
// deduped by track id (newest first), capped.
func (m *model) refreshListenAgain() {
	const limit = 15
	seen := make(map[string]struct{}, limit)
	out := m.homeListenAgain[:0] // reuse backing array across refreshes
	for _, h := range m.cfg.History {
		if h.Track.ID == "" {
			continue
		}
		if _, dup := seen[h.Track.ID]; dup {
			continue
		}
		seen[h.Track.ID] = struct{}{}
		out = append(out, h.Track)
		if len(out) >= limit {
			break
		}
	}
	m.homeListenAgain = out
}

// loadHomeQuickPicks fetches Quick Picks: tracks related to the most-recent play,
// falling back to Trending when there's no history (or no related results).
func (m *model) loadHomeQuickPicks() tea.Cmd {
	m.homeQPLoading = true
	m.homeQPErr = ""
	seed := ""
	if len(m.cfg.History) > 0 {
		seed = m.cfg.History[0].Track.ID
	}
	client := m.api
	return func() tea.Msg {
		var tracks []api.Track
		var err error
		if seed != "" {
			tracks, err = client.Related(seed)
		}
		if err == nil && len(tracks) == 0 {
			tracks, err = client.Trending()
		}
		return homeQuickPicksMsg{tracks: tracks, err: err}
	}
}

func (m *model) handleHomeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if nc, ok := m.vimMove(msg.String(), m.homeCursor, m.homeLen()); ok {
		m.homeCursor = nc
		return m, nil
	}
	switch msg.String() {
	case "enter":
		if t, ok := m.homeTrackAt(m.homeCursor); ok {
			m.enqueue(t)
		}
	case "p":
		if t, ok := m.homeTrackAt(m.homeCursor); ok {
			m.playNow(t)
		}
	}
	return m, nil
}

// ── Artist view ──

// goToArtist opens the artist view for the selected/playing track's artist.
func (m *model) goToArtist() tea.Cmd {
	t := m.contextTrack()
	if t == nil || t.Artist == "" {
		m.setError("no artist for selection")
		return nil
	}
	if m.activeView != viewArtist {
		m.prevView = m.activeView
	}
	m.clearFilter()
	m.activeView = viewArtist
	m.focus = focusPanel
	m.artistLoading = true
	m.artistErr = ""
	m.artistSongs = nil
	m.artistAlbums = nil
	m.artistCursor = 0
	m.artistName = firstArtistOf(t.Artist)

	client := m.api
	name := m.artistName
	return func() tea.Msg {
		res, err := api.ArtistByQuery(client, name)
		return artistDoneMsg{res: res, err: err}
	}
}

func (m *model) handleArtistKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	songVis := m.trackVisibleIndices(m.artistSongs)
	albums := m.filtAlbums(m.artistAlbums)
	total := len(songVis) + len(albums)
	if nc, ok := m.vimMove(msg.String(), m.artistCursor, total); ok {
		m.artistCursor = nc
		return m, nil
	}
	switch msg.String() {
	case "enter":
		// On a song, queue it (consistent with other views); on an album, open it.
		if m.artistCursor < len(songVis) {
			m.enqueue(m.artistSongAt(songVis[m.artistCursor]))
		} else if ai := m.artistCursor - len(songVis); ai < len(albums) {
			return m, m.openAlbumByID(albums[ai])
		}
	case "p":
		// Play the selected song now (albums have no play-now; enter opens them).
		if m.artistCursor < len(songVis) {
			m.playNow(m.artistSongAt(songVis[m.artistCursor]))
		}
	case "e":
		// Append the artist's top songs to the queue.
		m.enqueueAll(m.withArtist(m.artistSongs))
	}
	return m, nil
}

// artistSongAt returns the artist song at idx with the (implied) artist name
// filled in.
func (m *model) artistSongAt(idx int) api.Track {
	t := m.artistSongs[idx]
	if t.Artist == "" {
		t.Artist = m.artistName
	}
	return t
}

// withArtist returns a copy of ts with the (implied) artist name filled in.
func (m *model) withArtist(ts []api.Track) []api.Track {
	out := append([]api.Track(nil), ts...)
	for i := range out {
		if out[i].Artist == "" {
			out[i].Artist = m.artistName
		}
	}
	return out
}

// openAlbumByID opens an album (by browse id) from the artist view; esc returns
// to the artist.
func (m *model) openAlbumByID(a api.AlbumRef) tea.Cmd {
	m.clearFilter()
	m.prevView = viewArtist
	m.activeView = viewAlbum
	m.focus = focusPanel
	m.albumLoading = true
	m.albumErr = ""
	m.albumTracks = nil
	m.albumCursor = 0
	m.albumTitle = a.Title

	client := m.api
	id := a.ID
	return func() tea.Msg {
		tracks, title, err := api.AlbumByID(client, id)
		return albumDoneMsg{tracks: tracks, title: title, err: err}
	}
}

// firstArtistOf returns the primary artist from a possibly multi-artist byline.
func firstArtistOf(s string) string {
	for _, sep := range []string{" & ", ", ", " feat", " · ", " x "} {
		if i := strings.Index(s, sep); i > 0 {
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

// openGenres opens the random-genre picker.
func (m *model) openGenres() {
	if m.activeView != viewGenres {
		m.prevView = m.activeView
	}
	m.clearFilter()
	m.activeView = viewGenres
	m.focus = focusPanel
	m.genreCursor = 0
}

// handleGenresKey drives the random-genre picker. Row 0 is "Any" (a random
// genre); the rest are the seeds. Selecting one plays a random song from it and
// closes the picker.
func (m *model) handleGenresKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if nc, ok := m.vimMove(msg.String(), m.genreCursor, len(randomSeeds)+1); ok {
		m.genreCursor = nc
		return m, nil
	}
	switch msg.String() {
	case "enter":
		var cmd tea.Cmd
		if m.genreCursor == 0 {
			cmd = m.playRandom()
		} else {
			cmd = m.playRandomGenre(randomSeeds[m.genreCursor-1])
		}
		m.activeView = m.prevView
		m.navCursor = navIndexOf(m.activeView)
		return m, cmd
	}
	return m, nil
}

// playRandom searches a random seed genre and plays a random result.
func (m *model) playRandom() tea.Cmd {
	return m.playRandomGenre(randomSeeds[rand.Intn(len(randomSeeds))])
}

// playRandomGenre searches the given genre and plays a random result.
func (m *model) playRandomGenre(seed string) tea.Cmd {
	m.setStatus("finding a random " + seed + " song...")
	client := m.api
	return func() tea.Msg {
		tracks, err := client.SearchSongs(seed)
		return randomDoneMsg{tracks: tracks, err: err}
	}
}

// startRadio enqueues tracks related to the currently playing song.
func (m *model) startRadio() tea.Cmd {
	if !m.hasCurrent {
		m.setError("nothing playing to start radio from")
		return nil
	}
	id := m.current.ID
	m.setStatus("starting radio...")
	client := m.api
	return func() tea.Msg {
		tracks, err := client.Related(id)
		return radioDoneMsg{tracks: tracks, err: err}
	}
}

func (m *model) handleHistoryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	hist := m.filtHistory(m.cfg.History)
	if nc, ok := m.vimMove(msg.String(), m.historyCursor, len(hist)); ok {
		m.historyCursor = nc
		return m, nil
	}
	switch msg.String() {
	case "enter":
		if m.historyCursor < len(hist) {
			m.enqueue(hist[m.historyCursor].Track)
		}
	case "p":
		if m.historyCursor < len(hist) {
			m.playNow(hist[m.historyCursor].Track)
		}
	case "c":
		if len(m.cfg.History) == 0 {
			m.setStatus("history is already empty")
			return m, nil
		}
		m.confirmPrompt = fmt.Sprintf("clear all %d history entries?", len(m.cfg.History))
		m.confirmFn = func() {
			m.cfg.History = nil
			m.historyCursor = 0
			// Listen Again is built from history — rebuild it so Home matches.
			m.refreshListenAgain()
			if m.homeCursor >= m.homeLen() {
				m.homeCursor = 0
			}
			m.markConfigDirty()
			m.setStatus("history cleared")
		}
	}
	return m, nil
}

func (m *model) handlePlaylistsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pls := m.cfg.Playlists
	if nc, ok := m.vimMove(msg.String(), m.playlistCursor, len(pls)); ok {
		m.playlistCursor = nc
		return m, nil
	}
	if m.playlistCursor >= len(pls) {
		return m, nil
	}
	switch msg.String() {
	case "enter", "p":
		// Replace the queue with the playlist and start playing.
		m.loadPlaylist(pls[m.playlistCursor], true)
	case "e":
		// Append the playlist to the queue without disturbing playback.
		m.loadPlaylist(pls[m.playlistCursor], false)
	case "d", "x":
		name := pls[m.playlistCursor].Name
		m.cfg.DeletePlaylist(name)
		m.markConfigDirty()
		m.clampActiveCursor()
		m.setStatus("deleted playlist: " + name)
	}
	return m, nil
}

// loadPlaylist plays (replace == true) or appends (replace == false) a saved
// playlist's tracks. Replacing starts playback from the top.
func (m *model) loadPlaylist(pl config.Playlist, replace bool) {
	if len(pl.Tracks) == 0 {
		m.setError("playlist is empty")
		return
	}
	if replace {
		m.queue = append([]api.Track(nil), pl.Tracks...)
		m.queueCursor = 0
		m.playAt(0)
		m.setStatus("playing playlist: " + pl.Name)
		return
	}
	m.enqueueAll(pl.Tracks)
}

// activateView switches to v, syncs the sidebar selection, and moves focus to
// the panel. Used by quick-jump keys and sidebar activation.
func (m *model) activateView(v view) {
	m.clearFilter()
	m.activeView = v
	m.navCursor = navIndexOf(v)
	m.focus = focusPanel
	if v == viewHome {
		// Keep Listen Again current with recent plays each time Home is opened.
		m.refreshListenAgain()
		if m.homeCursor >= m.homeLen() {
			m.homeCursor = 0
		}
	}
}

// handleSidebarKey processes navigation while the Quick Links sidebar is focused.
func (m *model) handleSidebarKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if nc, ok := m.vimMove(msg.String(), m.navCursor, len(navEntries)); ok {
		m.navCursor = nc
		return m, nil
	}
	switch msg.String() {
	case "enter", "l", "right":
		target := navEntries[m.navCursor].view
		m.activateView(target)
		if target == viewSearch {
			m.searchTyping = true
			m.searchInput.Focus()
			return m, textinput.Blink
		}
		return m, nil
	}
	return m, nil
}

func (m *model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if nc, ok := m.vimMove(msg.String(), m.searchCursor, len(m.searchResults)); ok {
		m.searchCursor = nc
		// Reaching the end of the loaded results pulls in the next page.
		if nc >= len(m.searchResults)-1 && m.searchContinuation != "" {
			return m, m.loadMoreSearch()
		}
		return m, nil
	}
	switch msg.String() {
	case "enter":
		if len(m.searchResults) > 0 {
			m.enqueue(m.searchResults[m.searchCursor])
		}
	case "p":
		if len(m.searchResults) > 0 {
			m.playNow(m.searchResults[m.searchCursor])
		}
	case "e":
		// Append every loaded result to the queue.
		m.enqueueAll(m.searchResults)
	case "esc":
		// Already browsing results — clear them.
		m.searchResults = nil
		m.searchCursor = 0
		m.searchContinuation = ""
		m.searchMoreLoading = false
		m.searchInput.SetValue("")
	}
	return m, nil
}

func (m *model) handleQueueKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// vis maps filtered rows back to indices in the full queue (identity when no
	// filter is applied).
	vis := m.trackVisibleIndices(m.queue)
	if nc, ok := m.vimMove(msg.String(), m.queueCursor, len(vis)); ok {
		m.queueCursor = nc
		return m, nil
	}
	switch msg.String() {
	case "J":
		if !m.filterActive() {
			m.moveQueueItem(m.queueCursor, m.queueCursor+1)
		}
	case "K":
		if !m.filterActive() {
			m.moveQueueItem(m.queueCursor, m.queueCursor-1)
		}
	case "enter", "p":
		if m.queueCursor < len(vis) {
			m.queuePos = vis[m.queueCursor]
			m.playAt(m.queuePos)
		}
	case "d", "x":
		if m.queueCursor < len(vis) {
			removed := vis[m.queueCursor]
			m.queue = append(m.queue[:removed], m.queue[removed+1:]...)
			// Keep queuePos pointing at the same playing track.
			switch {
			case removed < m.queuePos:
				m.queuePos--
			case removed == m.queuePos:
				// The currently-playing entry was removed: it keeps playing, but it
				// is no longer in the queue, so drop the now-playing marker. Step
				// queuePos back so the next advance plays the track that shifted
				// into the removed slot instead of skipping it (-1 is fine: playAt
				// rejects it and nextTrack's queuePos+1 lands on index 0).
				m.hasCurrent = false
				m.queuePos--
			}
			m.clampActiveCursor() // keep cursor in range of the (refiltered) list
			m.prefetchNext()      // the upcoming tracks may have shifted — re-warm them
			m.setStatus("removed from queue")
		}
	case ".":
		// Jump the cursor to the now-playing track.
		if m.hasCurrent {
			for i, orig := range vis {
				if orig == m.queuePos {
					m.queueCursor = i
					break
				}
			}
		}
	case "c":
		m.queue = nil
		m.queueCursor = 0
		m.queuePos = 0
		m.player.Stop()
		m.hasCurrent = false
		m.setStatus("queue cleared")
	}
	return m, nil
}

// moveQueueItem swaps the track at from with the one at to, keeping the cursor on
// the moved track and queuePos pointing at the still-playing entry. No-op if
// either index is out of range.
func (m *model) moveQueueItem(from, to int) {
	if from < 0 || from >= len(m.queue) || to < 0 || to >= len(m.queue) {
		return
	}
	m.queue[from], m.queue[to] = m.queue[to], m.queue[from]
	switch m.queuePos {
	case from:
		m.queuePos = to
	case to:
		m.queuePos = from
	}
	m.queueCursor = to
	// Reordering changes what plays next — warm the new upcoming tracks.
	m.prefetchNext()
}

func (m *model) handleFavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	favs := m.filt(m.cfg.Favorites)
	if nc, ok := m.vimMove(msg.String(), m.favCursor, len(favs)); ok {
		m.favCursor = nc
		return m, nil
	}
	switch msg.String() {
	case "enter":
		if m.favCursor < len(favs) {
			m.enqueue(favs[m.favCursor])
		}
	case "p":
		if m.favCursor < len(favs) {
			m.playNow(favs[m.favCursor])
		}
	case "d":
		// Remove from favorites (f is the global toggle handled above).
		if m.favCursor < len(favs) {
			m.cfg.ToggleFavorite(favs[m.favCursor])
			m.markConfigDirty()
			m.clampActiveCursor()
			m.setStatus("removed from favorites")
		}
	}
	return m, nil
}

// toggleFavoriteContext favorites/unfavorites the track relevant to the active
// view (selected list entry, or the currently playing track on Home/Help).
func (m *model) toggleFavoriteContext() {
	t := m.contextTrack()
	if t == nil {
		return
	}
	added := m.cfg.ToggleFavorite(*t)
	m.markConfigDirty()
	if added {
		m.setStatus("favorited: " + t.Title)
	} else {
		m.setStatus("unfavorited: " + t.Title)
		if m.activeView == viewFavorites && m.favCursor >= len(m.cfg.Favorites) && m.favCursor > 0 {
			m.favCursor--
		}
	}
}

// contextTrack resolves the track the user is currently acting on, by view.
func (m *model) contextTrack() *api.Track {
	switch m.activeView {
	case viewHome:
		if t, ok := m.homeTrackAt(m.homeCursor); ok {
			return &t
		}
	case viewArtist:
		if songs := m.filt(m.artistSongs); m.artistCursor < len(songs) {
			t := songs[m.artistCursor]
			return &t
		}
	case viewSearch:
		if m.searchCursor < len(m.searchResults) {
			t := m.searchResults[m.searchCursor]
			return &t
		}
	case viewQueue:
		if q := m.filt(m.queue); m.queueCursor < len(q) {
			t := q[m.queueCursor]
			return &t
		}
	case viewFavorites:
		if favs := m.filt(m.cfg.Favorites); m.favCursor < len(favs) {
			t := favs[m.favCursor]
			return &t
		}
	case viewHistory:
		if hist := m.filtHistory(m.cfg.History); m.historyCursor < len(hist) {
			t := hist[m.historyCursor].Track
			return &t
		}
	case viewAlbum:
		if tracks := m.filt(m.albumTracks); m.albumCursor < len(tracks) {
			t := tracks[m.albumCursor]
			return &t
		}
	}
	// Fall back to the currently playing track (Help and empty lists).
	if m.hasCurrent {
		t := m.current
		return &t
	}
	return nil
}

// ── Action methods ──

func (m *model) enqueue(t api.Track) {
	m.queue = append(m.queue, t)
	m.setStatus("queued: " + t.Title)
	if !m.hasCurrent {
		m.playAt(len(m.queue) - 1)
		return
	}
	// Warm the newly queued track so a queue-then-play flow is instant.
	if m.player != nil {
		m.player.Prefetch(t.ID)
	}
}

func (m *model) playNow(t api.Track) {
	m.queue = append([]api.Track{t}, m.queue...)
	if m.queueCursor > 0 {
		m.queueCursor++
	}
	m.playAt(0)
}

func (m *model) playAt(idx int) {
	if idx < 0 || idx >= len(m.queue) {
		return
	}
	m.queuePos = idx
	t := m.queue[idx]
	m.current = t
	m.hasCurrent = true
	// Reset the cached position/duration so the now-bar and MPRIS don't briefly
	// show the previous track's values before the next tick refreshes them.
	m.playerState.Position = 0
	m.playerState.Duration = 0
	// Set MPRIS title so shells (noctalia/playerctl) show the song, not the URL.
	title := t.Title
	if t.Artist != "" {
		title += " — " + t.Artist
	}
	m.player.SetTitle(title)
	m.player.Load(t.ID)
	// No transient "loading" status — the now-playing bar shows load state.

	// Warm the next track's stream URL so auto-advance is instant.
	m.prefetchNext()

	// Record the play in history (newest first); persistence is debounced.
	m.cfg.AddHistory(t)
	m.markConfigDirty()
	m.historyCursor = 0

	// Publish the new track to MPRIS immediately (don't wait for the next tick).
	m.pushMPRIS()
}

// prefetchAhead is how many upcoming tracks to warm. 1 covers auto-advance; a
// few more absorb a manual skip-forward without resolving the whole queue (URLs
// expire, and the queue may be reordered/cleared before they're reached).
const prefetchAhead = 3

// prefetchNext resolves the stream URLs of the next few tracks so upcoming Loads
// are cache hits. Skipped under shuffle (unpredictable) and stops at the end of
// the queue unless repeatAll will wrap.
func (m *model) prefetchNext() {
	if m.player == nil || m.shuffle || len(m.queue) == 0 {
		return
	}
	n := len(m.queue)
	for i := 1; i <= prefetchAhead; i++ {
		idx := m.queuePos + i
		if idx >= n {
			if m.repeat != repeatAll {
				return
			}
			idx %= n // wrap for repeatAll
		}
		if idx == m.queuePos {
			return // wrapped fully around a queue smaller than prefetchAhead
		}
		m.player.Prefetch(m.queue[idx].ID)
	}
}

// nextTrack advances playback. It returns a non-nil tea.Cmd only when the queue
// has run out and auto-continue is on, in which case the cmd fetches radio tracks
// to keep playing.
func (m *model) nextTrack() tea.Cmd {
	if len(m.queue) == 0 {
		return nil
	}
	switch m.repeat {
	case repeatOne:
		if m.hasCurrent {
			m.playAt(m.queuePos)
		} else {
			// The playing entry was removed from the queue (queuePos may be -1);
			// resume with the track that shifted into its slot.
			m.playAt(m.queuePos + 1)
		}
	case repeatAll:
		if m.shuffle {
			m.playAt(rand.Intn(len(m.queue)))
		} else {
			m.playAt((m.queuePos + 1) % len(m.queue))
		}
	default:
		if m.shuffle {
			m.playAt(rand.Intn(len(m.queue)))
		} else if m.queuePos+1 < len(m.queue) {
			m.playAt(m.queuePos + 1)
		} else if m.cfg.AutoContinue && m.hasCurrent {
			return m.continueRadio()
		} else {
			m.hasCurrent = false
			m.setStatus("queue ended")
		}
	}
	return nil
}

// continueRadio fetches tracks related to the just-finished track so playback
// keeps going after the queue empties (auto-continue). hasCurrent stays true so
// the seed survives until the related tracks arrive.
func (m *model) continueRadio() tea.Cmd {
	seed := m.current.ID
	m.setStatus("auto-continue: finding more…")
	client := m.api
	return func() tea.Msg {
		tracks, err := client.Related(seed)
		return autoContinueMsg{tracks: tracks, err: err}
	}
}

func (m *model) prevTrack() {
	if len(m.queue) == 0 {
		return
	}
	// Restart current track if more than 3s in; otherwise go to previous.
	if m.playerState.Position > 3 {
		m.player.Seek(-m.playerState.Position)
		return
	}
	if m.queuePos > 0 {
		m.playAt(m.queuePos - 1)
	} else {
		m.player.Seek(-m.playerState.Position)
	}
}

func (m *model) doSearch(query string) tea.Cmd {
	m.searchGen++
	m.searchContinuation = ""
	m.searchMoreLoading = false
	gen := m.searchGen
	client := m.api
	return func() tea.Msg {
		// SearchSongsPage filters to the Songs tab so results are the official
		// audio (ATV) versions with proper title/artist/album, not music videos,
		// and returns a continuation token for lazily loading further pages.
		tracks, next, err := client.SearchSongsPage(query, "")
		return searchDoneMsg{tracks: tracks, next: next, gen: gen, err: err}
	}
}

// loadMoreSearch fetches the next page of search results and appends them. It is
// triggered when the cursor reaches the end of the current results.
func (m *model) loadMoreSearch() tea.Cmd {
	token := m.searchContinuation
	if token == "" || m.searchMoreLoading {
		return nil
	}
	m.searchContinuation = "" // consume; the response refills it (prevents double-fire)
	m.searchMoreLoading = true
	gen := m.searchGen
	client := m.api
	return func() tea.Msg {
		tracks, next, err := client.SearchSongsPage("", token)
		return searchMoreMsg{tracks: tracks, next: next, token: token, gen: gen, err: err}
	}
}

func (m *model) setStatus(s string) {
	m.status = s
	m.statusErr = false
	m.statusAt = time.Now()
}

func (m *model) setError(s string) {
	m.status = s
	m.statusErr = true
	m.statusAt = time.Now()
}
