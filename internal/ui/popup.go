package ui

import (
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/krisiasty/promtop/internal/expo"
	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/store"
)

const hStep = 8 // cells ← → scroll the details popup's label values

// popup returns the open popup's lines; render centres it over the body.
func (m *Model) popup(lay layout, rows []*store.Series) []string {
	switch {
	case m.errOpen && m.failing():
		return m.errorPopup(lay)
	case m.detailOpen && m.view <= ViewGraph:
		if s, i := m.selected(rows); s != nil {
			return m.detailsPopup(lay, s, i, len(rows))
		}
	}
	return nil
}

func (m *Model) bodyHeight() int {
	h := m.h - 6
	if m.banner(m.w) != nil {
		h -= 2
	}
	return h
}

func statCols(w int) int {
	if w >= 100 {
		return 4
	}
	return 2
}

// labelFit is how the details popup lays out a series' labels: two per row
// when every label fits whole in half the box, else one per row with the
// values scrolling sideways (hMax cells) past what the terminal can show.
type labelFit struct {
	pw         int // popup width
	cols, keyW int // label columns and key cells per column
	rows, hMax int
}

// fitLabels widens the popup beyond its usual 106 cells, up to the terminal
// width w, so the longest key="value" pair fits on one line.
func fitLabels(w int, ls []expo.Label) labelFit {
	maxK, maxV := 0, 0
	for _, l := range ls {
		maxK = max(maxK, format.Width(oneLine(l.Name)))
		maxV = max(maxV, format.Width(oneLine(l.Value))+2)
	}
	f := labelFit{pw: min(w-4, max(106, maxK+2+maxV+8)), cols: 1}
	in := f.pw - 8 // inside the labels box
	if half := (in - 3) / 2; maxK+2+maxV <= half {
		f.cols, f.keyW = 2, maxK+2
	} else {
		f.keyW = max(1, min(maxK+2, in*2/5))
		f.hMax = max(0, maxV-(in-f.keyW))
	}
	f.rows = (len(ls) + f.cols - 1) / f.cols
	return f
}

// labVisible is how many of rows label rows the details popup shows: as many
// as the body height leaves room for, at least one.
func (m *Model) labVisible(bodyH, rows int) int {
	statRows := (14 + statCols(m.w) - 1) / statCols(m.w)
	return clamp(bodyH-(12+statRows), 1, max(1, rows))
}

func (m *Model) labMax() int {
	s, _ := m.selected(m.rows())
	switch {
	case s == nil:
		return 0
	case s.Hist != nil: // ↑↓ scroll the buckets instead
		if !m.bucketRoom(m.bodyHeight(), s) {
			return 0
		}
		return max(0, len(s.Hist.LE)-m.bucketVisible(m.bodyHeight(), s))
	}
	rows := fitLabels(m.w, s.Labels).rows
	return max(0, rows-m.labVisible(m.bodyHeight(), rows))
}

// labHMax is how far ← → can scroll the selected series' label values.
func (m *Model) labHMax() int {
	if s, _ := m.selected(m.rows()); s != nil {
		return fitLabels(m.w, s.Labels).hMax
	}
	return 0
}

// histLabelRows is how many label rows a histogram's popup shows (1..3); the
// bucket list, not the labels, scrolls.
func histLabelRows(rows int) int { return clamp(rows, 1, 3) }

// bucketRoom reports whether a histogram's popup fits at least one bucket
// row; on short terminals the buckets box is left out.
func (m *Model) bucketRoom(bodyH int, s *store.Series) bool { return m.bucketSpace(bodyH, s) >= 1 }

// bucketVisible is how many bucket rows a histogram's popup shows.
func (m *Model) bucketVisible(bodyH int, s *store.Series) int {
	return clamp(m.bucketSpace(bodyH, s), 1, max(1, len(s.Hist.LE)))
}

// bucketSpace is the body height left for bucket rows once the popup's other
// parts (and the buckets box's borders and header) are placed.
func (m *Model) bucketSpace(bodyH int, s *store.Series) int {
	statRows := (14 + statCols(m.w) - 1) / statCols(m.w)
	return bodyH - (15 + statRows + histLabelRows(fitLabels(m.w, s.Labels).rows))
}

