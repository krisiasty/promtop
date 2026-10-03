// Package ui is promtop's Bubble Tea front end: one root model whose views are
// pure render functions of the model state.
package ui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/krisiasty/promtop/internal/config"
	"github.com/krisiasty/promtop/internal/scrape"
	"github.com/krisiasty/promtop/internal/stats"
	"github.com/krisiasty/promtop/internal/store"
)

// View is one of the six tabs.
type View int

const (
	ViewTable View = iota
	ViewHistory
	ViewGraph
	ViewHeatmap
	ViewSeries
	ViewRaw
)

var viewNames = [...]string{"Table", "History", "Graph", "Heatmap", "Series", "Raw"}

// SortKey is the Table sort order.
type SortKey int

const (
	SortName SortKey = iota // exposition order
	SortCurrent
	SortDeltaPct
	SortP99
)

var sortNames = [...]string{"name", "current", "Δ%", "p99"}

var windows = []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute,
	10 * time.Minute, 15 * time.Minute, 20 * time.Minute, 30 * time.Minute}

const (
	minW = 80
	minH = 24
)

// ScrapeFunc performs one scrape; (*scrape.Client).Scrape satisfies it.
type ScrapeFunc func(ctx context.Context) (*scrape.Result, error)

// Options injects dependencies; zero values select production defaults.
type Options struct {
	Now    func() time.Time
	Scrape ScrapeFunc
	Store  *store.Store
}

type clickInfo struct {
	idx int
	at  time.Time
}

type statsCache struct {
	seq    uint64
	window time.Duration
	rate   bool
	m      map[*store.Series]stats.Stats
	life   map[*store.Series]bool // histogram rows showing lifetime values
}

type heatCache struct {
	seq      uint64
	window   time.Duration
	bySeries bool
	idx      int
	ok       bool
	d        store.HeatData
}

type (
	tickMsg      struct{ gen uint64 } // gen: the scrape loop that scheduled it
	clockMsg     struct{}
	scrapeOKMsg  struct{ r *scrape.Result }
	scrapeErrMsg struct{ e *scrape.Error }
)

// Model is promtop's root Bubble Tea model.
type Model struct {
	cfg    config.Config
	st     *store.Store
	scrape ScrapeFunc
	now    func() time.Time
	th     theme
	w, h   int

	view           View
	cursor         int
	selID          string // key of the selected series; the cursor follows it
	histOff        int    // History rows scrolled back from the newest sample
	famIdx         int
	famOpen        bool
	heatIdx        int
	heatBySeries   bool
	heatSet        bool // c chose the heatmap panels' scope explicitly
	heatSinceStart bool
	rawOff         int
	rawWrap        bool
	labOff         int
	onlyPinned     bool
	showRate       bool
	paused         bool
	sort           SortKey
	window         time.Duration
	filter         string
	filtering      bool
	detailOpen     bool
	errOpen        bool
	inFlight       bool
	gen            uint64 // scrape loop generation, bumped on pause and resume
	lastClick      clickInfo
	cache          statsCache
	rowCache       rowsCache
	heat           heatCache
	raw            rawCache
}

// New builds the model. Rendering starts once a WindowSizeMsg arrives.
func New(cfg config.Config, opts Options) *Model {
	m := &Model{cfg: cfg, st: opts.Store, scrape: opts.Scrape, now: opts.Now, showRate: true,
		window: cfg.Window, th: newTheme(true)}
	if m.now == nil {
		m.now = time.Now
	}
	if m.st == nil {
		m.st = store.NewWithBudget(cfg.Interval, cfg.HistoryBudgetBytes)
	}
	if m.window == 0 {
		m.window = 5 * time.Minute
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.scrapeCmd(), clockTick())
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		resized := m.w > 0 && msg.Width != m.w
		m.w, m.h = msg.Width, msg.Height
		if resized {
			return m, resetTabStops()
		}
	case tea.BackgroundColorMsg:
		m.th = newTheme(msg.IsDark())
	case clockMsg:
		return m, clockTick()
	case tickMsg:
		if m.paused || msg.gen != m.gen { // stale ticks would start a second loop
			return m, nil
		}
		return m, m.scrapeCmd()
	case scrapeOKMsg:
		m.inFlight = false
		if m.paused {
			return m, nil
		}
		m.st.Apply(msg.r)
		m.clampCursor()
		m.dropHiddenPopups()
		return m, m.nextTick()
	case scrapeErrMsg:
		m.inFlight = false
		if m.paused {
			return m, nil
		}
		m.st.Fail(msg.e)
		return m, m.nextTick()
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseClickMsg:
		return m.handleClick(msg)
	case tea.MouseWheelMsg:
		return m.handleWheel(msg)
	}
	return m, nil
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "promtop " + m.cfg.Target.URL
	return v
}

// scrapeCmd starts a scrape unless one is already in flight.
func (m *Model) scrapeCmd() tea.Cmd {
	if m.scrape == nil || m.inFlight {
		return nil
	}
	m.inFlight = true
	fn := m.scrape
	return func() tea.Msg {
		r, err := fn(context.Background())
		if err == nil {
			return scrapeOKMsg{r}
		}
		var se *scrape.Error
		if !errors.As(err, &se) {
			se = &scrape.Error{Kind: scrape.KindOther, Full: err.Error(), Short: err.Error(), Attempt: err.Error(), At: time.Now()}
		}
		return scrapeErrMsg{se}
	}
}

// nextTick schedules the next attempt at the store's NextTry (fixed rate).
func (m *Model) nextTick() tea.Cmd {
	d, gen := max(0, m.st.Scrape.NextTry.Sub(m.now())), m.gen
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{gen} })
}

// resetTabStops sets tab stops every 8 columns again, then repaints. Bubble
// Tea sets them once at startup and its renderer moves the cursor with tabs,
// but terminals such as iTerm2 add no stops for columns gained by resizing:
// a tab past the last stop lands on the right margin and wraps what follows.
// The repaint replaces any frame drawn with tabs before the reset.
func resetTabStops() tea.Cmd {
	return tea.Sequence(tea.Raw(ansi.SetTabEvery8Columns), tea.ClearScreen)
}

func clockTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return clockMsg{} })
}
