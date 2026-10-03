package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/store"
)

type layout struct{ w, h int }

type hint struct{ key, desc string }

// body is a view's output plus the frame decorations it controls.
type body struct {
	lines       []string
	status      string
	statusStyle lipgloss.Style
	up, down    int // rows hidden above/below, shown in the frame separators
}

// render draws the whole screen: exactly m.h lines of exactly m.w cells.
func (m *Model) render() string {
	w, h := m.w, m.h
	if w <= 0 || h <= 0 {
		return ""
	}
	if w < minW || h < minH {
		return m.tooSmall(w, h)
	}
	bn := m.banner(w)
	lay := layout{w: w, h: h - 6}
	if bn != nil {
		lay.h -= 2
	}
	rows := m.rows()
	b := m.renderBody(lay, rows)
	lines := make([]string, lay.h)
	for i := range lines {
		ln := ""
		if i < len(b.lines) {
			ln = b.lines[i]
		}
		lines[i] = fit(ln, w)
	}
	if pop := m.popup(lay, rows); len(pop) > 0 {
		x := max(0, (w-format.Width(pop[0]))/2)
		y := max(0, (lay.h-len(pop))/2)
		lines = overlay(lines, pop, x, y, w)
	}
	out := make([]string, 0, h)
	out = append(out, m.header(w), m.tabs(w), m.separator(w, "▲", b.up))
	if bn != nil {
		out = append(out, m.bannerLine(w, bn), strings.Repeat(" ", w))
	}
	out = append(out, lines...)
	out = append(out, m.separator(w, "▼", b.down), m.statusLine(w, b, len(rows)), m.hintLine(w))
	return strings.Join(out, "\n")
}

// bodyTop is the screen row where the body starts.
func (m *Model) bodyTop() int {
	if m.banner(m.w) != nil {
		return 5
	}
	return 3
}

func (m *Model) header(w int) string {
	status, st, info := m.headerStatus()
	url := m.cfg.Target.URL
	base := 9 + format.Width(status) + 16
	showURL := base+format.Width(url)+2 <= w
	rem := w - base - 2
	if showURL {
		rem -= format.Width(url) + 2
	}
	parts := strings.Split(info, " · ")
	for len(parts) > 0 && format.Width(strings.Join(parts, " · ")) > rem {
		parts = parts[:len(parts)-1]
	}
	r := &row{}
	r.add(" promtop ", m.th.badge).space(2)
	if showURL {
		r.add(url, m.th.fg).space(2)
	}
	r.add(status, st)
	if len(parts) > 0 {
		r.space(2).add(strings.Join(parts, " · "), m.th.dim)
	}
	clock := format.Clock(m.now())
	r.space(max(1, w-r.width()-format.Width(clock))).add(clock, m.th.dim)
	return r.String(w)
}

func (m *Model) headerStatus() (string, lipgloss.Style, string) {
	sc := &m.st.Scrape
	now := m.now()
	switch st := sc.State(); {
	case m.paused:
		return "❚❚ paused", m.th.amber, m.liveInfo()
	case st == store.Down:
		return "✕ down", m.th.red, m.lastOK(now) + " · " + strconv.Itoa(sc.Fails) + " failed · retry in " + m.retryIn(now)
	case st == store.Degraded:
		return "● degraded", m.th.amber, m.lastOK(now) + " · " + strconv.Itoa(sc.Fails) + " failed · retrying every " + format.Interval(m.cfg.Interval)
	case st == store.NoData:
		code, _, _ := strings.Cut(sc.Status, " ")
		return "● no data", m.th.amber, "HTTP " + code + " · " + sc.ContentType + " · " + strconv.Itoa(sc.Bytes) + " bytes · 0 samples"
	case st == store.Connecting:
		return "● connecting", m.th.dim, "every " + format.Interval(m.cfg.Interval)
	}
	return "● live", m.th.green, m.liveInfo()
}

func (m *Model) liveInfo() string {
	sc := &m.st.Scrape
	tail := "scrape #" + strconv.FormatUint(sc.OKCount, 10)
	if m.st.HighCard() {
		tail = format.Size(sc.Bytes)
	}
	return "every " + format.Interval(m.cfg.Interval) + " · " + format.Millis(sc.Duration) + " · " +
		format.Int(sc.Samples) + " samples · " + tail
}

