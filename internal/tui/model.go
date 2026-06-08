package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/rob/ytmusic/internal/api"
	"github.com/rob/ytmusic/internal/config"
	"github.com/rob/ytmusic/internal/player"
)

type view int

const (
	viewHome view = iota
	viewSearch
	viewQueue
	viewFavorites
	viewHistory
	viewTrending
	viewNewReleases
	viewExplore
	viewAlbum
	viewArtist
	viewGenres
	viewHelp
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
	{"Trending", viewTrending},
	{"New Releases", viewNewReleases},
	{"Explore", viewExplore},
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
	searchInput   textinput.Model
	searchTyping  bool // true = editing query; false = browsing results
	searching     bool
	searchResults []api.Track
	searchCursor  int

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

	// async browse views (Trending / New Releases / Explore)
	browse map[view]*browseState

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
	err    error
}

type tickMsg time.Time

// browseState holds the async-loaded contents of a browse view (Trending,
// New Releases, Explore). Loaded lazily the first time the view is opened.
type browseState struct {
	title   string
	tracks  []api.Track
	cursor  int
	loading bool
	loaded  bool
	err     string
	load    func(*api.Client) ([]api.Track, error)
}

type browseLoadedMsg struct {
	view   view
	tracks []api.Track
	err    error
}

type randomDoneMsg struct {
	tracks []api.Track
	err    error
}

type radioDoneMsg struct {
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

	client := api.NewClient()
	client.SetTimeout(api.RequestTimeout)

	// Restore the saved color theme (default to the first if unset/unknown).
	themeIdx := 0
	if i := themeIndex(cfg.Theme); i >= 0 {
		themeIdx = i
	}
	applyTheme(themes[themeIdx])

	return &model{
		player:       p,
		cfg:          cfg,
		api:          client,
		activeView:   viewHome,
		focus:        focusPanel,
		navCursor:    navIndexOf(viewHome),
		searchInput:  ti,
		filterInput:  fi,
		searchTyping: false,
		themeIdx:     themeIdx,
		playerState:  player.State{Volume: cfg.Volume},
		browse: map[view]*browseState{
			viewTrending:    {title: "Trending", load: (*api.Client).Trending},
			viewNewReleases: {title: "New Releases", load: (*api.Client).NewReleases},
			viewExplore:     {title: "Explore", load: (*api.Client).Explore},
		},
	}
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
		if m.player.TrackEnded() {
			m.nextTrack()
		}
		// Auto-clear a transient status message once it has been shown long enough.
		if m.status != "" && time.Since(m.statusAt) >= statusTTL {
			m.status = ""
			m.statusErr = false
		}
		// Flush any debounced config changes.
		if m.cfgDirty && time.Since(m.cfgDirtyAt) >= configSaveDelay {
			m.cfg.Save()
			m.cfgDirty = false
		}
		// Lazily load the active browse view the first time it's opened.
		if bs, ok := m.browse[m.activeView]; ok && !bs.loaded && !bs.loading {
			return m, tea.Batch(m.nextTick(), m.loadBrowse(m.activeView))
		}
		return m, m.nextTick()

	case browseLoadedMsg:
		if bs, ok := m.browse[msg.view]; ok {
			bs.loading = false
			bs.loaded = true
			bs.cursor = 0
			if msg.err != nil {
				bs.err = msg.err.Error()
			} else {
				bs.err = ""
				bs.tracks = msg.tracks
			}
		}
		return m, nil

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
		m.searching = false
		if msg.err != nil {
			m.setError("search failed: " + msg.err.Error())
		} else {
			// Search lives in the secret-blocked ytmusic.go; clean its results here.
			m.searchResults = api.CleanTracks(msg.tracks)
			m.searchCursor = 0
			if len(msg.tracks) == 0 {
				m.setStatus("no results")
			} else {
				m.setStatus(fmt.Sprintf("%d results", len(msg.tracks)))
			}
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Forward any other message to the text input while typing (e.g. paste).
	if m.typing() {
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
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
		viewTrending, viewNewReleases, viewExplore, viewAlbum, viewArtist:
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
	case viewTrending, viewNewReleases, viewExplore:
		if bs, ok := m.browse[m.activeView]; ok {
			return &bs.cursor
		}
	case viewAlbum:
		return &m.albumCursor
	case viewArtist:
		return &m.artistCursor
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
	case viewTrending, viewNewReleases, viewExplore:
		if bs, ok := m.browse[m.activeView]; ok {
			return len(m.filt(bs.tracks))
		}
	case viewAlbum:
		return len(m.filt(m.albumTracks))
	case viewArtist:
		return len(m.filt(m.artistSongs)) + len(m.filtAlbums(m.artistAlbums))
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
		m.player.PlayPause()
		return m, nil
	}

	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "n":
		m.nextTrack()
		return m, nil
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
		m.activateView(viewTrending)
		return m, nil
	case "7":
		m.activateView(viewNewReleases)
		return m, nil
	case "8":
		m.activateView(viewExplore)
		return m, nil
	case "z":
		m.openGenres()
		return m, nil
	case "R":
		return m, m.startRadio()
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
	case viewTrending, viewNewReleases, viewExplore:
		return m.handleBrowseKey(m.activeView, msg)
	case viewAlbum:
		return m.handleAlbumKey(msg)
	case viewArtist:
		return m.handleArtistKey(msg)
	case viewGenres:
		return m.handleGenresKey(msg)
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
		// Play the whole album starting from the selected (filtered) track.
		if m.albumCursor < len(vis) {
			m.playAlbumFrom(vis[m.albumCursor])
		}
	case "p":
		// Play the whole album from the top.
		m.playAlbumFrom(0)
	}
	return m, nil
}

