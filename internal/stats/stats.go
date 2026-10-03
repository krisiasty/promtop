// Package stats computes window statistics, counter rates and histogram
// quantiles the way the promtop design (and PromQL) define them.
package stats

import (
	"math"
	"slices"
	"time"
)

// Stats summarizes one series over a window.
type Stats struct {
	Cur, Prev, Delta               float64
	Min, Max, Mean, Median, Stddev float64
	P50, P90, P95, P99             float64
	N                              int
}

// Increase is the counter increase from prev to cur with Prometheus reset
// semantics: a drop means the counter restarted from zero, so the increase is cur.
func Increase(prev, cur float64) float64 {
	if cur < prev {
		return cur
	}
	return cur - prev
}

// Rates converts counter samples to per-second rates. out[i] is the rate
// between vals[i] and the previous non-NaN sample, divided by the real time
// between them; NaN where undefined.
func Rates(vals []float64, ts []time.Time) []float64 {
	out := make([]float64, len(vals))
	for i := range out {
		out[i] = math.NaN()
	}
	prev := -1
	for i, v := range vals[:min(len(vals), len(ts))] {
		if math.IsNaN(v) {
			continue
		}
		if prev >= 0 {
			if dt := ts[i].Sub(ts[prev]).Seconds(); dt > 0 {
				out[i] = Increase(vals[prev], v) / dt //nolint:gosec // prev < i < min(len(vals), len(ts))
			}
		}
		prev = i
	}
	return out
}

// LatestNaN is st for a window whose latest sample is an explicit NaN: the
// current value and its delta are unavailable, the latest numeric sample is
// the previous value, and the aggregates (which ignore NaN) are unchanged.
// Summarize cannot decide this itself: NaN in vals also marks absent samples,
// idle histogram intervals and undefined rates, which keep the last value.
func (st Stats) LatestNaN() Stats {
	st.Prev, st.Cur, st.Delta = st.Cur, math.NaN(), math.NaN()
	return st
}

// Summarize computes window statistics over chronological vals, ignoring NaN.
// Percentiles interpolate linearly between closest ranks; stddev is the
// population standard deviation.
func Summarize(vals []float64) Stats {
	nan := math.NaN()
	st := Stats{Cur: nan, Prev: nan, Delta: nan, Min: nan, Max: nan, Mean: nan, Median: nan,
		Stddev: nan, P50: nan, P90: nan, P95: nan, P99: nan}
	clean := make([]float64, 0, len(vals))
	for _, v := range vals {
		if !math.IsNaN(v) {
			clean = append(clean, v)
		}
	}
	n := len(clean)
	st.N = n
	if n == 0 {
		return st
	}
	st.Cur = clean[n-1]
	if n > 1 {
		st.Prev = clean[n-2]
		st.Delta = st.Cur - st.Prev
	}
	sum := 0.0
	for _, v := range clean {
		sum += v
	}
	st.Mean = sum / float64(n)
	ss := 0.0
	for _, v := range clean {
		ss += (v - st.Mean) * (v - st.Mean)
	}
	st.Stddev = math.Sqrt(ss / float64(n))
	slices.Sort(clean)
	st.Min, st.Max = clean[0], clean[n-1]
	st.P50 = Percentile(clean, 0.5)
	st.Median = st.P50
	st.P90 = Percentile(clean, 0.9)
	st.P95 = Percentile(clean, 0.95)
	st.P99 = Percentile(clean, 0.99)
	return st
}

// Percentile interpolates linearly between the closest ranks of sorted.
func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	i := float64(len(sorted)-1) * p
	lo, hi := int(math.Floor(i)), int(math.Ceil(i))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(i-float64(lo))
}

// Rate1m is the per-second increase over the samples in the last 60 seconds
// (relative to the newest timestamp). NaN with fewer than two samples.
func Rate1m(vals []float64, ts []time.Time) float64 {
	n := min(len(vals), len(ts))
	if n == 0 {
		return math.NaN()
	}
	cut := ts[n-1].Add(-time.Minute) //nolint:gosec // 0 < n <= len(ts)
	inc, first, prev := 0.0, -1, -1
	for i := range n {
		if ts[i].Before(cut) || math.IsNaN(vals[i]) { //nolint:gosec // i < n <= len(vals), len(ts)
			continue
		}
		if first < 0 {
			first = i
		}
		if prev >= 0 {
			inc += Increase(vals[prev], vals[i]) //nolint:gosec // prev < i < n <= len(vals)
		}
		prev = i
	}
	if first < 0 || prev == first {
		return math.NaN()
	}
	return inc / ts[prev].Sub(ts[first]).Seconds() //nolint:gosec // first, prev < n <= len(ts)
}

// HistogramQuantile estimates quantile q from cumulative bucket counts cum
// with upper bounds le (ascending, last +Inf), like PromQL histogram_quantile:
// linear interpolation inside the bucket; a rank in +Inf returns the previous
// bound; the first bucket's lower bound is 0.
func HistogramQuantile(q float64, le, cum []float64) float64 {
	n := len(cum)
	if n == 0 || len(le) != n || !(cum[n-1] > 0) {
		return math.NaN()
	}
	rank := q * cum[n-1]
	for i := 0; i < n; i++ {
		if cum[i] < rank {
			continue
		}
		if math.IsInf(le[i], 1) {
			if i == 0 {
				return math.NaN()
			}
			return le[i-1] //nolint:gosec // i > 0 here, i < len(le)
		}
		lo, below := 0.0, 0.0
		if i > 0 {
			lo, below = le[i-1], cum[i-1] //nolint:gosec // 0 < i < n = len(le) = len(cum)
		} else if le[0] <= 0 {
			return le[0]
		}
		cnt := cum[i] - below
		if cnt <= 0 {
			return le[i]
		}
		return lo + (le[i]-lo)*(rank-below)/cnt
	}
	return le[max(0, n-2)]
}

// Bin is one equal-width bucket of a value distribution.
type Bin struct {
	Lo, Hi float64
	Count  int
}

// Distribution splits vals (NaN and ±Inf ignored) into n equal-width bins
// over min..max; the maximum lands in the last bin. Nil when there is no data.
func Distribution(vals []float64, n int) []Bin {
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	lo, hi, c := math.Inf(1), math.Inf(-1), 0
	for _, v := range vals {
		if finite(v) {
			lo, hi, c = math.Min(lo, v), math.Max(hi, v), c+1
		}
	}
	if c == 0 || n <= 0 {
		return nil
	}
	bw := (hi - lo) / float64(n)
	if bw == 0 {
		bw = 1
	}
	bins := make([]Bin, n)
	for i := range bins {
		bins[i] = Bin{Lo: lo + float64(i)*bw, Hi: lo + float64(i+1)*bw}
	}
	for _, v := range vals {
		if !finite(v) {
			continue
		}
		f := math.Floor((v - lo) / bw)
		if math.IsNaN(f) { // hi−lo overflowed to +Inf
			f = 0
		}
		bins[int(math.Max(0, math.Min(float64(n-1), f)))].Count++
	}
	return bins
}