func (m *Model) lastOK(now time.Time) string {
	if m.st.Scrape.OKCount == 0 {
		return "no successful scrape"
	}
	return "last ok " + format.Dur(now.Sub(m.st.Scrape.LastOK)) + " ago"
}

func (m *Model) retryIn(now time.Time) string {
	s := int(math.Ceil(m.st.Scrape.NextTry.Sub(now).Seconds()))
	return strconv.Itoa(max(0, s)) + "s"
}

func (m *Model) tabs(w int) string {
	r := &row{}
	for i, name := range viewNames {
		if i > 0 {
			r.space(1)
		}
		k := strconv.Itoa(i + 1)
		if View(i) == m.view {
			r.add(" "+k+" "+name+" ", m.th.tabActive)
		} else {
			r.space(1).add(k, m.th.cyan).space(1).add(name, m.th.mid).space(1)
		}
	}
	return r.String(w)
}

// tabAt maps a click on the tab row to a view.
func (m *Model) tabAt(x int) (View, bool) {
	pos := 0
	for i, name := range viewNames {
		wd := format.Width(name) + 4 // " k name "
		if x >= pos && x < pos+wd {
			return View(i), true
		}
		pos += wd + 1
	}
	return 0, false
}

// separator is a full-width rule, optionally with "▲/▼ N more" at the right.
func (m *Model) separator(w int, glyph string, n int) string {
	r := &row{}
	if n <= 0 {
		return r.add(strings.Repeat("─", w), m.th.line).String(w)
	}
	mk := " " + glyph + " " + format.Int(n) + " more "
	r.add(strings.Repeat("─", max(0, w-format.Width(mk)-1)), m.th.line).add(mk, m.th.cyan).add("─", m.th.line)
	return r.String(w)
}

func (m *Model) statusLine(w int, b body, matches int) string {
	left, lst := b.status, b.statusStyle
	switch {
	case m.filtering:
		left, lst = "/"+m.filter+"▌", m.th.amber
	case m.filter != "":
		left, lst = fmt.Sprintf("filter \"%s\" · %d match · esc to clear", m.filter, matches), m.th.amber
	case m.st.Seq() > 0 && m.st.Scrape.Samples == 0:
		left, lst = "waiting for series · still scraping every "+format.Interval(m.cfg.Interval), m.th.dim
	}
	rate := "as rate/s"
	if !m.showRate {
		rate = "raw"
	}
	right := fmt.Sprintf("window %s · counters %s", windowLabel(m.window), rate)
	if format.Width(left)+format.Width(right)+4 > w {
		right = ""
	}
	if format.Width(left) > w-2 {
		left = format.Truncate(left, w-2)
	}
	r := &row{}
	r.add(left, lst)
	if right != "" {
		r.space(w-r.width()-format.Width(right)).add(right, m.th.dim)
	}
	return r.String(w)
}

func (m *Model) viewHints() []hint {
	switch m.view {
	case ViewTable:
		return []hint{{"↑ ↓", "move"}, {"PgUp/Dn", "page"}, {"+/-", "window"}, {"space", "pin"}, {"a", "all/pinned"}, {"/", "filter"}, {"s", "sort"}, {"r", "rate"}, {"enter", "details"}}
	case ViewHistory:
		return []hint{{"↑ ↓", "scroll"}, {"PgUp/Dn", "page"}, {"+/-", "window"}, {"[ ]", "series"}, {"enter", "details"}, {"r", "rate"}, {"space", "pin"}}
	case ViewGraph:
		return []hint{{"↑ ↓", "series"}, {"+/-", "window"}, {"enter", "details"}, {"r", "rate"}, {"space", "pin"}}
	case ViewHeatmap:
		mode := "by series"
		if m.heatBySeries {
			mode = "by family"
		}
		gauge, life := m.heatScope()
		if gauge {
			return []hint{{"↑ ↓", "histogram"}, {"+/-", "window"}, {"m", mode}}
		}
		scope := "since start"
		if life {
			scope = "window"
		}
		return []hint{{"↑ ↓", "histogram"}, {"+/-", "window"}, {"m", mode}, {"c", scope}}
	case ViewSeries:
		switch {
		case m.w >= 146:
			return []hint{{"↑ ↓", "family"}, {"PgUp/Dn", "page"}, {"+/-", "window"}}
		case m.famOpen:
			return []hint{{"esc", "back"}, {"↑ ↓", "prev/next"}, {"PgUp/Dn", "page"}}
		}
		return []hint{{"↑ ↓", "family"}, {"PgUp/Dn", "page"}, {"enter", "open"}}
	case ViewRaw:
		wrap := "wrap"
		if m.rawWrap {
			wrap = "no wrap"
		}
		return []hint{{"↑ ↓", "scroll"}, {"PgUp/Dn", "page"}, {"w", wrap}}
	}
	return nil
}

