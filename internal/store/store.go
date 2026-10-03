package store

import (
	"math"
	"slices"
	"strings"
	"time"

	"github.com/krisiasty/promtop/internal/expo"
	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/scrape"
)

const (
	// MaxWindow is the longest stats window; rings keep this much history.
	MaxWindow = 30 * time.Minute
	// HighCardinality is the count of Table series plus histogram samples in
	// a scrape above which promtop limits work.
	HighCardinality = 10000
	// HighCardCap is the ring size of non-pinned series and of histograms in
	// high-cardinality mode.
	HighCardCap = 120
	// HistoryBudgetBytes bounds the estimated storage for timestamp and value
	// rings, including histogram buckets.
	HistoryBudgetBytes = 512 << 20
	// ResetHold is how long "↺ reset" and the reset banner stay visible.
	ResetHold = 60 * time.Second
)

const (
	historySlotBytes      = 16 // conservative cost per sample slot, including ring overhead
	timestampSlotBytes    = 24 // time.Time in the shared timestamp ring
	historyCapGranularity = 128
)

// Series is one Table-eligible time series.
type Series struct {
	ID         string // name{labels} in exposition order
	Key        string // name{labels} in canonical order; stable across scrapes
	Name       string
	Family     string
	Labels     []expo.Label
	Type       expo.MetricType // sample type, or the histogram type for a distribution
	Unit       format.Unit     // the sample's unit: the family's, except a summary _count (None)
	Help       string
	ResetAt    time.Time      // last counter reset seen
	LastChange time.Time      // last scrape whose value differed from the previous one
	Hist       *Histogram     // the histogram behind a Histogram row (nil otherwise)
	vals       *Ring[float64] // histogram interval means, or gauge histogram snapshot means
	lastSeen   uint64
	lastNum    float64 // latest non-NaN sample, for reset detection across exposed NaNs
}

// Values returns the samples for sequences from..to (NaN where absent).
func (s *Series) Values(from, to uint64) []float64 { return s.vals.Range(nil, from, to) }

// ExposedNaN reports whether scrape seq exposed the series' sample as NaN.
// An absent sample, or a histogram row's interval without observations, is
// also stored as NaN but is not an exposed value.
func (s *Series) ExposedNaN(seq uint64) bool {
	last, v := s.vals.Last()
	return s.Hist == nil && seq > 0 && last == seq && math.IsNaN(v)
}

// Last is the most recent value.
func (s *Series) Last() float64 { _, v := s.vals.Last(); return v }

// Capacity is how many scrapes of history the series keeps.
func (s *Series) Capacity() int { return s.vals.Cap() }

// FamilyInfo describes one metric family of the latest scrape.
type FamilyInfo struct {
	Name        string
	Type        expo.MetricType
	Help        string
	Unit        format.Unit
	Samples     int
	LabelNames  []string
	LabelValues map[string][]string
	Series      []*Series
	Hists       []*Histogram
	seen        map[string]map[string]bool
}

func (fi *FamilyInfo) addLabels(ls []expo.Label) {
	for _, l := range ls {
		vals := fi.seen[l.Name]
		if vals == nil {
			vals = map[string]bool{}
			fi.seen[l.Name] = vals
			fi.LabelNames = append(fi.LabelNames, l.Name)
		}
		if !vals[l.Value] {
			vals[l.Value] = true
			fi.LabelValues[l.Name] = append(fi.LabelValues[l.Name], l.Value)
		}
	}
}

// ResetState summarizes the counter resets and target restarts of the last
// ResetHold, as of the latest scrape.
type ResetState struct {
	At        time.Time // latest scrape with a reset or restart
	Count     int       // counters that reset within ResetHold
	Restarted bool      // process_start_time_seconds changed within ResetHold
	RestartAt time.Time // latest change of process_start_time_seconds
}

// Store is promtop's whole in-memory history.
type Store struct {
	Interval time.Duration
	Scrape   ScrapeState
	Reset    ResetState

	capacity           int
	historyCap         int
	historyBudgetBytes int64
	seq                uint64
	ts                 *Ring[time.Time]
	fails              *Ring[int] // failed attempts just before each successful scrape
	series             map[string]*Series
	order              []*Series
	hists              map[string]*Histogram
	families           []*FamilyInfo
	last               *expo.Parsed
	lastVals           map[string]float64
	prevVals           map[string]float64
	pinned             map[string]bool
	pinGen             uint64
	highCard           bool
	topFamily          string
	topCount           int
	startTime          float64
	winSeq             uint64
	winFrom            map[time.Duration]uint64 // Window starts for scrape winSeq
}