// playAlbumFrom replaces the queue with the loaded album and plays from idx.
func (m *model) playAlbumFrom(idx int) {
	if idx < 0 || idx >= len(m.albumTracks) {
		return
	}
	tracks := append([]api.Track(nil), m.albumTracks...)
	// Album rows carry the artist but not the album name; fill it so the
	// now-playing bar shows the album.
	for i := range tracks {
		if tracks[i].Album == "" {
			tracks[i].Album = m.albumTitle
		}
	}
	m.queue = tracks
	m.queueCursor = idx
	m.playAt(idx)
	m.setStatus("playing album: " + m.albumTitle)
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
	case "ctrl+r":
		m.refreshListenAgain()
		return m, m.loadHomeQuickPicks()
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
		if m.artistCursor < len(songVis) {
			m.playArtistSongsFrom(songVis[m.artistCursor])
		} else if ai := m.artistCursor - len(songVis); ai < len(albums) {
			return m, m.openAlbumByID(albums[ai])
		}
	case "p":
		m.playArtistSongsFrom(0)
	}
	return m, nil
}

// playArtistSongsFrom replaces the queue with the artist's top songs and plays
// from idx.
func (m *model) playArtistSongsFrom(idx int) {
	if idx < 0 || idx >= len(m.artistSongs) {
		return
	}
	songs := append([]api.Track(nil), m.artistSongs...)
	// Artist-page rows often omit the (implied) artist; fill it so the now-playing
	// bar shows it.
	for i := range songs {
		if songs[i].Artist == "" {
			songs[i].Artist = m.artistName
		}
	}
	m.queue = songs
	m.queueCursor = idx
	m.playAt(idx)
	m.setStatus("playing: " + m.artistName)
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

// loadBrowse kicks off the async API call for a browse view.
func (m *model) loadBrowse(v view) tea.Cmd {
	bs, ok := m.browse[v]
	if !ok || bs.load == nil {
		return nil
	}
	bs.loading = true
	load := bs.load
	client := m.api
	return func() tea.Msg {
		tracks, err := load(client)
		return browseLoadedMsg{view: v, tracks: tracks, err: err}
	}
}

func (m *model) handleBrowseKey(v view, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	bs, ok := m.browse[v]
	if !ok {
		return m, nil
	}
	tracks := m.filt(bs.tracks)
	if nc, ok := m.vimMove(msg.String(), bs.cursor, len(tracks)); ok {
		bs.cursor = nc
		return m, nil
	}
	switch msg.String() {
	case "enter":
		if bs.cursor < len(tracks) {
			m.enqueue(tracks[bs.cursor])
		}
	case "p":
		if bs.cursor < len(tracks) {
			m.playNow(tracks[bs.cursor])
		}
	case "ctrl+r":
		// reload the feed
		bs.loaded = false
		bs.loading = false
		return m, m.loadBrowse(v)
	}
	return m, nil
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
		tracks, err := client.Search(seed)
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
	}
	return m, nil
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
	case "esc":
		// Already browsing results — clear them.
		m.searchResults = nil
		m.searchCursor = 0
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
				// is no longer in the queue, so drop the now-playing marker.
				m.hasCurrent = false
				if m.queuePos >= len(m.queue) {
					m.queuePos = 0
				}
			}
			m.clampActiveCursor() // keep cursor in range of the (refiltered) list
			m.setStatus("removed from queue")
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
	case viewTrending, viewNewReleases, viewExplore:
		if bs, ok := m.browse[m.activeView]; ok {
			if tracks := m.filt(bs.tracks); bs.cursor < len(tracks) {
				t := tracks[bs.cursor]
				return &t
			}
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
	// Set MPRIS title so shells (noctalia/playerctl) show the song, not the URL.
	title := t.Title
	if t.Artist != "" {
		title += " — " + t.Artist
	}
	m.player.SetTitle(title)
	m.player.Load(t.ID)
	// No transient "loading" status — the now-playing bar shows load state.

	// Record the play in history (newest first); persistence is debounced.
	m.cfg.AddHistory(t)
	m.markConfigDirty()
	m.historyCursor = 0
}

func (m *model) nextTrack() {
	if len(m.queue) == 0 {
		return
	}
	switch m.repeat {
	case repeatOne:
		m.playAt(m.queuePos)
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
		} else {
			m.hasCurrent = false
			m.setStatus("queue ended")
		}
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
	return func() tea.Msg {
		tracks, err := m.api.Search(query)
		return searchDoneMsg{tracks: tracks, err: err}
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
