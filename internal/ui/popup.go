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

const maxLabelRows = 6

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

// labVisible is how many label rows the details popup shows (1..6).
func (m *Model) labVisible(bodyH, nLabels int) int {
	statRows := (14 + statCols(m.w) - 1) / statCols(m.w)
	lv := clamp(bodyH-(12+statRows), 1, maxLabelRows)
	return min(lv, max(1, (nLabels+1)/2))
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
	return max(0, (len(s.Labels)+1)/2-m.labVisible(m.bodyHeight(), len(s.Labels)))
}

// histLabelRows is how many label rows a histogram's popup shows (1..3); the
// bucket list, not the labels, scrolls.
func histLabelRows(s *store.Series) int { return clamp((len(s.Labels)+1)/2, 1, 3) }

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
	return bodyH - (15 + statRows + histLabelRows(s))
}

func (m *Model) detailsPopup(lay layout, s *store.Series, idx, n int) []string {
	th := &m.th
	pw := min(106, lay.w-4)
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

	lv := m.labVisible(lay.h, len(s.Labels))
	rowsN := (len(s.Labels) + 1) / 2
	lo := clamp(m.labOff, 0, max(0, rowsN-lv))
	if s.Hist != nil {
		lv, lo = histLabelRows(s), 0
	}
	lb := box{w: inner, pad: 1, title: "labels · " + strconv.Itoa(len(s.Labels)), titleStyle: th.dim, border: th.line,
		topRightStyle: th.cyan, markStyle: th.cyan}
	if len(s.Labels) == 0 {
		lb.lines = []string{(&row{}).left("no labels", lb.inner(), th.dim).String(lb.inner())}
	} else {
		lb.lines = labelGrid(s.Labels[lo*2:min(len(s.Labels), (lo+lv)*2)], lb.inner(), th)
		if above := lo * 2; above > 0 {
			lb.topRight = "▲ " + strconv.Itoa(above) + " more"
		}
		if below := len(s.Labels) - (lo+lv)*2; below > 0 {
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
	case rowsN > lv:
		scroll = " scroll labels"
	}

	fr := &row{}
	fr.add("enter/esc", th.cyan).add(" close", th.dim).space(3).add("space", th.cyan).add(" pin", th.dim)
	if scroll != "" {
		fr.space(2).add("↑ ↓", th.cyan).add(scroll, th.dim)
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

// labelGrid shows labels two per row: key (21 cells, amber) and "value".
func labelGrid(ls []expo.Label, width int, th *theme) []string {
	const gap = 3
	cw := (width - gap) / 2
	var out []string
	for i := 0; i < len(ls); i += 2 {
		r := &row{}
		for c := 0; c < 2; c++ {
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
			r.left(l.Name, 21, th.amber).left("\""+l.Value+"\"", w-21, th.fg)
		}
		out = append(out, r.String(width))
	}
	return out
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