// New returns an empty store with the default history memory budget.
func New(interval time.Duration) *Store { return NewWithBudget(interval, HistoryBudgetBytes) }

// NewWithBudget returns an empty store with the given estimated ring memory
// budget. A nonpositive budget uses the default.
func NewWithBudget(interval time.Duration, budgetBytes int64) *Store {
	if budgetBytes <= 0 {
		budgetBytes = HistoryBudgetBytes
	}
	c := int(MaxWindow/interval) + 1
	return &Store{
		Interval:           interval,
		capacity:           c,
		historyCap:         c,
		historyBudgetBytes: budgetBytes,
		ts:                 NewRing(c, time.Time{}),
		fails:              NewRing(c, 0),
		series:             map[string]*Series{},
		hists:              map[string]*Histogram{},
		pinned:             map[string]bool{},
	}
}

// Apply records one successful scrape.
func (s *Store) Apply(r *scrape.Result) {
	s.seq++
	seq := s.seq
	s.fails.Put(seq, s.Scrape.Fails)
	s.Scrape.recordOK(r, s.Interval)
	s.ts.Put(seq, r.At)
	p := r.Parsed
	s.last = p
	s.prevVals, s.lastVals = s.lastVals, make(map[string]float64, p.Samples)
	s.order = make([]*Series, 0, len(s.order))
	s.families = make([]*FamilyInfo, 0, len(p.Families))
	s.setCapacityFor(p) // before any new ring is allocated
	resets, restarted := 0, false
	for _, f := range p.Families {
		fi := &FamilyInfo{Name: f.Name, Type: f.Type, Help: oneLine(f.Help), Unit: unitOf(f), Samples: len(f.Samples),
			LabelValues: map[string][]string{}, seen: map[string]map[string]bool{}}
		s.families = append(s.families, fi)
		if f.Type.IsHistogram() {
			s.applyHistogram(f, fi, seq)
			continue
		}
		for i := range f.Samples {
			smp := &f.Samples[i]
			key := expo.Key(smp.Name, smp.Labels)
			s.lastVals[key] = smp.Value
			fi.addLabels(smp.Labels)
			sr := s.seriesFor(key, smp.Name, seriesType(f, smp.Name), sampleUnit(f, fi.Unit, smp.Name), nil, fi)
			if sr.ID == "" || !slices.Equal(sr.Labels, smp.Labels) {
				sr.ID, sr.Labels = expo.ID(smp.Name, smp.Labels), smp.Labels
			}
			if sr.lastSeen == 0 || !sameValue(smp.Value, sr.Last()) {
				sr.LastChange = r.At
			}
			sr.vals.Put(seq, smp.Value)
			sr.lastSeen = seq
			if !math.IsNaN(smp.Value) {
				if sr.Type == expo.Counter && smp.Value < sr.lastNum {
					sr.ResetAt = r.At
					resets++
				}
				sr.lastNum = smp.Value
			}
			fi.Series = append(fi.Series, sr)
			s.order = append(s.order, sr)
			if smp.Name == "process_start_time_seconds" && len(smp.Labels) == 0 && !math.IsNaN(smp.Value) {
				restarted = s.startTime != 0 && smp.Value != s.startTime
				s.startTime = smp.Value
			}
		}
	}
	s.forget(seq)
	s.updateReset(r.At, resets > 0, restarted)
	s.updateHighCard()
}

// Fail records one failed scrape; data is untouched (no partial updates).
func (s *Store) Fail(e *scrape.Error) { s.Scrape.recordFail(e, s.Interval) }

// NextDelay is the wait before the next attempt.
func (s *Store) NextDelay() time.Duration { return s.Scrape.Delay(s.Interval) }

