package ui

import (
	"math"
	"strconv"
	"time"

	"github.com/krisiasty/promtop/internal/expo"
	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/stats"
	"github.com/krisiasty/promtop/internal/store"
)

// historyPanelMin is the narrowest stats/distribution panel worth drawing.
const historyPanelMin = 40

type historyEntry struct {
	i, prev int // sample index and preceding valid sample; i = -1 for a gap
	gap     int
}

// historyEntries covers every valid sample in the selected window, newest
// first. Failed scrapes between adjacent samples appear as a single gap row.
func (m *Model) historyEntries(vals []float64) ([]historyEntry, float64, float64, int) {
	var idx []int
	lo, hi := math.Inf(1), math.Inf(-1)
	for i, v := range vals {
		if !math.IsNaN(v) {
			idx = append(idx, i)
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	fails := m.st.Failures(m.st.Window(m.window)) // aligned with vals
	out := make([]historyEntry, 0, len(idx))
	for k := len(idx) - 1; k >= 0; k-- {
		prev := -1
		if k > 0 {
			prev = idx[k-1]
		}
		out = append(out, historyEntry{i: idx[k], prev: prev})
		if prev < 0 {
			continue
		}
		gap := 0
		for _, n := range fails[prev+1 : idx[k]+1] {
			gap += n
		}
		if gap > 0 {
			out = append(out, historyEntry{i: -1, gap: gap})
		}
	}
	return out, lo, hi, len(idx)
}

func (m *Model) historyMaxOff() int {
	s, _ := m.selected(m.rows())
	if s == nil {
		return 0
	}
	vals, _ := m.values(s)
	entries, _, _, _ := m.historyEntries(vals)
	return max(0, len(entries)-m.pageSize())
}

func (m *Model) viewHistory(lay layout, rows []*store.Series) body {
	th := &m.th
	s, _ := m.selected(rows)
	if s == nil {
		return body{lines: []string{th.cyanBold.Render("No series selected")}, status: "no series selected", statusStyle: th.fg}
	}
	vals, ts := m.values(s)
	u := m.unit(s)
	pw := lay.w - 83
	wide := pw >= historyPanelMin
	entries, lo, hi, nSamples := m.historyEntries(vals)
	vis := lay.h - 3
	off := clamp(m.histOff, 0, max(0, len(entries)-vis))
	n := strconv.Itoa(nSamples)
	t := &row{}
	t.add(s.Name, th.cyanBold).add(expo.LabelString(s.Labels), th.dim).space(2)
	if wide {
		t.add("window "+windowLabel(m.window)+" · "+n+" samples · "+m.typeWord(s), th.dim)
	} else {
		t.add("window "+windowLabel(m.window)+" · "+n+" · "+m.typeWord(s), th.dim).space(2).add("enter", th.cyan).add(" stats", th.dim)
	}
	if ft := m.fullTime(s); ft != "" {
		t.space(2).add(ft, th.fg)
	}
	hdr := (&row{}).right("AGE", 6, th.dim).space(2).left("TIME", 10, th.dim).right("VALUE", 12, th.dim).
		right("Δ", 11, th.dim).right("Δ%", 9, th.dim).space(2).left("RANGE (window)", 28, th.dim).String(80)
	left := append([]string{"", hdr}, m.historyRows(vals, ts, u, entries, lo, hi, off, vis)...)
	var right []string
	if wide {
		right = m.historyPanels(m.plotStats(s, vals), vals, u, pw)
	}
	lines := []string{t.String(lay.w)}
	for i := 0; i < lay.h-1; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if wide {
			l = fit(l, 80) + "   " + r
		}
		lines = append(lines, l)
	}
	return body{lines: lines, status: "▸ " + s.ID, statusStyle: th.fg,
		up: off, down: max(0, len(entries)-off-vis)}
}

// historyRows renders a page of the selected window's samples.
func (m *Model) historyRows(vals []float64, ts []time.Time, u format.Unit, entries []historyEntry,
	lo, hi float64, off, limit int) []string {
	th := &m.th
	latest := m.st.LastTime()
	var out []string
	for j := off; j < min(len(entries), off+limit); j++ {
		e := entries[j]
		if e.i < 0 {
			word := "scrapes"
			if e.gap == 1 {
				word = "scrape"
			}
			out = append(out, (&row{}).space(52).add("⋯ "+strconv.Itoa(e.gap)+" "+word+" failed", th.amber).String(80))
			continue
		}
		i := e.i
		v := vals[i]
		r := &row{}
		if j == 0 {
			r.bg = th.c.sel
		}
		age := "now"
		if d := latest.Sub(ts[i]); d >= time.Second/2 {
			age = "-" + format.Dur(d)
		}
		r.right(age, 6, th.dim).space(2).left(format.Clock(ts[i]), 10, th.prev).right(m.num(v, u), 12, th.hiBold)
		d, dp, dst := "", "", th.dim
		if e.prev >= 0 {
			p := vals[e.prev]
			dd := v - p
			d = m.signed(dd, u)
			if p != 0 {
				sign := "+"
				if dd < 0 {
					sign = format.Minus
				}
				dp = sign + strconv.FormatFloat(math.Abs(dd/p*100), 'f', 1, 64) + "%"
			}
			switch {
			case dd > 0:
				dst = th.green
			case dd < 0:
				dst = th.red
			}
		}
		frac := 1.0
		if hi > lo {
			frac = (v - lo) / (hi - lo)
		}
		r.right(d, 11, dst).right(dp, 9, dst).space(2).add(format.Bar(frac, 28), th.cyan)
		out = append(out, r.String(80))
	}
	return out
}

func (m *Model) historyPanels(x stats.Stats, vals []float64, u format.Unit, pw int) []string {
	th := &m.th
	sb := box{w: pw, pad: 1, title: "stats · window " + windowLabel(m.window), titleStyle: th.dim, border: th.line}
	sb.lines = grid([][2]string{
		{"current", m.num(x.Cur, u)}, {"min", m.num(x.Min, u)}, {"previous", m.num(x.Prev, u)},
		{"max", m.num(x.Max, u)}, {"delta", m.signed(x.Delta, u)}, {"mean", m.num(x.Mean, u)},
		{"median", m.num(x.Median, u)}, {"stddev", m.num(x.Stddev, u)}, {"p90", m.num(x.P90, u)},
		{"p95", m.num(x.P95, u)}, {"p99", m.num(x.P99, u)}, {"samples", strconv.Itoa(x.N)},
	}, 2, sb.inner(), 10, th.dim, th.hi)
	db := box{w: pw, pad: 1, title: "distribution · window", titleStyle: th.dim, border: th.line}
	in := db.inner()
	bins := stats.Distribution(vals, 10)
	most := 0
	for _, b := range bins {
		most = max(most, b.Count)
	}
	rangeW, barW := 24, 26  // design widths at 146 columns
	if in < rangeW+barW+6 { // narrower panels (w between 123 and ~140): scale down
		rangeW = in / 3
		barW = in - rangeW - 6
	}
	for _, b := range bins {
		bar := ""
		if b.Count > 0 {
			bar = format.Bar(float64(b.Count)/float64(most), barW-2)
		}
		db.lines = append(db.lines, (&row{}).left(m.num(b.Lo, u)+" – "+m.num(b.Hi, u), rangeW, th.prev).
			left(bar, barW, th.magenta).right(strconv.Itoa(b.Count), in-rangeW-barW, th.stat).String(in))
	}
	return append(sb.render(), db.render()...)
}
