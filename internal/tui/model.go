package tui

import (
	"fmt"
	"math/rand"
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

	activeView view
	focus      focusArea
	navCursor  int
	width      int
	height     int

	// search
	searchInput   textinput.Model
	searchTyping  bool // true = editing query; false = browsing results
	searching     bool
	searchResults []api.Track
	searchCursor  int

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

	// home menu
	homeCursor int

	// player snapshot (refreshed each tick)
	playerState player.State
	current     *api.Track

	// transient status line
	status    string
	statusErr bool
}

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

// randomSeeds drive the "play random" feature (z): a random seed is searched
// and a random result is played. No extra API endpoint required.
var randomSeeds = []string{
	"lofi", "rock", "jazz", "pop hits", "electronic", "hip hop",
	"classical", "indie", "chill", "80s", "90s", "metal", "r&b", "funk",
}

// homeMenu lists the entries shown on the Home view, in cursor order.
var homeMenu = []struct {
	label string
	view  view
}{
	{"Search", viewSearch},
	{"Queue", viewQueue},
	{"Favorites", viewFavorites},
	{"History", viewHistory},
	{"Trending", viewTrending},
	{"New Releases", viewNewReleases},
	{"Explore", viewExplore},
	{"Help", viewHelp},
}

// viewCycle is the tab-order of top-level views (Help excluded from cycling).
var viewCycle = []view{
	viewHome, viewSearch, viewQueue, viewFavorites, viewHistory,
	viewTrending, viewNewReleases, viewExplore,
}

func New(p *player.Player, cfg *config.Config) *model {
	ti := textinput.New()
	ti.Placeholder = "Search YouTube Music..."
	ti.CharLimit = 200

	return &model{
		player:       p,
		cfg:          cfg,
		api:          api.NewClient(),
		activeView:   viewHome,
		focus:        focusSidebar,
		navCursor:    0,
		searchInput:  ti,
		searchTyping: true,
		playerState:  player.State{Volume: cfg.Volume},
		browse: map[view]*browseState{
			viewTrending:    {title: "Trending", load: (*api.Client).Trending},
			viewNewReleases: {title: "New Releases", load: (*api.Client).NewReleases},
			viewExplore:     {title: "Explore", load: (*api.Client).Explore},
		},
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tick(),
	)
}

func tick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
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
		// Lazily load the active browse view the first time it's opened.
		if bs, ok := m.browse[m.activeView]; ok && !bs.loaded && !bs.loading {
			return m, tea.Batch(tick(), m.loadBrowse(m.activeView))
		}
		return m, tick()

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
		if msg.err != nil {
			m.setError("random failed: " + msg.err.Error())
		} else if len(msg.tracks) == 0 {
			m.setError("no random track found")
		} else {
			m.playNow(msg.tracks[rand.Intn(len(msg.tracks))])
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

	case searchDoneMsg:
		m.searching = false
		if msg.err != nil {
			m.setError("search failed: " + msg.err.Error())
		} else {
			m.searchResults = msg.tracks
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
			m.searchTyping = false
			m.searchInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		return m, cmd
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
	case "/":
		m.activateView(viewSearch)
		m.searchTyping = true
		m.searchInput.Focus()
		return m, textinput.Blink
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
		return m, m.playRandom()
	case "R":
		return m, m.startRadio()
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
	case viewHelp:
		return m, nil
	}
	return m, nil
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
	switch msg.String() {
	case "j", "down":
		if bs.cursor < len(bs.tracks)-1 {
			bs.cursor++
		}
	case "k", "up":
		if bs.cursor > 0 {
			bs.cursor--
		}
	case "enter":
		if bs.cursor < len(bs.tracks) {
			m.enqueue(bs.tracks[bs.cursor])
		}
	case "p":
		if bs.cursor < len(bs.tracks) {
			m.playNow(bs.tracks[bs.cursor])
		}
	case "g":
		// reload the feed
		bs.loaded = false
		bs.loading = false
		return m, m.loadBrowse(v)
	}
	return m, nil
}

// playRandom searches a random seed term and plays a random result.
func (m *model) playRandom() tea.Cmd {
	seed := randomSeeds[rand.Intn(len(randomSeeds))]
	m.setStatus("finding a random song (" + seed + ")...")
	client := m.api
	return func() tea.Msg {
		tracks, err := client.Search(seed)
		return randomDoneMsg{tracks: tracks, err: err}
	}
}

