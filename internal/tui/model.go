package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rob/ytmusic/internal/api"
	"github.com/rob/ytmusic/internal/config"
	"github.com/rob/ytmusic/internal/player"
)

type view int

const (
	viewSearch view = iota
	viewQueue
	viewFavorites
)

type repeatMode int

const (
	repeatOff repeatMode = iota
	repeatAll
	repeatOne
)

type model struct {
	player *player.Player
	cfg    *config.Config
	api    *api.Client

	activeView view
	width      int
	height     int

	searchInput   textinput.Model
	searchMode    bool
	searching     bool
	searchResults []api.Track
	searchCursor  int

	queue       []api.Track
	queueCursor int
	queuePos    int
	shuffle     bool
	repeat      repeatMode

	favCursor int

	playerState player.State
	current     *api.Track

	status    string
	statusErr bool
}

type searchDoneMsg struct {
	tracks []api.Track
	err    error
}

type tickMsg time.Time

func New(p *player.Player, cfg *config.Config) *model {
	ti := textinput.New()
	ti.Placeholder = "Search YouTube Music..."
	ti.CharLimit = 200

	return &model{
		player:      p,
		cfg:         cfg,
		api:         api.NewClient(),
		searchInput: ti,
		playerState: player.State{Volume: cfg.Volume},
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
		return m, tick()

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

	if m.searchMode {
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Always quit on ctrl+c
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	// Tab/shift+tab work outside search input mode
	if !m.searchMode {
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "tab":
			m.activeView = (m.activeView + 1) % 3
			return m, nil
		case "shift+tab":
			m.activeView = (m.activeView + 2) % 3
			return m, nil
		}
	}

	// Search input mode
	if m.searchMode {
		switch msg.String() {
		case "esc":
			m.searchMode = false
			m.searchInput.Blur()
			return m, nil
		case "enter":
			query := strings.TrimSpace(m.searchInput.Value())
			if query == "" {
				return m, nil
			}
			m.searching = true
			m.activeView = viewSearch
			m.searchMode = false
			m.searchInput.Blur()
			return m, m.doSearch(query)
		}
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		return m, cmd
	}

	// Playback controls (always active outside search input mode)
	switch msg.String() {
	case " ":
		m.player.PlayPause()
		return m, nil
	case "n", "right":
		m.nextTrack()
		return m, nil
	case "b", "left":
		m.prevTrack()
		return m, nil
	case "shift+right":
		m.player.Seek(10)
		return m, nil
	case "shift+left":
		m.player.Seek(-10)
		return m, nil
	case "=", "+":
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
	case "/":
		m.searchMode = true
		m.searchInput.Focus()
		return m, textinput.Blink
	}

	// View-specific keys
	switch m.activeView {
	case viewSearch:
		return m.handleSearchKey(msg)
	case viewQueue:
		return m.handleQueueKey(msg)
	case viewFavorites:
		return m.handleFavKey(msg)
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
			t := m.searchResults[m.searchCursor]
			m.enqueue(t)
			m.setStatus("queued: " + t.Title)
		}
	case "p":
		if len(m.searchResults) > 0 {
			t := m.searchResults[m.searchCursor]
			m.playNow(t)
		}
	case "f":
		if len(m.searchResults) > 0 {
			t := m.searchResults[m.searchCursor]
			if m.cfg.ToggleFavorite(t) {
				m.setStatus("favorited: " + t.Title)
			} else {
				m.setStatus("unfavorited: " + t.Title)
			}
		}
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
			m.setStatus("removed from queue")
		}
	case "c":
		m.queue = nil
		m.queueCursor = 0
		m.queuePos = 0
		m.player.Stop()
		m.current = nil
		m.setStatus("queue cleared")
	case "f":
		if len(m.queue) > 0 {
			t := m.queue[m.queueCursor]
			if m.cfg.ToggleFavorite(t) {
				m.setStatus("favorited: " + t.Title)
			} else {
				m.setStatus("unfavorited: " + t.Title)
			}
		}
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
			t := favs[m.favCursor]
			m.enqueue(t)
			m.setStatus("queued: " + t.Title)
		}
	case "p":
		if m.favCursor < len(favs) {
			m.playNow(favs[m.favCursor])
		}
	case "f", "d":
		if m.favCursor < len(favs) {
			t := favs[m.favCursor]
			m.cfg.ToggleFavorite(t)
			if m.favCursor >= len(m.cfg.Favorites) && m.favCursor > 0 {
				m.favCursor--
			}
		}
	}
	return m, nil
}

func (m *model) enqueue(t api.Track) {
	m.queue = append(m.queue, t)
	if m.current == nil {
		m.queuePos = len(m.queue) - 1
		m.playAt(m.queuePos)
	}
}

func (m *model) playNow(t api.Track) {
	m.queue = append([]api.Track{t}, m.queue...)
	m.queuePos = 0
	if m.queueCursor > 0 {
		m.queueCursor++
	}
	m.playAt(0)
}

