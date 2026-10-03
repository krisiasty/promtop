package ui

import (
	"math"
	"strconv"
	"time"

	"github.com/krisiasty/promtop/internal/expo"
	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/store"
)

type tableCols struct {
	prev, min, max, p50, p95, trend bool
	name                            int
}

// tableLayout applies the column priorities: ≥146 all columns, ≥100 drops
// PREV, P50, P95 and TREND, below 100 also MIN and MAX. NAME takes the rest.
func tableLayout(w int) tableCols {
	c := tableCols{prev: w >= 146, p50: w >= 146, p95: w >= 146, trend: w >= 146, min: w >= 100, max: w >= 100}
	c.name = w - 3 - 4 - 11 - 10 - 9 - 9 // mark, TYP, CURRENT, Δ, AVG, P99
	for _, col := range []struct {
		on bool
		w  int
	}{{c.prev, 10}, {c.min, 9}, {c.max, 9}, {c.p50, 9}, {c.p95, 9}, {c.trend, 18}} {
		if col.on {
			c.name -= col.w
		}
	}
	return c
}

func (m *Model) viewTable(lay layout, rows []*store.Series) body {
	th := &m.th
	c := tableLayout(lay.w)
	vis := lay.h - 1
	_, cur := m.selected(rows)
	off := clampOffset(cur, vis, len(rows))
	sk := m.effectiveSort()
	ind := func(on bool, s string) string {
		if on {
			return s
		}
		return ""
	}
	h := &row{}
	h.space(3).left("NAME"+ind(sk == SortName, " ▲"), c.name, th.dim).left("TYP", 4, th.dim).
		right("CURRENT"+ind(sk == SortCurrent, " ▼"), 11, th.dim)
	if c.prev {
		h.right("PREV", 10, th.dim)
	}
	delta := "Δ"
	if sk == SortDeltaPct {
		delta = "Δ ▼"
	}
	h.right(delta, 10, th.dim)
	if c.min {
		h.right("MIN", 9, th.dim)
	}
	if c.max {
		h.right("MAX", 9, th.dim)
	}
	h.right("AVG", 9, th.dim)
	if c.p50 {
		h.right("P50", 9, th.dim)
	}
	if c.p95 {
		h.right("P95", 9, th.dim)
	}
	h.right("P99"+ind(sk == SortP99, " ▼"), 9, th.dim)
	if c.trend {
		h.space(2).add("TREND", th.dim)
	}
	lines := []string{h.String(lay.w)}
	if len(rows) == 0 {
		lines = append(lines, "", "   "+th.dim.Render("No series match. Esc clears the filter, a shows all series."))
	}
	stale := m.st.Scrape.State() == store.Down
	now := m.now()
	for i := off; i < min(len(rows), off+vis); i++ {
		lines = append(lines, m.tableRow(rows[i], i == cur, stale, now, c, lay.w))
	}
	return body{lines: lines, status: m.tableStatus(rows, off, vis), statusStyle: th.fg,
		up: off, down: max(0, len(rows)-off-vis)}
}

func (m *Model) tableRow(s *store.Series, selected, stale bool, now time.Time, c tableCols, w int) string {
	th := &m.th
	x := m.stats(s)
	u := m.unit(s)
	r := &row{faint: stale}
	if selected {
		r.bg = th.c.sel
	}
	cursor, pin := " ", " "
	if selected {
		cursor = "▸"
	}
	if m.st.Pinned(s.Key) {
		pin = "●"
	}
	r.add(cursor+pin+" ", th.cyan)
	name, labels := splitName(s.Name, expo.LabelString(s.Labels), c.name-1)
	r.add(name, th.fg).add(labels, th.dim).space(c.name - format.Width(name) - format.Width(labels))
	tag, tst := th.typeTag(s.Type)
	r.left(tag, 4, tst).right(m.num(x.Cur, u), 11, th.hiBold)
	if c.prev {
		r.right(m.num(x.Prev, u), 10, th.prev)
	}
	d, dst := m.signed(x.Delta, u), th.dim
	switch {
	case s.Type == expo.Counter && !s.ResetAt.IsZero() && now.Sub(s.ResetAt) < store.ResetHold:
		d, dst = "↺ reset", th.amber
	case x.Delta > 0:
		dst = th.green
	case x.Delta < 0:
		dst = th.red
	}
	r.right(d, 10, dst)
	for _, col := range []struct {
		on bool
		v  string
	}{{c.min, m.num(x.Min, u)}, {c.max, m.statMax(s, x.Max, u)}, {true, m.num(x.Mean, u)}, {c.p50, m.num(x.P50, u)},
		{c.p95, m.num(x.P95, u)}, {true, m.num(x.P99, u)}} {
		if col.on {
			r.right(col.v, 9, th.stat)
		}
	}
	if c.trend {
		vals, _ := m.values(s)
		if s.Hist != nil && !s.Hist.IsGauge() {
			vals = m.histRates(s.Hist) // observations/s
		}
		r.space(2).add(format.Spark(tail(vals, 64), 16, math.NaN(), math.NaN()), th.cyan)
	}
	return r.String(w)
}

func (m *Model) tableStatus(rows []*store.Series, off, vis int) string {
	s := format.Int(len(rows)) + "/" + format.Int(len(m.st.Series())) + " series"
	if m.onlyPinned {
		s += " (pinned only)"
	}
	sortName := sortNames[m.sort]
	if m.st.HighCard() {
		sortName = "name (value sort off above 10k)"
	}
	s += " · " + strconv.Itoa(m.st.PinCount()) + " pinned · sort " + sortName
	if len(rows) > vis {
		s += " · rows " + strconv.Itoa(off+1) + "–" + strconv.Itoa(off+vis) + " of " + format.Int(len(rows))
	}
	return s
}

// splitName fits name+labels into w cells: the name wins, labels get the
// remainder, each truncated with "…".
func splitName(name, labels string, w int) (string, string) {
	if format.Width(name) >= w {
		return format.Truncate(name, w), ""
	}
	return name, format.Truncate(labels, w-format.Width(name))
}

func tail(v []float64, n int) []float64 {
	if len(v) > n {
		return v[len(v)-n:]
	}
	return v
}
