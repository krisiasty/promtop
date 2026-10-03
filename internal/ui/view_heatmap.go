package ui

import (
	"math"
	"slices"
	"strconv"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/krisiasty/promtop/internal/expo"
	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/stats"
	"github.com/krisiasty/promtop/internal/store"
)

// heatPanelMin is the narrowest right-hand panel worth drawing.
const heatPanelMin = 40

var heatGlyphs = []string{"░", "▒", "▓", "█"}

func (m *Model) heatSourceCount() int { return len(m.st.HeatSources(m.heatBySeries)) }

func (m *Model) viewHeatmap(lay layout) body {
	th := &m.th
	b := body{status: "last scrape " + format.Clock(m.st.LastTime()) + " · changed samples highlighted", statusStyle: th.fg}
	srcs := m.st.HeatSources(m.heatBySeries)
	if len(srcs) == 0 {
		b.lines = center([]string{th.dim.Render("no histogram metrics in this target")}, lay.w, lay.h)
		return b
	}
	idx := clamp(m.heatIdx, 0, len(srcs)-1)
	src := srcs[idx]
	from, to := m.st.Window(m.window)
	d := m.heatData(src, idx, from, to)
	pw := lay.w - 102
	wide := pw >= heatPanelMin
	ncols, lw := 89, 99
	if !wide {
		ncols, lw = lay.w-12, lay.w
	}
	cols := heatColumns(d, m.st.Times(from, to), m.st.LastTime(), m.window, ncols)
	// Reserve the title, blank, axis (2), blank, three quantile rows and the
	// summary (narrow: blank + line; wide: the quantile box needs as much)
	// first; buckets that do not fit are dropped from the +Inf side and the
	// first bucket row says how many.
	nb, hidden := len(d.LE), 0
	if room := lay.h - 10; nb > room {
		nb = max(1, room-1)
		hidden = len(d.LE) - nb
	}

	t := &row{}
	kind := "histogram"
	if d.Gauge {
		kind = "gaugehistogram"
	}
	t.add(src.Family, th.cyanBold).add(expo.LabelString(src.Labels), th.dim).space(2).
		add(kind+" · "+strconv.Itoa(len(d.LE))+" buckets · columns normalised", th.dim)
	left := []string{""}
	if hidden > 0 {
		left = append(left, (&row{}).space(10).add("▲ "+strconv.Itoa(hidden)+" more buckets", th.dim).String(lw))
	}
	for k := nb - 1; k >= 0; k-- {
		r := &row{}
		r.right(bucketLabel(d.LE[k], src.Unit), 9, th.dim).space(1)
		for _, c := range cols {
			mx := 0.0
			for _, v := range c {
				if v > mx { // NaN buckets are skipped
					mx = v
				}
			}
			if !(c[k] > 0) { // also NaN
				r.space(1)
				continue
			}
			f := math.Sqrt(c[k] / mx)
			if math.IsNaN(f) { // +Inf / +Inf
				f = 1
			}
			r.add(heatGlyphs[min(3, int(f*4))], th.heatStyle(f))
		}
		left = append(left, r.String(lw))
	}
	ticks, labels := format.Axis(ncols, m.window)
	left = append(left, (&row{}).space(10).add(ticks, th.axis).String(lw), (&row{}).space(10).add(labels, th.dim).String(lw), "")

	qs := []struct {
		k  string
		q  float64
		st lipgloss.Style
	}{{"p50", 0.5, th.green}, {"p90", 0.9, th.amber}, {"p99", 0.99, th.red}}
	qv := make([][]float64, len(qs))
	qmx := 0.0
	for i, q := range qs {
		qv[i] = make([]float64, ncols)
		for c, col := range cols {
			v := stats.HistogramQuantile(q.q, d.LE, cumulative(col))
			qv[i][c] = v
			if !math.IsNaN(v) {
				qmx = math.Max(qmx, v)
			}
		}
	}
	for i, q := range qs {
		left = append(left, (&row{}).right(q.k, 9, th.dim).space(1).add(format.Spark(qv[i], ncols, 0, qmx), q.st).String(lw))
	}

	tb := heatBuckets(d)
	cum := cumulative(tb)
	sum, count := d.Sum, d.Count
	life := !d.Gauge && m.heatLifetime(d)
	if life { // the panels show everything observed since the target started
		if le, tc, ts, tn := src.Totals(); len(tc) == len(d.LE) && slices.Equal(le, d.LE) {
			cum, tb, sum, count = tc, perBucket(tc), ts, tn
		}
	}
	tot, most := 0.0, 0.0
	if len(cum) > 0 {
		tot = cum[len(cum)-1]
	}
	for _, v := range tb {
		most = math.Max(most, v)
	}
	summary := []struct {
		k, v string
		st   lipgloss.Style
	}{
		{"p50", format.Num(stats.HistogramQuantile(0.5, d.LE, cum), src.Unit), th.green},
		{"p90", format.Num(stats.HistogramQuantile(0.9, d.LE, cum), src.Unit), th.amber},
		{"p99", format.Num(stats.HistogramQuantile(0.99, d.LE, cum), src.Unit), th.red},
		{"mean", format.Num(mean(sum, count), src.Unit), th.dim},
		{"requests/s", format.SI(d.Rate), th.dim},
	}
	scope := "window " + windowLabel(m.window)
	if life {
		scope = "since start"
		summary[4] = struct {
			k, v string
			st   lipgloss.Style
		}{"observed", format.SI(count), th.dim}
	}
	if d.Gauge {
		scope = "current distribution"
		summary[4].k, summary[4].v = "observed", format.SI(count)
	}

	var right []string
	if wide {
		bb := box{w: pw, pad: 1, title: "buckets · " + scope, titleStyle: th.dim, border: th.line}
		in := bb.inner()
		barW := min(20, in-8-7-5) // keep the count column at 5 cells even when the panel is narrow
		for k, c := range tb[:nb] {
			bar, pct := "", "0.0%"
			if c > 0 && most > 0 {
				bar = format.Bar(c/most, barW)
			}
			if tot > 0 {
				pct = strconv.FormatFloat(c/tot*100, 'f', 1, 64) + "%"
			}
			bb.lines = append(bb.lines, (&row{}).left(bucketLabel(d.LE[k], src.Unit), 8, th.prev).left(bar, barW, th.cyan).
				right(pct, 7, th.stat).right(format.SI(c), in-8-barW-7, th.dim).String(in))
		}
		if hidden > 0 { // the panel lists buckets upwards: the dropped ones are last
			bb.lines = append(bb.lines, (&row{}).add("▼ "+strconv.Itoa(hidden)+" more buckets", th.dim).String(in))
		}
		qb := box{w: pw, pad: 1, title: "histogram_quantile", titleStyle: th.dim, border: th.line}
		for _, s := range summary {
			qb.lines = append(qb.lines, (&row{}).left(s.k, 12, s.st).right(s.v, qb.inner()-12, th.hi).String(qb.inner()))
		}
		right = append(bb.render(), qb.render()...)
	} else {
		sr := &row{}
		sr.space(10)
		for i, s := range summary {
			if i > 0 {
				sr.space(3)
			}
			sr.add(s.k+" ", s.st).add(s.v, th.hi)
		}
		left = append(left, "", sr.String(lw))
	}

	b.lines = []string{t.String(lay.w)}
	for i := 0; i < lay.h-1; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if wide {
			l = fit(l, lw) + "   " + r
		}
		b.lines = append(b.lines, l)
	}
	return b
}

