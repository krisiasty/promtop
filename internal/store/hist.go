package store

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/krisiasty/promtop/internal/expo"
	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/stats"
)

// Histogram is one histogram label set (every label except le).
type Histogram struct {
	Family   string
	Type     expo.MetricType
	Labels   []expo.Label
	Unit     format.Unit
	LE       []float64 // ascending upper bounds, last is +Inf
	cum      *Ring[[]float64]
	sum      *Ring[float64]
	count    *Ring[float64]
	ts       *Ring[time.Time] // the store's scrape times
	lastSeen uint64
}

func (s *Store) applyHistogram(f *expo.Family, fi *FamilyInfo, seq uint64) {
	type group struct {
		h       *Histogram
		buckets map[float64]float64
	}
	groups := map[string]*group{}
	var keys []string
	for i := range f.Samples {
		smp := &f.Samples[i]
		s.lastVals[expo.Key(smp.Name, smp.Labels)] = smp.Value
		fi.addLabels(smp.Labels)
		labels, le, hasLE := splitLE(smp.Labels)
		key := expo.Key(f.Name, labels)
		g := groups[key]
		if g == nil {
			h := s.hists[key]
			if h == nil || h.Type != f.Type {
				c := s.histCap()
				h = &Histogram{Family: f.Name, Type: f.Type, Unit: fi.Unit, cum: NewRing[[]float64](c, nil),
					sum: NewRing(c, math.NaN()), count: NewRing(c, math.NaN()), ts: s.ts}
				s.hists[key] = h
			}
			h.Labels, h.Unit = labels, fi.Unit
			g = &group{h: h, buckets: map[float64]float64{}}
			groups[key] = g
			keys = append(keys, key)
		}
		switch {
		case strings.HasSuffix(smp.Name, "_bucket") && hasLE:
			g.buckets[le] = smp.Value
		case strings.HasSuffix(smp.Name, "_sum") || strings.HasSuffix(smp.Name, "_gsum"):
			g.h.sum.Put(seq, smp.Value)
		case strings.HasSuffix(smp.Name, "_count") || strings.HasSuffix(smp.Name, "_gcount"):
			g.h.count.Put(seq, smp.Value)
		}
	}
	for _, k := range keys {
		g := groups[k]
		les := make([]float64, 0, len(g.buckets))
		for le := range g.buckets {
			les = append(les, le)
		}
		slices.Sort(les)
		if !slices.Equal(les, g.h.LE) { // bucket layout changed: restart its history
			g.h.LE = les
			g.h.cum = NewRing[[]float64](s.histCap(), nil)
		}
		vec := make([]float64, len(les))
		for i, le := range les {
			vec[i] = g.buckets[le]
		}
		g.h.cum.Put(seq, vec)
		g.h.lastSeen = seq
		fi.Hists = append(fi.Hists, g.h)
		s.histogramRow(k, fi, g.h, seq)
	}
}

// histogramRow keeps the Table row for one histogram label set: its value per
// scrape is the mean of new observations, or the current gauge distribution.
func (s *Store) histogramRow(id string, fi *FamilyInfo, h *Histogram, seq uint64) {
	sr := s.seriesFor(id, fi.Name, h.Type, fi.Unit, h, fi)
	if sr.ID == "" || !slices.Equal(sr.Labels, h.Labels) {
		sr.ID, sr.Labels = expo.ID(fi.Name, h.Labels), h.Labels
	}
	mean := math.NaN()
	if _, ds, dc := h.sample(seq); dc > 0 {
		mean = ds / dc
	}
	prev := sr.Last()
	sr.vals.Put(seq, mean)
	sr.lastSeen = seq
	if !math.IsNaN(mean) && mean != prev {
		sr.LastChange = s.ts.Get(seq)
	}
	s.order = append(s.order, sr)
}

// Totals are the histogram's latest cumulative bucket counts, sum and count.
func (h *Histogram) Totals() (le, cum []float64, sum, count float64) {
	seq, cum := h.cum.Last()
	sum, count = h.sum.Get(seq), h.count.Get(seq)
	if seq == 0 {
		return h.LE, nil, math.NaN(), math.NaN()
	}
	return h.LE, cum, sum, count
}

// IsGauge reports whether the buckets are current distributions (gauge
// histogram snapshots) rather than counters of observations.
func (h *Histogram) IsGauge() bool { return h.Type == expo.GaugeHistogram }

