package ui

import (
	"math"

	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/stats"
	"github.com/krisiasty/promtop/internal/store"
)

// histView is what a histogram row shows: the observations of the window, or
// of the target's lifetime ("since start") when the window saw none. A gauge
// histogram shows its current distribution.
type histView struct {
	cum        []float64 // cumulative bucket counts, aligned with Hist.LE
	sum, count float64
	lifetime   bool
}

// histView picks the window's observations, falling back to lifetime totals.
func (m *Model) histView(h *store.Histogram) histView {
	from, to := m.st.Window(m.window)
	if from == 0 {
		return histView{sum: math.NaN(), count: math.NaN()}
	}
	if cum, sum, count := h.Window(from, to); h.IsGauge() || count > 0 || (len(cum) > 0 && cum[len(cum)-1] > 0) {
		return histView{cum: cum, sum: sum, count: count}
	}
	cum, sum, count := h.At(to)
	return histView{cum: cum, sum: sum, count: count, lifetime: true}
}

// histStats summarizes a histogram row. Cur is the mean observation of the
// window (or lifetime), Prev the same one scrape earlier; Min and Max are the
// bounds of the lowest and highest buckets with observations; Mean is the
// lifetime mean, or a gauge's average snapshot mean over the window (as Graph
// shows it); quantiles come from the buckets like histogram_quantile.
func (m *Model) histStats(s *store.Series) (stats.Stats, bool) {
	nan := math.NaN()
	x := stats.Stats{Cur: nan, Prev: nan, Delta: nan, Min: nan, Max: nan, Mean: nan, Median: nan,
		Stddev: nan, P50: nan, P90: nan, P95: nan, P99: nan}
	h := s.Hist
	v := m.histView(h)
	from, to := m.st.Window(m.window)
	if from == 0 {
		return x, false
	}
	x.Cur = mean(v.sum, v.count)
	if v.lifetime {
		_, ps, pc := h.At(to - 1)
		x.Prev = mean(ps, pc)
	} else if to > 1 {
		_, ps, pc := h.Window(max(1, from-1), to-1)
		x.Prev = mean(ps, pc)
	}
	x.Delta = x.Cur - x.Prev
	if h.IsGauge() {
		x.Mean = stats.Summarize(s.Values(from, to)).Mean
	} else {
		_, _, ls, lc := h.Totals()
		x.Mean = mean(ls, lc)
	}
	if v.count > 0 {
		x.N = int(v.count)
	}
	if len(v.cum) == len(h.LE) {
		x.Min, x.Max = observedRange(h.LE, v.cum)
		x.P50 = stats.HistogramQuantile(0.5, h.LE, v.cum)
		x.P90 = stats.HistogramQuantile(0.9, h.LE, v.cum)
		x.P95 = stats.HistogramQuantile(0.95, h.LE, v.cum)
		x.P99 = stats.HistogramQuantile(0.99, h.LE, v.cum)
		x.Median = x.P50
	}
	return x, v.lifetime
}

// observedRange is the lower bound of the lowest and the upper bound of the
// highest bucket holding observations (NaN when there are none).
func observedRange(le, cum []float64) (lo, hi float64) {
	lo, hi = math.NaN(), math.NaN()
	below := 0.0
	for k, c := range cum[:min(len(cum), len(le))] {
		if c > below {
			if math.IsNaN(lo) {
				lo = 0
				if k > 0 {
					lo = le[k-1] //nolint:gosec // 0 < k < min(len(cum), len(le))
				}
			}
			hi = le[k]
		}
		below = math.Max(below, c)
	}
	return lo, hi
}

func mean(sum, count float64) float64 {
	if !(count > 0) {
		return math.NaN()
	}
	return sum / count
}

// histRates is a histogram row's trend: observations per second per scrape.
func (m *Model) histRates(h *store.Histogram) []float64 {
	from, to := m.st.Window(m.window)
	if from == 0 {
		return nil
	}
	f := max(1, from-1)
	r := h.CountRates(f, to, m.st.Times(f, to))
	if f < from {
		r = r[1:]
	}
	return r
}

// statMax formats a Max statistic; a histogram whose highest observations are
// in the +Inf bucket shows ">" and the top finite bound instead of "+Inf".
func (m *Model) statMax(s *store.Series, v float64, u format.Unit) string {
	if h := s.Hist; h != nil && math.IsInf(v, 1) && len(h.LE) > 1 {
		return ">" + format.Num(h.LE[len(h.LE)-2], u)
	}
	return m.num(v, u)
}
