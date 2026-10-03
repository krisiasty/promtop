package ui

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/krisiasty/promtop/internal/expo"
	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/stats"
	"github.com/krisiasty/promtop/internal/store"
)

// rowsKey is everything rows depends on; sort keys come from stats, which
// change with the scrape, window and rate mode.
type rowsKey struct {
	seq, pinGen      uint64
	filter           string
	onlyPinned, rate bool
	sort             SortKey
	window           time.Duration
}

type rowsCache struct {
	key  rowsKey
	rows []*store.Series // nil until built
}

// rows is the Table list (also navigated by History and Graph): filtered by
// case-insensitive substring of the series ID, pinned-only when toggled, and
// sorted (value sorts are disabled in high-cardinality mode). It is cached:
// key handling and rendering ask for it several times per event.
func (m *Model) rows() []*store.Series {
	k := rowsKey{seq: m.st.Seq(), pinGen: m.st.PinGen(), filter: m.filter, onlyPinned: m.onlyPinned,
		rate: m.showRate, sort: m.effectiveSort(), window: m.window}
	if m.rowCache.rows == nil || m.rowCache.key != k {
		m.rowCache = rowsCache{key: k, rows: m.buildRows(k.sort)}
	}
	return m.rowCache.rows
}

func (m *Model) buildRows(sort SortKey) []*store.Series {
	all := m.st.Series()
	f := strings.ToLower(m.filter)
	out := make([]*store.Series, 0, len(all))
	for _, s := range all {
		if f != "" && !strings.Contains(strings.ToLower(s.ID), f) {
			continue
		}
		if m.onlyPinned && !m.st.Pinned(s.Key) {
			continue
		}
		out = append(out, s)
	}
	var key func(stats.Stats) float64
	switch sort {
	case SortCurrent:
		key = func(x stats.Stats) float64 {
			if math.IsNaN(x.Cur) { // an exposed NaN keeps its row in place: sort by the latest number
				return x.Prev
			}
			return x.Cur
		}
	case SortDeltaPct:
		key = func(x stats.Stats) float64 {
			p := x.Prev
			if p == 0 || math.IsNaN(p) {
				p = 1
			}
			return math.Abs(x.Delta / p)
		}
	case SortP99:
		key = func(x stats.Stats) float64 { return x.P99 }
	default:
		return out // "name" keeps exposition order, as in the design
	}
	keys := make(map[*store.Series]float64, len(out))
	for _, s := range out {
		keys[s] = key(m.stats(s))
	}
	slices.SortStableFunc(out, func(a, b *store.Series) int { return descNaNLast(keys[a], keys[b]) })
	return out
}

func descNaNLast(a, b float64) int {
	switch an, bn := math.IsNaN(a), math.IsNaN(b); {
	case an && bn:
		return 0
	case an:
		return 1
	case bn:
		return -1
	}
	return cmp.Compare(b, a)
}

func (m *Model) effectiveSort() SortKey {
	if m.st.HighCard() {
		return SortName
	}
	return m.sort
}

// selected returns the selected series and its index: the series last
// selected by canonical key wherever it now sorts, else (it is gone, or
// nothing was selected yet) the row at the clamped cursor.
func (m *Model) selected(rows []*store.Series) (*store.Series, int) {
	if len(rows) == 0 {
		return nil, -1
	}
	if m.selID != "" {
		for i, s := range rows {
			if s.Key == m.selID {
				return s, i
			}
		}
	}
	i := clamp(m.cursor, 0, len(rows)-1)
	return rows[i], i
}

// setCursor moves the cursor to row i (clamped) and selects that series.
func (m *Model) setCursor(rows []*store.Series, i int) {
	oldID := m.selID
	m.cursor, m.selID = clamp(i, 0, len(rows)-1), ""
	if len(rows) > 0 {
		m.selID = rows[m.cursor].Key
	}
	if oldID != "" && m.selID != oldID {
		m.histOff = 0
	}
}

// values returns what views display for s over the current window: per-second
// rates for counters in rate mode, raw samples otherwise, with timestamps.
func (m *Model) values(s *store.Series) ([]float64, []time.Time) {
	from, to := m.st.Window(m.window)
	if from == 0 {
		return nil, nil
	}
	if s.Type == expo.Counter && m.showRate {
		f := from
		if f > 1 {
			f--
		}
		raw, ts := s.Values(f, to), m.st.Times(f, to)
		r := stats.Rates(raw, ts)
		if f < from {
			r, ts = r[1:], ts[1:]
		}
		return r, ts
	}
	return s.Values(from, to), m.st.Times(from, to)
}

// unit is the display unit: counter rates are unit-less except bytes.
func (m *Model) unit(s *store.Series) format.Unit {
	if s.Type == expo.Counter && m.showRate && s.Unit != format.Bytes {
		return format.None
	}
	return s.Unit
}

// stats summarizes s over the window, cached until the next scrape or a
// window/rate change. Only series that are rendered or sorted are computed.
func (m *Model) stats(s *store.Series) stats.Stats {
	x, _ := m.statsLife(s)
	return x
}

// plotStats summarizes the per-scrape values Graph and History plot: a
// histogram row's means (an idle interval keeps the last mean), else stats.
func (m *Model) plotStats(s *store.Series, vals []float64) stats.Stats {
	if s.Hist != nil {
		return stats.Summarize(vals)
	}
	return m.stats(s)
}

// statsLife is stats plus whether a histogram row fell back to lifetime values.
func (m *Model) statsLife(s *store.Series) (stats.Stats, bool) {
	c := &m.cache
	if c.m == nil || c.seq != m.st.Seq() || c.window != m.window || c.rate != m.showRate {
		*c = statsCache{seq: m.st.Seq(), window: m.window, rate: m.showRate, m: map[*store.Series]stats.Stats{},
			life: map[*store.Series]bool{}}
	}
	if x, ok := c.m[s]; ok {
		return x, c.life[s]
	}
	var x stats.Stats
	var life bool
	if s.Hist != nil {
		x, life = m.histStats(s)
	} else {
		vals, _ := m.values(s)
		x = stats.Summarize(vals)
		if s.ExposedNaN(m.st.Seq()) {
			x = x.LatestNaN()
		}
	}
	c.m[s], c.life[s] = x, life
	return x, life
}

func windowLabel(d time.Duration) string { return format.Dur(d) }

// clampOffset keeps the cursor centred in a vis-row viewport over n rows.
func clampOffset(cur, vis, n int) int { return max(0, min(cur-vis/2, n-vis)) }

// typeWord is the series kind as History/Graph titles show it.
func (m *Model) typeWord(s *store.Series) string {
	if s.Type == expo.Counter && m.showRate {
		return "rate/s"
	}
	return s.Type.String()
}

// num formats a value for display; Timestamp values render as a local time
// relative to the model's clock.
func (m *Model) num(v float64, u format.Unit) string { return format.Value(v, u, m.now()) }

// signed formats a delta for display; Timestamp deltas render as durations.
func (m *Model) signed(d float64, u format.Unit) string { return format.Delta(d, u) }

// fullTime is the "date time · age" text shown next to timestamp series, or
// "" for other series.
func (m *Model) fullTime(s *store.Series) string {
	if v := s.Last(); s.Unit == format.Timestamp && format.IsEpoch(v) {
		return format.TimeFull(v, m.now())
	}
	return ""
}