func (s *Store) Seq() uint64                       { return s.seq }
func (s *Store) Series() []*Series                 { return s.order }
func (s *Store) Families() []*FamilyInfo           { return s.families }
func (s *Store) Last() *expo.Parsed                { return s.last }
func (s *Store) HighCard() bool                    { return s.highCard }
func (s *Store) TopFamily() (string, int)          { return s.topFamily, s.topCount }
func (s *Store) LastTime() time.Time               { return s.ts.Get(s.seq) }
func (s *Store) Times(from, to uint64) []time.Time { return s.ts.Range(nil, from, to) }
func (s *Store) Failures(from, to uint64) []int    { return s.fails.Range(nil, from, to) }
func (s *Store) Pinned(key string) bool            { return s.pinned[key] }
func (s *Store) PinCount() int                     { return len(s.pinned) }

// PinGen changes whenever a pin is set or cleared.
func (s *Store) PinGen() uint64 { return s.pinGen }

// PrevValue is a sample's value in the scrape before the latest one, looked
// up by its canonical key.
func (s *Store) PrevValue(key string) (float64, bool) {
	v, ok := s.prevVals[key]
	return v, ok
}

// SetPinned pins or unpins a series by canonical key (pins outlive absent series).
func (s *Store) SetPinned(key string, on bool) {
	s.pinGen++
	if on {
		s.pinned[key] = true
	} else {
		delete(s.pinned, key)
	}
	if sr := s.series[key]; sr != nil {
		s.resizeRing(sr)
	}
}

// Window returns the sequences whose timestamps fall within d of the latest
// scrape (strictly after LastTime()−d).
func (s *Store) Window(d time.Duration) (from, to uint64) {
	if s.seq == 0 {
		return 0, 0
	}
	if s.winSeq != s.seq {
		s.winSeq, s.winFrom = s.seq, map[time.Duration]uint64{}
	}
	if from, ok := s.winFrom[d]; ok {
		return from, s.seq
	}
	cut := s.ts.Get(s.seq).Add(-d)
	from = s.seq
	for from > 1 {
		t := s.ts.Get(from - 1)
		if t.IsZero() || !t.After(cut) {
			break
		}
		from--
	}
	s.winFrom[d] = from
	return from, s.seq
}

// seriesFor returns key's series, starting fresh history when the key now
// holds another type or histogram, so no view mixes the two (pins are by key).
// A kept series takes the latest unit and help, which may change between scrapes.
func (s *Store) seriesFor(key, name string, typ expo.MetricType, unit format.Unit, h *Histogram, fi *FamilyInfo) *Series {
	if sr := s.series[key]; sr != nil && sr.Type == typ && sr.Hist == h {
		sr.Unit, sr.Help = unit, fi.Help
		return sr
	}
	if h == nil {
		delete(s.hists, key) // a histogram replaced by a plain series keeps no bucket history
	}
	sr := &Series{Key: key, Name: name, Family: fi.Name, Type: typ, Unit: unit, Help: fi.Help, Hist: h,
		vals: NewRing(s.ringCap(key), math.NaN()), lastNum: math.NaN()}
	s.series[key] = sr
	return sr
}

func (s *Store) ringCap(id string) int {
	if s.highCard && !s.pinned[id] {
		return min(s.historyCap, HighCardCap)
	}
	return s.historyCap
}

func (s *Store) histCap() int {
	if s.highCard {
		return min(s.historyCap, HighCardCap)
	}
	return s.historyCap
}

func (s *Store) resizeRing(sr *Series) {
	if c := s.ringCap(sr.Key); c != sr.vals.Cap() {
		sr.vals = sr.vals.Resize(c)
	}
}

func (s *Store) forget(seq uint64) {
	limit := uint64(s.capacity) //nolint:gosec // capacity is positive
	for id, sr := range s.series {
		if seq-sr.lastSeen >= limit {
			delete(s.series, id)
		}
	}
	for k, h := range s.hists {
		if seq-h.lastSeen >= limit {
			delete(s.hists, k)
		}
	}
}

// updateReset folds this scrape's resets into the ResetHold window: Count is
// the number of distinct series whose last reset is within the hold.
func (s *Store) updateReset(at time.Time, reset, restarted bool) {
	rs := &s.Reset
	if reset || restarted {
		rs.At = at
	}
	if restarted {
		rs.RestartAt = at
	}
	if rs.At.IsZero() || at.Sub(rs.At) >= ResetHold {
		return // nothing within the hold: the banner is off
	}
	rs.Count = 0
	for _, sr := range s.series {
		if !sr.ResetAt.IsZero() && at.Sub(sr.ResetAt) < ResetHold {
			rs.Count++
		}
	}
	rs.Restarted = !rs.RestartAt.IsZero() && at.Sub(rs.RestartAt) < ResetHold
}