// Window is what the histogram shows for scrapes from..to: a gauge's snapshot
// at to, or a counter's Increase over the window.
func (h *Histogram) Window(from, to uint64) (cum []float64, sum, count float64) {
	if h.IsGauge() {
		return h.At(to)
	}
	return h.Increase(from, to)
}

// sample is one scrape's contribution: a gauge's snapshot, or a counter's
// increase since the previous scrape.
func (h *Histogram) sample(seq uint64) (cum []float64, sum, count float64) {
	if h.IsGauge() {
		return h.At(seq)
	}
	return h.step(seq)
}

// At is the histogram's cumulative state at scrape seq (nil/NaN if unknown).
func (h *Histogram) At(seq uint64) (cum []float64, sum, count float64) {
	return h.cum.Get(seq), h.sum.Get(seq), h.count.Get(seq)
}

// Increase is what was observed in scrapes from..to: per-bucket cumulative
// increases (nil if no complete interval is known), and the increases of sum
// and count from those same intervals, with counter reset semantics. An omitted
// optional sum or count is NaN rather than a measured zero.
func (h *Histogram) Increase(from, to uint64) (cum []float64, sum, count float64) {
	for s := from; s <= to; s++ {
		step, ds, dc := h.step(s)
		if step == nil {
			continue
		}
		if cum == nil {
			cum = make([]float64, len(step))
		}
		for b, v := range step {
			cum[b] += v
		}
		sum += ds
		count += dc
	}
	return cum, sum, count
}

// Seconds is the time spanned by the intervals Increase(from, to) includes.
func (h *Histogram) Seconds(from, to uint64) float64 {
	secs := 0.0
	for s := from; s <= to; s++ {
		if cum, _, _ := h.step(s); cum != nil {
			secs += h.interval(s)
		}
	}
	return secs
}

// interval is the time between scrapes seq-1 and seq (0 if either is unknown).
func (h *Histogram) interval(seq uint64) float64 {
	if h.ts == nil {
		return 0
	}
	cur, prev := h.ts.Get(seq), h.ts.Get(seq-1)
	if cur.IsZero() || prev.IsZero() {
		return 0
	}
	return cur.Sub(prev).Seconds()
}

// CountRates returns per-second observation rates, detecting resets from the
// whole histogram before calculating each count increase. Gauge rates are NaN.
func (h *Histogram) CountRates(from, to uint64, times []time.Time) []float64 {
	rates, _ := h.counterRates(-1, from, to, times)
	return rates
}

func splitLE(ls []expo.Label) ([]expo.Label, float64, bool) {
	out := make([]expo.Label, 0, len(ls))
	le, ok := 0.0, false
	for _, l := range ls {
		if l.Name == "le" {
			if v, err := strconv.ParseFloat(l.Value, 64); err == nil {
				le, ok = v, true
			}
			continue
		}
		out = append(out, l)
	}
	return out, le, ok
}

// step returns the cumulative bucket, sum and count increases for one scrape
// interval. Buckets must have the same pair of samples so a missing histogram
// cannot contribute a mean without corresponding buckets. OpenMetrics makes
// _sum and _count optional: either is unavailable (NaN) when absent at either end.
func (h *Histogram) step(seq uint64) (cum []float64, sum, count float64) {
	if seq < 2 || h.IsGauge() {
		return nil, 0, 0
	}
	cur, prev := h.cum.Get(seq), h.cum.Get(seq-1)
	if len(cur) == 0 || len(cur) != len(prev) || len(cur) != len(h.LE) {
		return nil, 0, 0
	}
	reset := h.resetBetween(seq-1, seq)
	sum = pairDelta(h.sum, seq, reset)
	count = pairDelta(h.count, seq, reset)
	cum = make([]float64, len(cur))
	for b := range cur {
		if reset {
			cum[b] = cur[b]
		} else {
			cum[b] = stats.Increase(prev[b], cur[b])
		}
	}
	return cum, sum, count
}

// resetBetween detects a restart from any decreasing counter component. The
// observation sum is not reset evidence because negative observations can lower it.
func (h *Histogram) resetBetween(prevSeq, seq uint64) bool {
	if h.IsGauge() {
		return false
	}
	if h.count.Get(seq) < h.count.Get(prevSeq) {
		return true
	}
	cur, prev := h.cum.Get(seq), h.cum.Get(prevSeq)
	if len(cur) != len(prev) {
		return false
	}
	for b := range cur {
		if cur[b] < prev[b] {
			return true
		}
	}
	return false
}