// startRadio enqueues tracks related to the currently playing song.
func (m *model) startRadio() tea.Cmd {
	if m.current == nil {
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
	hist := m.cfg.History
	switch msg.String() {
	case "j", "down":
		if m.historyCursor < len(hist)-1 {
			m.historyCursor++
		}
	case "k", "up":
		if m.historyCursor > 0 {
			m.historyCursor--
		}
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
	m.activeView = v
	m.navCursor = navIndexOf(v)
	m.focus = focusPanel
}

// handleSidebarKey processes navigation while the Quick Links sidebar is focused.
func (m *model) handleSidebarKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.navCursor < len(navEntries)-1 {
			m.navCursor++
		}
		return m, nil
	case "k", "up":
		if m.navCursor > 0 {
			m.navCursor--
		}
		return m, nil
	case "enter", "l", "right":
		target := navEntries[m.navCursor].view
		m.activeView = target
		m.focus = focusPanel
		if target == viewSearch {
			m.searchTyping = true
			m.searchInput.Focus()
			return m, textinput.Blink
		}
		return m, nil
	}
	return m, nil
}

func (m *model) handleHomeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.homeCursor < len(homeMenu)-1 {
			m.homeCursor++
		}
	case "k", "up":
		if m.homeCursor > 0 {
			m.homeCursor--
		}
	case "enter":
		target := homeMenu[m.homeCursor]
		m.activeView = target.view
		if target.view == viewSearch {
			m.searchTyping = true
			m.searchInput.Focus()
			return m, textinput.Blink
		}
	}
	return m, nil
}

func (m *model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.searchCursor < len(m.searchResults)-1 {
			m.searchCursor++
		}
	case "k", "up":
		if m.searchCursor > 0 {
			m.searchCursor--
		}
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
	switch msg.String() {
	case "j", "down":
		if m.queueCursor < len(m.queue)-1 {
			m.queueCursor++
		}
	case "k", "up":
		if m.queueCursor > 0 {
			m.queueCursor--
		}
	case "enter", "p":
		if len(m.queue) > 0 {
			m.queuePos = m.queueCursor
			m.playAt(m.queuePos)
		}
	case "d", "x":
		if len(m.queue) > 0 {
			m.queue = append(m.queue[:m.queueCursor], m.queue[m.queueCursor+1:]...)
			if m.queueCursor > 0 && m.queueCursor >= len(m.queue) {
				m.queueCursor--
			}
			if m.queuePos > m.queueCursor {
				m.queuePos--
			}
			m.setStatus("removed from queue")
		}
	case "c":
		m.queue = nil
		m.queueCursor = 0
		m.queuePos = 0
		m.player.Stop()
		m.current = nil
		m.setStatus("queue cleared")
	}
	return m, nil
}

func (m *model) handleFavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	favs := m.cfg.Favorites
	switch msg.String() {
	case "j", "down":
		if m.favCursor < len(favs)-1 {
			m.favCursor++
		}
	case "k", "up":
		if m.favCursor > 0 {
			m.favCursor--
		}
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
			m.cfg.Save()
			if m.favCursor >= len(m.cfg.Favorites) && m.favCursor > 0 {
				m.favCursor--
			}
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
	m.cfg.Save()
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
	case viewSearch:
		if m.searchCursor < len(m.searchResults) {
			t := m.searchResults[m.searchCursor]
			return &t
		}
	case viewQueue:
		if m.queueCursor < len(m.queue) {
			t := m.queue[m.queueCursor]
			return &t
		}
	case viewFavorites:
		if m.favCursor < len(m.cfg.Favorites) {
			t := m.cfg.Favorites[m.favCursor]
			return &t
		}
	}
	// Fall back to the currently playing track (Home/Help and empty lists).
	return m.current
}

// ── Action methods ──

func (m *model) enqueue(t api.Track) {
	m.queue = append(m.queue, t)
	m.setStatus("queued: " + t.Title)
	if m.current == nil {
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
	m.current = &m.queue[idx]
	t := m.queue[idx]
	// Set MPRIS title so shells (noctalia/playerctl) show the song, not the URL.
	title := t.Title
	if t.Artist != "" {
		title += " — " + t.Artist
	}
	m.player.SetTitle(title)
	m.player.Load(t.ID)
	m.setStatus("loading: " + t.Title)

	// Record the play in history (newest first) and persist.
	m.cfg.AddHistory(t)
	m.cfg.Save()
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
			m.current = nil
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
}

func (m *model) setError(s string) {
	m.status = s
	m.statusErr = true
}