func (m *Model) detailsPopup(lay layout, s *store.Series, idx, n int) []string {
	th := &m.th
	lf := fitLabels(lay.w, s.Labels)
	pw := lf.pw
	inner := pw - 4
	x, life := m.statsLife(s)
	u := m.unit(s)
	text := func(v string, st lipgloss.Style) string { return (&row{}).left(v, inner, st).String(inner) }
	name := s.Name
	if len(s.Labels) > 0 {
		name += "{…}"
	}
	tr := &row{}
	_, tst := th.typeTag(s.Type)
	tr.add(s.Type.String(), tst).space(2).add("unit: "+unitName(s.Unit), th.dim)
	if ft := m.fullTime(s); ft != "" {
		tr.space(2).add(ft, th.fg)
	}
	if m.st.Pinned(s.Key) {
		tr.space(2).add("● pinned", th.amber)
	}
	lines := []string{text(name, th.hiBold), tr.String(inner), text("# HELP "+s.Help, th.dim), ""}

	title := "window " + windowLabel(m.window)
	if s.Type == expo.Counter && m.showRate {
		title += " · rate/s"
	}
	if life {
		title += " · since start"
	}
	if s.Hist != nil && s.Hist.IsGauge() {
		title = "current distribution"
	}
	samples, raw := [2]string{"samples", strconv.Itoa(x.N)}, [2]string{"raw value", format.Raw(s.Last())}
	if s.Hist != nil {
		v := m.histView(s.Hist)
		samples, raw = [2]string{"observed", format.SI(v.count)}, [2]string{"sum", m.num(v.sum, u)}
	}
	sb := box{w: inner, pad: 1, title: title, titleStyle: th.dim, border: th.line}
	sb.lines = grid([][2]string{
		{"current", m.num(x.Cur, u)}, {"min", m.num(x.Min, u)}, {"previous", m.num(x.Prev, u)}, {"max", m.statMax(s, x.Max, u)},
		{"delta", m.signed(x.Delta, u)}, {"mean", m.num(x.Mean, u)}, {"median", m.num(x.Median, u)}, {"stddev", m.num(x.Stddev, u)},
		{"p90", m.num(x.P90, u)}, {"p95", m.num(x.P95, u)}, {"p99", m.num(x.P99, u)}, samples,
		{"last change", m.lastChange(s)}, raw, // first in its row: the widest value gets the wider column
	}, statCols(lay.w), sb.inner(), 11, th.dim, th.hi)
	lines = append(lines, sb.render()...)

	lv := m.labVisible(lay.h, lf.rows)
	lo := clamp(m.labOff, 0, max(0, lf.rows-lv))
	if s.Hist != nil {
		lv, lo = histLabelRows(lf.rows), 0
	}
	ho := clamp(m.labHOff, 0, lf.hMax)
	lt := "labels · " + strconv.Itoa(len(s.Labels))
	switch { // which way the values can still scroll
	case lf.hMax == 0:
	case ho == 0:
		lt += " · ▶"
	case ho == lf.hMax:
		lt += " · ◀"
	default:
		lt += " · ◀ ▶"
	}
	lb := box{w: inner, pad: 1, title: lt, titleStyle: th.dim, border: th.line,
		topRightStyle: th.cyan, markStyle: th.cyan}
	if len(s.Labels) == 0 {
		lb.lines = []string{(&row{}).left("no labels", lb.inner(), th.dim).String(lb.inner())}
	} else {
		first, last := lo*lf.cols, min(len(s.Labels), (lo+lv)*lf.cols)
		lb.lines = labelGrid(s.Labels[first:last], lf, ho, lb.inner(), th)
		if first > 0 {
			lb.topRight = "▲ " + strconv.Itoa(first) + " more"
		}
		if below := len(s.Labels) - last; below > 0 {
			lb.bottomRight = "▼ " + strconv.Itoa(below) + " more"
		}
	}
	lines = append(lines, lb.render()...)

	scroll := ""
	switch {
	case s.Hist != nil && m.bucketRoom(lay.h, s):
		bv := m.bucketVisible(lay.h, s)
		lines = append(lines, m.bucketBox(s, inner, bv).render()...)
		if len(s.Hist.LE) > bv {
			scroll = " scroll buckets"
		}
	case lf.rows > lv:
		scroll = " scroll labels"
	}

	fr := &row{}
	fr.add("enter/esc", th.cyan).add(" close", th.dim).space(3).add("space", th.cyan).add(" pin", th.dim)
	if scroll != "" {
		fr.space(2).add("↑ ↓", th.cyan).add(scroll, th.dim)
	}
	if lf.hMax > 0 {
		fr.space(2).add("← →", th.cyan).add(" scroll values", th.dim)
	}
	lines = append(lines, th.line.Render(strings.Repeat("─", inner)), fr.String(inner))
	return box{w: pw, pad: 1, title: "series details", titleStyle: th.cyan,
		topRight: strconv.Itoa(idx+1) + " / " + strconv.Itoa(n), topRightStyle: th.dim, border: th.cyan, lines: lines}.render()
}