// pairDelta is r's signed change from seq-1 to seq, or its current value after
// a histogram reset: NaN when either sample is unavailable.
func pairDelta(r *Ring[float64], seq uint64, reset bool) float64 {
	cur, prev := r.Get(seq), r.Get(seq-1)
	if math.IsNaN(cur) || math.IsNaN(prev) {
		return math.NaN()
	}
	if reset {
		return cur
	}
	return cur - prev
}

// HeatSource is what the heatmap renders: one histogram label set, or a whole
// family summed by le (Labels nil).
type HeatSource struct {
	Family string
	Type   expo.MetricType
	Labels []expo.Label
	Unit   format.Unit
	Hists  []*Histogram
}

// IsGauge reports whether the source's buckets are current distributions.
func (src HeatSource) IsGauge() bool { return src.Type == expo.GaugeHistogram }

// HeatSources lists histogram families (bySeries=false) or label sets.
func (s *Store) HeatSources(bySeries bool) []HeatSource {
	var out []HeatSource
	for _, fi := range s.families {
		if len(fi.Hists) == 0 {
			continue
		}
		if !bySeries {
			out = append(out, HeatSource{Family: fi.Name, Type: fi.Type, Unit: fi.Unit, Hists: fi.Hists})
			continue
		}
		for _, h := range fi.Hists {
			out = append(out, HeatSource{Family: fi.Name, Type: fi.Type, Labels: h.Labels, Unit: fi.Unit, Hists: []*Histogram{h}})
		}
	}
	return out
}

// HeatData is a heat source's activity over a window.
type HeatData struct {
	Gauge bool // bucket columns are current distributions, not increases
	LE    []float64
	Inc   [][]float64 // per scrape, per bucket (non-cumulative); nil = unknown
	Sum   float64     // sum increase, or latest gauge sum (NaN if omitted)
	Count float64     // count increase, or latest gauge count (NaN if omitted)
	Rate  float64     // observations per second: each histogram's _count rate over its included intervals, summed (NaN if none)
}

// Data sums histograms sharing the first layout: counter increases or gauge
// snapshots per scrape. Gauge sum/count totals come only from the latest scrape.
func (src HeatSource) Data(from, to uint64) HeatData {
	d := HeatData{Gauge: src.IsGauge(), Rate: math.NaN()}
	if from == 0 {
		return d
	}
	for _, h := range src.Hists {
		if len(h.LE) == 0 {
			continue
		}
		if d.LE == nil {
			d.LE, d.Inc = h.LE, make([][]float64, to-from+1)
		}
		if !slices.Equal(h.LE, d.LE) {
			continue
		}
		count, secs := 0.0, 0.0
		for i := range d.Inc {
			seq := from + uint64(i)
			cum, ds, dc := h.sample(seq)
			if cum == nil || len(cum) != len(d.LE) {
				continue
			}
			if d.Inc[i] == nil {
				d.Inc[i] = make([]float64, len(d.LE))
			}
			for k, v := range cum {
				if k > 0 {
					v = math.Max(0, v-cum[k-1])
				}
				d.Inc[i][k] += v
			}
			if !d.Gauge || seq == to {
				d.Sum += ds
				d.Count += dc
			}
			if !d.Gauge && !math.IsNaN(dc) {
				count += dc
				secs += h.interval(seq)
			}
		}
		if secs > 0 { // a label set's rate counts only the time its increases span
			if math.IsNaN(d.Rate) {
				d.Rate = 0
			}
			d.Rate += count / secs
		}
	}
	return d
}

// Totals sums the latest cumulative buckets, sum and count of the histograms
// that share the first histogram's bucket layout (nil when none is known).
func (src HeatSource) Totals() (le, cum []float64, sum, count float64) {
	for _, h := range src.Hists {
		l, c, s, n := h.Totals()
		if c == nil || len(c) != len(l) {
			continue
		}
		if le == nil {
			le, cum = l, make([]float64, len(l))
		}
		if !slices.Equal(l, le) {
			continue
		}
		for k, v := range c {
			cum[k] += v
		}
		sum += s
		count += n
	}
	return le, cum, sum, count
}