func (m *model) playAt(idx int) {
	if idx < 0 || idx >= len(m.queue) {
		return
	}
	m.current = &m.queue[idx]
	m.player.Load(m.queue[idx].ID)
	m.setStatus("loading: " + m.queue[idx].Title)
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
			m.queuePos = rand.Intn(len(m.queue))
		} else {
			m.queuePos = (m.queuePos + 1) % len(m.queue)
		}
		m.playAt(m.queuePos)
	default:
		if m.shuffle {
			m.queuePos = rand.Intn(len(m.queue))
			m.playAt(m.queuePos)
		} else if m.queuePos+1 < len(m.queue) {
			m.queuePos++
			m.playAt(m.queuePos)
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
	if m.playerState.Position > 3 {
		m.player.Seek(-m.playerState.Position)
		return
	}
	if m.queuePos > 0 {
		m.queuePos--
		m.playAt(m.queuePos)
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

// ─── View ────────────────────────────────────────────────────────────────────

func (m *model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	header := m.renderHeader()
	playerBar := m.renderPlayerBar()
	headerH := lipgloss.Height(header)
	playerH := lipgloss.Height(playerBar)
	contentH := m.height - headerH - playerH
	if contentH < 1 {
		contentH = 1
	}

	content := m.renderContent(contentH)

	return lipgloss.JoinVertical(lipgloss.Left, header, content, playerBar)
}

func (m *model) renderHeader() string {
	title := styleTitle.Render("▶ YT Music")

	// Build tabs
	tabNames := []string{"Search", "Queue", "Favorites"}
	var tabParts []string
	for i, name := range tabNames {
		padded := "  " + name + "  "
		if view(i) == m.activeView {
			tabParts = append(tabParts, lipgloss.NewStyle().
				Foreground(colorRed).Bold(true).Underline(true).Render(padded))
		} else {
			tabParts = append(tabParts, styleHelp.Render(padded))
		}
	}
	tabBar := strings.Join(tabParts, styleHelp.Render("│"))

	// Mode indicators
	var indParts []string
	if m.shuffle {
		indParts = append(indParts, styleStatus.Render("⇌"))
	}
	switch m.repeat {
	case repeatAll:
		indParts = append(indParts, styleStatus.Render("↻"))
	case repeatOne:
		indParts = append(indParts, styleStatus.Render("↺1"))
	}
	indicators := strings.Join(indParts, " ")

	titleW := lipgloss.Width(title)
	tabW := lipgloss.Width(tabBar)
	indW := lipgloss.Width(indicators)

	// Center tabs, right-align indicators
	remaining := m.width - titleW - indW
	if remaining < tabW {
		remaining = tabW + 2
	}
	leftPad := (remaining - tabW) / 2
	rightPad := remaining - tabW - leftPad
	if leftPad < 1 {
		leftPad = 1
	}
	if rightPad < 1 {
		rightPad = 1
	}

	line := title + strings.Repeat(" ", leftPad) + tabBar + strings.Repeat(" ", rightPad) + indicators
	sep := styleHelp.Render(strings.Repeat("─", m.width))
	return line + "\n" + sep
}

func (m *model) renderContent(height int) string {
	switch m.activeView {
	case viewSearch:
		return m.renderSearch(height)
	case viewQueue:
		return m.renderQueue(height)
	case viewFavorites:
		return m.renderFavorites(height)
	}
	return ""
}

func (m *model) renderSearch(height int) string {
	var sb strings.Builder

	// Search input line
	prefix := styleHelp.Render("  / ")
	if m.searchMode {
		prefix = styleStatus.Render("  / ")
	}
	inputLine := prefix + m.searchInput.View()
	sb.WriteString(inputLine + "\n")
	sb.WriteString(styleHelp.Render(strings.Repeat("─", m.width)) + "\n")
	reserved := 2 // input line + separator

	if m.searching {
		sb.WriteString(styleStatus.Render("  Searching..."))
		return m.padContent(sb.String(), height)
	}

	if len(m.searchResults) == 0 {
		sb.WriteString(styleHelp.Render("  [/] search  [Enter] queue  [p] play now  [f] favorite"))
		return m.padContent(sb.String(), height)
	}

	listH := height - reserved
	if listH < 1 {
		listH = 1
	}
	start := 0
	if m.searchCursor >= listH {
		start = m.searchCursor - listH + 1
	}

	for i := start; i < len(m.searchResults) && i-start < listH; i++ {
		sb.WriteString(m.renderTrackLine(m.searchResults[i], i == m.searchCursor) + "\n")
	}
	return sb.String()
}

func (m *model) renderQueue(height int) string {
	if len(m.queue) == 0 {
		lines := styleHelp.Render("  Queue empty — search tracks and press Enter to add.\n") +
			styleHelp.Render("  [d/x] remove  [c] clear  [f] favorite  [Enter/p] play")
		return m.padContent(lines, height)
	}

	var sb strings.Builder
	start := 0
	if m.queueCursor >= height {
		start = m.queueCursor - height + 1
	}

	for i := start; i < len(m.queue) && i-start < height; i++ {
		nowPlayingPrefix := "  "
		if i == m.queuePos && m.current != nil {
			nowPlayingPrefix = styleNowPlaying.Render("▶ ")
		}
		sb.WriteString(nowPlayingPrefix + m.renderTrackLine(m.queue[i], i == m.queueCursor) + "\n")
	}
	return sb.String()
}

func (m *model) renderFavorites(height int) string {
	favs := m.cfg.Favorites
	if len(favs) == 0 {
		return m.padContent(styleHelp.Render("  No favorites yet — press [f] on any track to add."), height)
	}

	var sb strings.Builder
	start := 0
	if m.favCursor >= height {
		start = m.favCursor - height + 1
	}

	for i := start; i < len(favs) && i-start < height; i++ {
		sb.WriteString(m.renderTrackLine(favs[i], i == m.favCursor) + "\n")
	}
	return sb.String()
}

func (m *model) renderTrackLine(t api.Track, selected bool) string {
	fav := " "
	if m.cfg.IsFavorite(t.ID) {
		fav = styleFavorite.Render("♥")
	}

	dur := t.Duration
	if dur == "" {
		dur = "─:──"
	}

	// Calculate column widths
	total := m.width - 4 // margins
	if total < 20 {
		total = 20
	}
	durW := 6
	favW := 2
	artistW := total / 4
	titleW := total - artistW - durW - favW
	if titleW < 10 {
		titleW = 10
	}

	title := truncate(t.Title, titleW)
	artist := truncate(t.Artist, artistW)

	if selected {
		row := fmt.Sprintf(" %-*s  %-*s  %s %*s",
			titleW, title,
			artistW, artist,
			fav,
			durW, dur,
		)
		return styleSelected.Render(row)
	}

	return fmt.Sprintf(" %s  %s  %s %s",
		styleNormal.Render(fmt.Sprintf("%-*s", titleW, title)),
		styleSubtitle.Render(fmt.Sprintf("%-*s", artistW, artist)),
		fav,
		styleHelp.Render(fmt.Sprintf("%*s", durW, dur)),
	)
}

func (m *model) renderPlayerBar() string {
	sep := styleHelp.Render(strings.Repeat("─", m.width))
	help := styleHelp.Render("  [/]search [space]pause [n/b]skip [shift+←→]seek [=/-]vol [f]fav [s]shuffle [r]repeat [q]quit")

	if m.current == nil {
		return sep + "\n" + help
	}

	state := m.playerState

	// Play/pause icon
	playIcon := styleStatus.Render("▶")
	if state.Paused {
		playIcon = styleHelp.Render("⏸")
	}

	// Line 1: icon + title · artist + status
	titleStr := truncate(m.current.Title, m.width/2)
	artistStr := truncate(m.current.Artist, m.width/4)
	trackInfo := styleTitle.Render(titleStr) + styleHelp.Render("  ·  ") + styleSubtitle.Render(artistStr)

	statusPart := ""
	if m.status != "" {
		if m.statusErr {
			statusPart = "  " + styleError.Render(m.status)
		} else {
			statusPart = "  " + styleStatus.Render(m.status)
		}
	}
	line1 := " " + playIcon + "  " + trackInfo + statusPart

	// Line 2: icon + progress bar + time + volume
	posStr := fmtDur(state.Position)
	durStr := fmtDur(state.Duration)
	timeStr := styleHelp.Render(posStr + "/" + durStr)
	volStr := styleHelp.Render(fmt.Sprintf(" vol:%d%%", int(state.Volume)))

	timeW := len(posStr) + len(durStr) + 1
	volW := lipgloss.Width(volStr)
	iconW := lipgloss.Width(playIcon)
	progressW := m.width - timeW - iconW - volW - 6
	if progressW < 4 {
		progressW = 4
	}
	bar := renderProgress(state.Position, state.Duration, progressW)
	line2 := " " + playIcon + "  " + bar + "  " + timeStr + volStr

	return sep + "\n" + line1 + "\n" + line2 + "\n" + help
}

func renderProgress(pos, dur float64, width int) string {
	if dur <= 0 || width <= 0 {
		return styleProgressEmpty.Render(strings.Repeat("─", width))
	}
	pct := pos / dur
	if pct > 1 {
		pct = 1
	}
	filled := int(float64(width) * pct)
	if filled > width {
		filled = width
	}
	return styleProgressFull.Render(strings.Repeat("━", filled)) +
		styleProgressEmpty.Render(strings.Repeat("─", width-filled))
}

func fmtDur(secs float64) string {
	if secs <= 0 {
		return "0:00"
	}
	s := int(secs)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return s[:max-1] + "…"
}

// padContent pads content string to fill height lines (used for empty states)
func (m *model) padContent(content string, height int) string {
	lines := strings.Count(content, "\n")
	if lines < height {
		content += strings.Repeat("\n", height-lines)
	}
	return content
}
