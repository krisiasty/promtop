package ui

import (
	"math"

	"github.com/krisiasty/promtop/internal/expo"
	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/store"
)

func (m *Model) viewGraph(lay layout, rows []*store.Series) body {
	th := &m.th
	s, _ := m.selected(rows)
	if s == nil {
		return body{lines: []string{th.cyanBold.Render("No series selected")}, status: "no series selected", statusStyle: th.fg}
	}
	vals, times := m.values(s)
	x := m.plotStats(s, vals) // Graph plots per-scrape means, not bucket bounds.
	u := m.unit(s)
	wc := lay.w - 14
	hc := max(4, lay.h-8)
	mode := s.Type.String()
	if s.Hist != nil {
		mode = "histogram · interval mean"
		if s.Hist.IsGauge() {
			mode = "gaugehistogram · mean"
		}
	} else if s.Type == expo.Counter {
		mode = "counter · raw"
		if m.showRate {
			mode = "counter · rate/s"
		}
	}
	mode += " · window " + windowLabel(m.window)
	cur := m.num(x.Cur, u)
	title := cur
	if ft := m.fullTime(s); ft != "" {
		title = ft
	}
	t := &row{}
	t.add(s.Name, th.cyanBold).add(expo.LabelString(s.Labels), th.dim).space(2).add(mode, th.dim)
	t.space(lay.w-t.width()-format.Width(title)).add(title, th.hiBold)
	lines := []string{t.String(lay.w), ""}

	lo, hi := format.YRange(x.Min, x.Max)
	if math.IsNaN(lo) || math.IsNaN(hi) {
		lo, hi = 0, 1
	}
	step := float64(hc-1) / 4
	for i, ln := range format.BrailleTimed(vals, times, wc, hc, lo, hi, m.st.LastTime(), m.window) {
		label, ax := "", " │"
		for q := 0; q <= 4; q++ {
			if int(math.Round(float64(q)*step)) == i {
				label, ax = m.num(hi-float64(i)/float64(hc-1)*(hi-lo), u), " ┤"
			}
		}
		lines = append(lines, (&row{}).right(label, 10, th.dim).add(ax, th.axis).add(ln, th.cyan).String(lay.w))
	}
	ticks, labels := format.Axis(wc, m.window)
	lines = append(lines,
		(&row{}).space(10).add(" └", th.axis).add(ticks, th.axis).String(lay.w),
		(&row{}).space(12).add(labels, th.dim).String(lay.w),
		"")
	lg := &row{}
	lg.space(12)
	for i, kv := range [][2]string{{"cur", cur}, {"Δ", m.signed(x.Delta, u)}, {"min", m.num(x.Min, u)},
		{"avg", m.num(x.Mean, u)}, {"max", m.num(x.Max, u)}, {"p95", m.num(x.P95, u)}, {"p99", m.num(x.P99, u)}} {
		if i > 0 {
			lg.space(4)
		}
		lg.add(kv[0]+" ", th.dim).add(kv[1], th.hi)
	}
	lines = append(lines, lg.String(lay.w))
	return body{lines: lines, status: "▸ " + s.ID, statusStyle: th.fg}
}