// Bucket returns bucket k's cumulative value per scrape in from..to, summed
// over histograms sharing the first layout (NaN where unknown).
func (src HeatSource) Bucket(k int, from, to uint64) []float64 {
	if from == 0 || len(src.Hists) == 0 {
		return nil
	}
	le := src.Hists[0].LE
	out := make([]float64, to-from+1)
	for i := range out {
		out[i] = math.NaN()
	}
	for _, h := range src.Hists {
		if !slices.Equal(h.LE, le) || k >= len(le) {
			continue
		}
		for i := range out {
			if v := h.cum.Get(from + uint64(i)); v != nil {
				if math.IsNaN(out[i]) {
					out[i] = 0
				}
				out[i] += v[k]
			}
		}
	}
	return out
}

// BucketRates returns the sum of per-second rates for bucket k at each
// scrape. Each histogram's reset is handled before aggregation, so a label
// set disappearing cannot look like a reset of the whole family.
func (src HeatSource) BucketRates(k int, from, to uint64, times []time.Time) []float64 {
	if from == 0 || len(src.Hists) == 0 {
		return nil
	}
	le := src.Hists[0].LE
	out := make([]float64, to-from+1)
	for i := range out {
		out[i] = math.NaN()
	}
	for _, h := range src.Hists {
		if !slices.Equal(h.LE, le) || k >= len(le) {
			continue
		}
		rates, _ := h.counterRates(k, from, to, times)
		for i, v := range rates {
			if math.IsNaN(v) {
				continue
			}
			if math.IsNaN(out[i]) {
				out[i] = 0
			}
			out[i] += v
		}
	}
	return out
}

// BucketRate1m sums each histogram's one-minute bucket rate. A family rate
// is available when at least one label set has two samples in that minute.
func (src HeatSource) BucketRate1m(k int, from, to uint64, times []time.Time) float64 {
	if from == 0 || len(src.Hists) == 0 {
		return math.NaN()
	}
	le := src.Hists[0].LE
	total, known := 0.0, false
	for _, h := range src.Hists {
		if !slices.Equal(h.LE, le) || k >= len(le) {
			continue
		}
		if _, rate := h.counterRates(k, from, to, times); !math.IsNaN(rate) {
			total += rate
			known = true
		}
	}
	if !known {
		return math.NaN()
	}
	return total
}

// counterRates returns per-scrape and one-minute rates for bucket k (k=-1
// selects _count). Missing samples remain NaN; rates still span gaps using the
// previous known histogram state. Each increase uses whole-histogram resets.
func (h *Histogram) counterRates(k int, from, to uint64, times []time.Time) ([]float64, float64) {
	out := make([]float64, to-from+1)
	for i := range out {
		out[i] = math.NaN()
	}
	n := min(len(out), len(times))
	if n == 0 || h.IsGauge() {
		return out, math.NaN()
	}
	cut := times[n-1].Add(-time.Minute) //nolint:gosec // 0 < n <= len(times)
	prev, first, last := -1, -1, -1
	prevValue, minuteIncrease := 0.0, 0.0
	for i := range n {
		seq := from + uint64(i)
		v := math.NaN()
		if k < 0 {
			v = h.count.Get(seq)
		} else if cum := h.cum.Get(seq); k < len(cum) {
			v = cum[k]
		}
		if math.IsNaN(v) {
			continue
		}
		if !times[i].Before(cut) { //nolint:gosec // i < n <= len(times)
			if first < 0 {
				first = i
			}
			last = i
		}
		if prev >= 0 {
			inc := stats.Increase(prevValue, v)
			if h.resetBetween(from+uint64(prev), seq) {
				inc = v
			}
			if dt := times[i].Sub(times[prev]).Seconds(); dt > 0 { //nolint:gosec // 0 <= prev < i < n <= len(times)
				out[i] = inc / dt
			}
			if !times[prev].Before(cut) && !times[i].Before(cut) { //nolint:gosec // 0 <= prev < i < n <= len(times)
				minuteIncrease += inc
			}
		}
		prev, prevValue = i, v
	}
	if first < 0 || first == last {
		return out, math.NaN()
	}
	return out, minuteIncrease / times[last].Sub(times[first]).Seconds() //nolint:gosec // 0 <= first < last < n <= len(times)
}