// bucketBox lists a histogram's buckets from the popup's scroll offset: upper
// bound, cumulative count and share (window or lifetime, like the stats), the
// cumulative rate over the window (counters only), and the bucket's own count
// as a bar.
func (m *Model) bucketBox(s *store.Series, width, vis int) box {
	th := &m.th
	h := s.Hist
	v := m.histView(h)
	from, to := m.st.Window(m.window)
	var inc []float64
	secs, rateW := math.NaN(), 10
	if h.IsGauge() {
		rateW = 0 // snapshots have no rate
	} else if from > 0 {
		inc, _, _ = h.Increase(from, to)
		secs = h.Seconds(from, to)
	}
	n := len(h.LE)
	off := clamp(m.labOff, 0, max(0, n-vis))
	b := box{w: width, pad: 1, title: "buckets · " + strconv.Itoa(n), titleStyle: th.dim, border: th.line,
		topRightStyle: th.cyan, markStyle: th.cyan}
	in := b.inner()
	barW := clamp(in-10-10-8-rateW-2, 0, 30)
	hdr := (&row{}).left("le", 10, th.dim).right("count", 10, th.dim).right("share", 8, th.dim)
	if rateW > 0 {
		hdr.right("rate/s", rateW, th.dim)
	}
	b.lines = []string{hdr.String(in)}
	most, below := 0.0, 0.0
	for _, c := range v.cum {
		most, below = math.Max(most, c-below), math.Max(below, c)
	}
	total := math.NaN()
	if len(v.cum) == n && n > 0 {
		total = v.cum[n-1]
	}
	for k := off; k < min(n, off+vis); k++ {
		cnt, share, rate, bar := math.NaN(), "—", math.NaN(), ""
		if len(v.cum) == n {
			cnt = v.cum[k]
			if total > 0 {
				share = strconv.FormatFloat(cnt/total*100, 'f', 1, 64) + "%"
			}
			own := cnt
			if k > 0 {
				own -= v.cum[k-1]
			}
			if own > 0 && most > 0 {
				bar = format.Bar(own/most, barW)
			}
		}
		if len(inc) == n && secs > 0 {
			rate = inc[k] / secs
		}
		r := (&row{}).left(bucketLabel(h.LE[k], s.Unit), 10, th.prev).right(format.SI(cnt), 10, th.hi).right(share, 8, th.stat)
		if rateW > 0 {
			r.right(format.SI(rate), rateW, th.dim)
		}
		b.lines = append(b.lines, r.space(2).add(bar, th.cyan).String(in))
	}
	if off > 0 {
		b.topRight = "▲ " + strconv.Itoa(off) + " more"
	}
	if below := n - off - vis; below > 0 {
		b.bottomRight = "▼ " + strconv.Itoa(below) + " more"
	}
	return b
}

// labelGrid shows labels lf.cols per row: key (lf.keyW cells, amber) and
// "value". A single column's values are scrolled hOff cells to the left, each
// only as far as its own tail needs.
func labelGrid(ls []expo.Label, lf labelFit, hOff, width int, th *theme) []string {
	const gap = 3
	cw := width
	if lf.cols == 2 {
		cw = (width - gap) / 2
	}
	var out []string
	for i := 0; i < len(ls); i += lf.cols {
		r := &row{}
		for c := 0; c < lf.cols; c++ {
			w := cw
			if c == 1 {
				r.space(gap)
				w = width - gap - cw
			}
			if i+c >= len(ls) {
				r.space(w)
				continue
			}
			l := ls[i+c]
			v := oneLine("\"" + l.Value + "\"")
			vw := w - lf.keyW
			r.left(l.Name, lf.keyW, th.amber).left(slide(v, min(hOff, format.Width(v)-vw), vw), vw, th.fg)
		}
		out = append(out, r.String(width))
	}
	return out
}