// hintLine: "e error" while failing → view hints → p/←→/q. View hints drop
// from the end until the line fits; the last three always stay.
func (m *Model) hintLine(w int) string {
	var hs []hint
	if m.failing() {
		hs = append(hs, hint{"e", "error"})
	}
	hs = append(hs, m.viewHints()...)
	pause := "pause"
	if m.paused {
		pause = "resume"
	}
	tail := []hint{{"p", pause}, {"← →", "view"}, {"q", "quit"}}
	total := func(n int) int {
		t := 0
		for _, h := range append(append([]hint{}, hs[:n]...), tail...) {
			t += format.Width(h.key) + 1 + format.Width(h.desc) + 3
		}
		return t
	}
	n := len(hs)
	for n > 0 && total(n) > w {
		n--
	}
	r := &row{}
	for i, h := range append(append([]hint{}, hs[:n]...), tail...) {
		if i > 0 {
			r.space(3)
		}
		r.add(h.key, m.th.cyan).space(1).add(h.desc, m.th.dim)
	}
	return r.String(w)
}

func (m *Model) renderBody(lay layout, rows []*store.Series) body {
	switch {
	case m.st.Seq() == 0:
		msg := format.Truncate("waiting for the first successful scrape of "+m.cfg.Target.URL, lay.w)
		return body{lines: center([]string{m.th.dim.Render(msg)}, lay.w, lay.h), status: "connecting", statusStyle: m.th.dim}
	case m.st.Scrape.Samples == 0:
		return m.viewEmpty(lay)
	}
	switch m.view {
	case ViewHistory:
		return m.viewHistory(lay, rows)
	case ViewGraph:
		return m.viewGraph(lay, rows)
	case ViewHeatmap:
		return m.viewHeatmap(lay)
	case ViewSeries:
		return m.viewSeries(lay)
	case ViewRaw:
		return m.viewRaw(lay)
	}
	return m.viewTable(lay, rows)
}

// viewEmpty is the "no metrics" panel for a 200 response without samples.
func (m *Model) viewEmpty(lay layout) body {
	sc := &m.st.Scrape
	b := box{w: min(78, lay.w-4), pad: 2, title: "no metrics", titleStyle: m.th.amber, border: m.th.line}
	in := b.inner()
	code, _, _ := strings.Cut(sc.Status, " ")
	text := func(s string, st lipgloss.Style) string { return (&row{}).left(s, in, st).String(in) }
	bullet := func(s string) string { return (&row{}).add("·  ", m.th.cyan).add(s, m.th.fg).String(in) }
	b.lines = []string{
		text("The endpoint responded, but the payload contains no samples.", m.th.hiBold),
		text("HTTP "+code+" · Content-Type: "+sc.ContentType+" · "+strconv.Itoa(sc.Bytes)+" bytes", m.th.dim),
		"",
		text("Things to check", m.th.mid),
		bullet("is the path right? exporters usually serve /metrics"),
		bullet("are collectors registered with the registry being served?"),
		bullet("does --match exclude everything?"),
		"",
		text("Still scraping — views fill in as soon as series appear.", m.th.dim),
	}
	return body{lines: center(b.render(), lay.w, lay.h)}
}

func (m *Model) tooSmall(w, h int) string {
	msg := format.Truncate(fmt.Sprintf("terminal too small · need %d×%d, have %d×%d", minW, minH, w, h), w)
	lines := center([]string{m.th.amber.Render(msg)}, w, h)
	for i := range lines {
		lines[i] = fit(lines[i], w)
	}
	return strings.Join(lines, "\n")
}