// highCardinality decides the mode from the payload: Table series plus
// histogram samples (every histogram sample costs ring memory).
func highCardinality(p *expo.Parsed) bool {
	return p.Samples > HighCardinality // every non-histogram sample is a Table series
}

// budgetCap limits all existing and incoming history to a fixed ring memory
// budget. Counting both sets is conservative when a scrape reuses series, but
// also covers series churn that leaves old histories resident until forgotten.
func (s *Store) budgetCap(p *expo.Parsed) int {
	units := int64(len(s.series) + p.Samples)
	for _, h := range s.hists {
		units += int64(len(h.LE) + 3) // bucket vectors, cumulative ring, sum and count
	}
	for _, f := range p.Families {
		if f.Type.IsHistogram() {
			units += int64(len(f.Samples)) // new histogram groups also need row/ring overhead
		}
	}
	if units == 0 {
		return s.capacity
	}
	available := max(int64(0), s.historyBudgetBytes-int64(s.capacity)*timestampSlotBytes)
	cap := min(s.capacity, max(1, int(available/(units*historySlotBytes))))
	if cap < s.capacity && cap >= historyCapGranularity {
		cap = cap / historyCapGranularity * historyCapGranularity
	}
	return cap
}

// setCapacityFor applies the budget and high-cardinality mode together so
// existing rings are resized at most once before new rings are allocated.
func (s *Store) setCapacityFor(p *expo.Parsed) {
	hc, cap := highCardinality(p), s.budgetCap(p)
	if hc == s.highCard && cap == s.historyCap {
		return
	}
	s.highCard, s.historyCap = hc, cap
	for _, sr := range s.series {
		s.resizeRing(sr)
	}
	c := s.histCap()
	for _, h := range s.hists {
		if h.cum.Cap() != c {
			h.cum, h.sum, h.count = h.cum.Resize(c), h.sum.Resize(c), h.count.Resize(c)
		}
	}
}

// updateHighCard names the family with the most Table series (histogram
// families: samples) for the high-cardinality banner.
func (s *Store) updateHighCard() {
	s.topFamily, s.topCount = "", 0
	if s.highCard {
		for _, fi := range s.families {
			n := len(fi.Series)
			if fi.Type.IsHistogram() {
				n = fi.Samples
			}
			if n > s.topCount {
				s.topFamily, s.topCount = fi.Name, n
			}
		}
	}
}

func seriesType(f *expo.Family, sample string) expo.MetricType {
	switch f.Type {
	case expo.Summary:
		if sample == f.Name {
			return expo.Gauge // quantiles
		}
		return expo.Counter // _sum, _count
	case expo.Counter, expo.Gauge:
		return f.Type
	}
	return expo.Untyped
}

// sampleUnit is one sample's unit within a family whose unit is u: a summary's
// _count is a number of observations, which does not carry their unit.
func sampleUnit(f *expo.Family, u format.Unit, sample string) format.Unit {
	if f.Type == expo.Summary && sample == f.Name+"_count" {
		return format.None
	}
	return u
}

func unitOf(f *expo.Family) format.Unit {
	switch f.Unit {
	case "bytes":
		return format.Bytes
	case "seconds":
		return format.Seconds
	}
	n := strings.TrimSuffix(f.Name, "_total")
	switch {
	case n == f.Name && (strings.HasSuffix(n, "_timestamp_seconds") || strings.HasSuffix(n, "_time_seconds")):
		return format.Timestamp // epoch seconds, e.g. process_start_time_seconds (not *_seconds_total counters)
	case strings.HasSuffix(n, "_bytes"):
		return format.Bytes
	case strings.HasSuffix(n, "_seconds"):
		return format.Seconds
	}
	return format.None
}

// sameValue is equality that also holds between two NaNs.
func sameValue(a, b float64) bool { return a == b || math.IsNaN(a) && math.IsNaN(b) }

func oneLine(s string) string { return strings.ReplaceAll(s, "\n", " ") }