// slide shows w cells of s starting off cells in, with "…" marking text cut
// on either side.
func slide(s string, off, w int) string {
	if off > 0 {
		s = "…" + s[len(format.Cut(s, off+1)):]
	}
	return format.Truncate(s, w)
}

func unitName(u format.Unit) string {
	switch u {
	case format.Bytes:
		return "bytes"
	case format.Seconds:
		return "seconds"
	case format.Timestamp:
		return "timestamp"
	}
	return "—"
}

func (m *Model) lastChange(s *store.Series) string {
	last := m.st.LastTime()
	if s.LastChange.IsZero() || !s.LastChange.Before(last) {
		return "this scrape"
	}
	return format.Dur(last.Sub(s.LastChange)) + " ago"
}

func (m *Model) errorPopup(lay layout) []string {
	th := &m.th
	sc := &m.st.Scrape
	col, state := th.amber, "DEGRADED"
	if sc.State() == store.Down {
		col, state = th.red, "DOWN"
	}
	pw := min(106, lay.w-4)
	inner := pw - 4
	now := m.now()
	last := "never"
	if sc.OKCount > 0 {
		last = format.Clock(sc.LastOK) + " (" + format.Dur(now.Sub(sc.LastOK)) + " ago)"
	}
	errText := ""
	if sc.LastErr != nil {
		errText = sc.LastErr.Full
	}
	kv := []struct {
		k, v string
		st   lipgloss.Style
	}{
		{"target", m.cfg.Target.URL, th.fg},
		{"state", state, col},
		{"error", errText, th.hi},
		{"last success", last, th.fg},
		{"failures", strconv.Itoa(sc.Fails) + " consecutive", th.fg},
		{"next retry", "in " + m.retryIn(now), th.fg},
		{"backoff", "every " + format.Interval(m.cfg.Interval) + " for 4 attempts, then 2s → 4s → 8s … max 30s", th.fg},
		{"timeout", format.Interval(m.cfg.Target.Timeout), th.fg},
	}
	var lines []string
	for _, e := range kv {
		for i, part := range wrapText(e.v, inner-14) {
			k := ""
			if i == 0 {
				k = e.k
			}
			lines = append(lines, (&row{}).left(k, 14, th.dim).add(part, e.st).String(inner))
		}
	}
	lines = append(lines, "")
	fixed := len(lines) + 2 + 2 + 2 // attempts box borders, rule + footer, popup borders
	na := min(len(sc.Attempts), clamp(lay.h-fixed, 1, store.MaxAttempts))
	ab := box{w: inner, pad: 1, title: "last 10 attempts", titleStyle: th.dim, border: th.line}
	aw := ab.inner()
	for _, a := range sc.Attempts[:na] {
		icon, st := "✕", th.red
		if a.OK {
			icon, st = "✓", th.green
		}
		ab.lines = append(ab.lines, (&row{}).left(format.Clock(a.At), 10, th.dim).left(icon, 3, st).
			left(a.Text, aw-22, st).right(format.Millis(a.Duration), 9, th.prev).String(aw))
	}
	lines = append(lines, ab.render()...)
	lines = append(lines, th.line.Render(strings.Repeat("─", inner)),
		(&row{}).add("e/esc", th.cyan).add(" close", th.dim).String(inner))
	return box{w: pw, pad: 1, title: "scrape error", titleStyle: col, border: col, lines: lines}.render()
}

// wrapText word-wraps plain text to w cells; words longer than w are cut.
func wrapText(s string, w int) []string {
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		for w > 0 && format.Width(word) > w {
			if line != "" {
				out = append(out, line)
				line = ""
			}
			part := format.Cut(word, w)
			out = append(out, part)
			word = word[len(part):]
		}
		switch {
		case word == "":
		case line == "":
			line = word
		case format.Width(line)+1+format.Width(word) <= w:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	if line != "" || len(out) == 0 {
		out = append(out, line)
	}
	return out
}