// heatData is src's activity over the window, cached until the next scrape
// or a window, mode or source change, so frames between scrapes (keys, clock
// ticks) do not re-sum every histogram of a family.
func (m *Model) heatData(src store.HeatSource, idx int, from, to uint64) store.HeatData {
	c := &m.heat
	if !c.ok || c.seq != m.st.Seq() || c.window != m.window || c.bySeries != m.heatBySeries || c.idx != idx {
		*c = heatCache{seq: m.st.Seq(), window: m.window, bySeries: m.heatBySeries, idx: idx, ok: true, d: src.Data(from, to)}
	}
	return c.d
}

// heatLifetime reports whether the heatmap panels show lifetime totals: when
// the window saw no observations, unless c chose explicitly.
func (m *Model) heatLifetime(d store.HeatData) bool {
	if m.heatSet {
		return m.heatSinceStart
	}
	if d.Count > 0 {
		return false
	}
	for _, inc := range d.Inc {
		for _, count := range inc {
			if count > 0 {
				return false
			}
		}
	}
	return true
}

// heatScope reports whether the selected heat source is a gauge histogram
// (a current distribution with no scope) and, if not, whether its panels show
// lifetime totals (heatLifetime).
func (m *Model) heatScope() (gauge, lifetime bool) {
	srcs := m.st.HeatSources(m.heatBySeries)
	if len(srcs) == 0 {
		return false, m.heatSet && m.heatSinceStart
	}
	idx := clamp(m.heatIdx, 0, len(srcs)-1)
	if srcs[idx].IsGauge() {
		return true, false
	}
	from, to := m.st.Window(m.window)
	return false, m.heatLifetime(m.heatData(srcs[idx], idx, from, to))
}

// toggleHeatScope is the c key: switch counter panels between window and since start.
func (m *Model) toggleHeatScope() {
	if gauge, life := m.heatScope(); !gauge {
		m.heatSet, m.heatSinceStart = true, !life
	}
}

// heatBuckets is what the panels list per bucket: the window's increases
// summed, or a gauge's latest snapshot (summing snapshots would count one
// distribution once per scrape). Data takes a gauge's sum and count from that
// same latest scrape.
func heatBuckets(d store.HeatData) []float64 {
	tb := make([]float64, len(d.LE))
	if d.Gauge {
		if n := len(d.Inc); n > 0 {
			copy(tb, d.Inc[n-1])
		}
		return tb
	}
	for _, inc := range d.Inc {
		for k, v := range inc {
			tb[k] += v
		}
	}
	return tb
}

// perBucket turns cumulative bucket counts into per-bucket counts.
func perBucket(cum []float64) []float64 {
	out := make([]float64, len(cum))
	below := 0.0
	for k, c := range cum {
		out[k] = math.Max(0, c-below)
		below = math.Max(below, c)
	}
	return out
}

func bucketLabel(le float64, u format.Unit) string {
	if math.IsInf(le, 1) {
		return "+Inf"
	}
	return "≤" + format.Num(le, u)
}

// heatColumns folds each scrape's bucket increments into its time column.
func heatColumns(d store.HeatData, times []time.Time, latest time.Time, window time.Duration, ncols int) [][]float64 {
	cols := make([][]float64, ncols)
	for c := range cols {
		cols[c] = make([]float64, len(d.LE))
	}
	for i := 0; i < min(len(d.Inc), len(times)); i++ {
		c := format.TimeColumn(times[i], latest, window, ncols)
		if c < 0 {
			continue
		}
		if d.Gauge {
			if d.Inc[i] != nil {
				copy(cols[c], d.Inc[i])
			}
			continue
		}
		for k, v := range d.Inc[i] {
			cols[c][k] += v
		}
	}
	return cols
}

func cumulative(v []float64) []float64 {
	out := make([]float64, len(v))
	sum := 0.0
	for i, x := range v {
		sum += x
		out[i] = sum
	}
	return out
}
