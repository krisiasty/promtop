package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/krisiasty/promtop/internal/store"
)

func (m *Model) handleKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.dropHiddenPopups()
	cmd := m.key(k)
	m.clampCursor()
	return m, cmd
}

// key applies the README keymap. Precedence: filter input → view switching
// (closes popups) → open popup → global and view keys.
func (m *Model) key(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if s == "ctrl+c" {
		return tea.Quit
	}
	if m.filtering {
		m.filterKey(k, s)
		return nil
	}
	if v, ok := viewKey(s, m.view); ok {
		m.detailOpen, m.errOpen, m.view = false, false, v
		return nil
	}
	if m.errOpen {
		switch s {
		case "esc", "e", "enter", "q":
			m.errOpen = false
		}
		return nil
	}
	if m.detailOpen {
		switch s {
		case "esc", "enter", "q":
			m.detailOpen = false
		case "up":
			m.labOff = max(0, m.labOff-1)
		case "down":
			m.labOff = min(m.labMax(), m.labOff+1)
		case "pgup":
			m.labOff = max(0, m.labOff-m.detailPageSize())
		case "pgdown":
			m.labOff = min(m.labMax(), m.labOff+m.detailPageSize())
		case "space":
			m.togglePin()
		}
		return nil
	}
	switch s {
	case "q":
		return tea.Quit
	case "up":
		m.move(-1)
	case "down":
		m.move(1)
	case "pgup":
		m.move(-m.pageSize())
	case "pgdown":
		m.move(m.pageSize())
	case "g":
		m.move(-1 << 30)
	case "G":
		m.move(1 << 30)
	case "space":
		if m.view <= ViewGraph {
			m.togglePin()
		}
	case "e":
		if m.failing() {
			m.errOpen = true
		}
	case "a":
		m.onlyPinned = !m.onlyPinned
		m.setCursor(m.rows(), 0)
	case "r":
		m.showRate = !m.showRate
	case "w":
		if m.view == ViewRaw {
			m.rawWrap, m.rawOff = !m.rawWrap, 0
		} else {
			m.window = nextWindow(m.window)
			m.histOff = 0
		}
	case "+", "=":
		if w := stepWindow(m.window, 1); w != m.window {
			m.window, m.histOff = w, 0
		}
	case "-":
		if w := stepWindow(m.window, -1); w != m.window {
			m.window, m.histOff = w, 0
		}
	case "[":
		if m.view == ViewHistory {
			m.moveSeries(-1)
		}
	case "]":
		if m.view == ViewHistory {
			m.moveSeries(1)
		}
	case "p":
		m.paused, m.gen = !m.paused, m.gen+1
		if !m.paused {
			return m.scrapeCmd()
		}
	case "s":
		if !m.st.HighCard() {
			m.sort = (m.sort + 1) % 4
		}
	case "/":
		m.filtering = true
	case "m":
		if m.view == ViewHeatmap {
			m.heatBySeries, m.heatIdx = !m.heatBySeries, 0
		}
	case "c":
		if m.view == ViewHeatmap {
			m.toggleHeatScope()
		}
	case "enter":
		switch {
		case m.view <= ViewGraph && len(m.rows()) > 0:
			m.detailOpen, m.labOff = true, 0
		case m.view == ViewSeries && m.w < 146:
			m.famOpen = true
		}
	case "esc":
		if m.view == ViewSeries && m.famOpen {
			m.famOpen = false
		} else {
			m.filter = ""
		}
	}
	return nil
}

func (m *Model) filterKey(k tea.KeyPressMsg, s string) {
	switch s {
	case "esc":
		m.filtering, m.filter = false, ""
	case "enter":
		m.filtering = false
	case "backspace":
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
		}
	default:
		if k.Text == "" {
			return
		}
		m.filter += k.Text
	}
	m.setCursor(m.rows(), 0)
}

func viewKey(s string, cur View) (View, bool) {
	switch s {
	case "1", "2", "3", "4", "5", "6":
		return View(s[0] - '1'), true
	case "tab", "right":
		return (cur + 1) % 6, true
	case "shift+tab", "left":
		return (cur + 5) % 6, true
	}
	return cur, false
}

func nextWindow(d time.Duration) time.Duration {
	for i, w := range windows {
		if w == d {
			return windows[(i+1)%len(windows)]
		}
	}
	return 5 * time.Minute
}

func stepWindow(d time.Duration, step int) time.Duration {
	for i, w := range windows {
		if w == d {
			return windows[clamp(i+step, 0, len(windows)-1)]
		}
	}
	return 5 * time.Minute
}

// dropHiddenPopups closes popups that popup() no longer draws (the target
// recovered, or no series is left to detail) so they stop capturing input.
func (m *Model) dropHiddenPopups() {
	if m.errOpen && !m.failing() {
		m.errOpen = false
	}
	if m.detailOpen && (m.view > ViewGraph || len(m.rows()) == 0) {
		m.detailOpen = false
	}
}

func (m *Model) failing() bool {
	s := m.st.Scrape.State()
	return s == store.Degraded || s == store.Down
}

// move handles ↑ ↓ PgUp PgDn g G for the current view.
func (m *Model) move(n int) {
	switch m.view {
	case ViewHistory:
		m.histOff = clamp(m.histOff+n, 0, m.historyMaxOff())
	case ViewSeries:
		m.famIdx = clamp(m.famIdx+n, 0, len(m.st.Families())-1)
	case ViewHeatmap:
		m.heatIdx = clamp(m.heatIdx+n, 0, m.heatSourceCount()-1)
	case ViewRaw:
		m.rawOff = clamp(m.rawOff+n, 0, m.rawMaxOff())
	default:
		rows := m.rows()
		_, cur := m.selected(rows)
		m.setCursor(rows, cur+n)
	}
}

func (m *Model) moveSeries(n int) {
	rows := m.rows()
	_, cur := m.selected(rows)
	m.setCursor(rows, cur+n)
}

func (m *Model) pageSize() int {
	switch m.view {
	case ViewTable:
		return max(1, m.bodyHeight()-1)
	case ViewHistory, ViewRaw:
		return max(1, m.bodyHeight()-3)
	case ViewSeries:
		return max(1, m.bodyHeight()-4)
	default:
		return 1
	}
}

func (m *Model) detailPageSize() int {
	s, _ := m.selected(m.rows())
	if s == nil {
		return 1
	}
	if s.Hist != nil {
		return m.bucketVisible(m.bodyHeight(), s)
	}
	return m.labVisible(m.bodyHeight(), len(s.Labels))
}

func (m *Model) togglePin() {
	if s, _ := m.selected(m.rows()); s != nil {
		m.st.SetPinned(s.Key, !m.st.Pinned(s.Key))
	}
}

// clampCursor re-derives the cursor from the selected series after the rows
// were rebuilt, falling back to the clamped index when the series is gone.
func (m *Model) clampCursor() {
	rows := m.rows()
	_, cur := m.selected(rows)
	m.setCursor(rows, cur)
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	return min(max(v, lo), hi)
}
