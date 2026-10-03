package ui

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/store"
)

type bannerInfo struct {
	text   string
	fg, bg color.Color
	hint   bool // show "e details" on the right
}

// banner picks the active banner (failure > counter reset > high
// cardinality) and the richest candidate text that fits.
func (m *Model) banner(w int) *bannerInfo {
	sc := &m.st.Scrape
	now := m.now()
	var cands [][]string
	b := &bannerInfo{}
	switch st := sc.State(); {
	case st == store.Down && sc.LastErr != nil:
		a := "✕ target down"
		r := "retry in " + m.retryIn(now)
		stale := ""
		if sc.OKCount > 0 {
			stale = "stale since " + format.Clock(sc.LastOK)
		}
		f := strconv.Itoa(sc.Fails) + " consecutive failures"
		full, short := a+": "+sc.LastErr.Full, a+": "+sc.LastErr.Short
		cands = [][]string{{full, r, stale, f}, {full, r, stale}, {short, r, stale, f}, {short, r, stale},
			{short, r}, {short}, {"✕ down: " + sc.LastErr.Short}, {"✕ down"}}
		b.fg, b.bg, b.hint = m.th.c.red, m.th.c.redBg, true
	case st == store.Degraded && sc.LastErr != nil:
		a := "⚠ " + strconv.Itoa(sc.Fails) + " failed scrape"
		if sc.Fails > 1 {
			a += "s"
		}
		last := ""
		if sc.OKCount > 0 {
			last = "last success " + format.Clock(sc.LastOK)
		}
		r := "retrying every " + format.Interval(m.cfg.Interval)
		full, short := a+": "+sc.LastErr.Full, a+": "+sc.LastErr.Short
		cands = [][]string{{full, last, r}, {full, last}, {short, last, r}, {short, last}, {short}, {"⚠ " + sc.LastErr.Short}}
		b.fg, b.bg, b.hint = m.th.c.amber, m.th.c.amberBg, true
	case !m.st.Reset.At.IsZero() && now.Sub(m.st.Reset.At) < store.ResetHold:
		rs := m.st.Reset
		n := strconv.Itoa(rs.Count) + " counters reset"
		r := "rates computed across the reset"
		if rs.Restarted {
			a := "↺ target restarted " + format.Clock(rs.RestartAt)
			cands = [][]string{{a + " (process_start_time_seconds changed)", n, r}, {a, n, r}, {a, n}, {a}}
		} else {
			cands = [][]string{{"↺ " + n, r}, {"↺ " + n}}
		}
		b.fg, b.bg = m.th.c.cyan, m.th.c.cyanBg
	case m.st.HighCard():
		a := "⚠ high cardinality: " + format.Int(len(m.st.Series())) + " series"
		top := ""
		if name, n := m.st.TopFamily(); name != "" {
			top = ", " + format.Int(n) + " from " + name
			if m.isHistogram(name) { // histogram samples tipped it over
				top = ", " + format.Int(n) + " samples from " + name
			}
		}
		cands = [][]string{{a + top, "stats for visible rows only", "value sort disabled"},
			{a, "stats for visible rows only", "value sort disabled"}, {a, "stats for visible rows only"}, {a}}
		b.fg, b.bg = m.th.c.amber, m.th.c.amberBg
	default:
		return nil
	}
	room := w - 2
	if b.hint {
		room -= 11
	}
	b.text = fitSegs(cands, room)
	return b
}

func (m *Model) isHistogram(family string) bool {
	for _, fi := range m.st.Families() {
		if fi.Name == family {
			return fi.Type.IsHistogram()
		}
	}
	return false
}

// fitSegs returns the first candidate (segments joined by " · ", empty ones
// skipped) that fits w cells, truncating only the last one if nothing fits.
func fitSegs(cands [][]string, w int) string {
	join := func(c []string) string {
		var parts []string
		for _, s := range c {
			if s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " · ")
	}
	for _, c := range cands {
		if t := join(c); format.Width(t) <= w {
			return t
		}
	}
	return format.Truncate(join(cands[len(cands)-1]), max(1, w))
}

func (m *Model) bannerLine(w int, b *bannerInfo) string {
	st := lipgloss.NewStyle().Foreground(b.fg)
	r := &row{bg: b.bg}
	r.space(1).add(b.text, st)
	if b.hint {
		r.space(w-1-r.width()-9).add("e", m.th.hiBold).add(" details", st)
	}
	return r.String(w)
}
