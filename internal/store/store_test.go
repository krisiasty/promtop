package store

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/krisiasty/promtop/internal/expo"
	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/scrape"
)

func apply(t *testing.T, st *Store, sec int, text string) {
	t.Helper()
	p, err := expo.Parse(strings.NewReader(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	st.Apply(&scrape.Result{Parsed: p, Status: "200 OK", ContentType: "text/plain", Bytes: len(text),
		Duration: 10 * time.Millisecond, At: t0.Add(time.Duration(sec) * time.Second)})
}

func payload(req, b1, b2, binf, sum int) string {
	return fmt.Sprintf(`# TYPE up gauge
up 1
# TYPE http_requests_total counter
http_requests_total{method="GET",code="200"} %d
# TYPE node_memory_bytes gauge
node_memory_bytes 2048
# TYPE rpc_seconds summary
rpc_seconds{quantile="0.5"} 0.2
rpc_seconds_sum 10
rpc_seconds_count 50
# TYPE req_duration_seconds histogram
req_duration_seconds_bucket{le="0.1"} %d
req_duration_seconds_bucket{le="1"} %d
req_duration_seconds_bucket{le="+Inf"} %d
req_duration_seconds_sum %d
req_duration_seconds_count %d
`, req, b1, b2, binf, sum, binf)
}

const reqID = `http_requests_total{method="GET",code="200"}`
const reqKey = `http_requests_total{code="200",method="GET"}`

func TestApplySeriesAndFamilies(t *testing.T) {
	st := New(time.Second)
	apply(t, st, 0, payload(100, 1, 2, 3, 5))
	apply(t, st, 1, payload(110, 2, 5, 7, 9))
	var ids []string
	for _, s := range st.Series() {
		ids = append(ids, s.ID+":"+s.Type.String())
	}
	want := "up:gauge " + reqID + `:counter node_memory_bytes:gauge rpc_seconds{quantile="0.5"}:gauge rpc_seconds_sum:counter rpc_seconds_count:counter req_duration_seconds:histogram`
	if got := strings.Join(ids, " "); got != want {
		t.Fatalf("series = %s\nwant     %s", got, want)
	}
	byID := map[string]*Series{}
	for _, s := range st.Series() {
		byID[s.ID] = s
	}
	if byID["node_memory_bytes"].Unit != format.Bytes || byID[`rpc_seconds{quantile="0.5"}`].Unit != format.Seconds || byID[reqID].Unit != format.None {
		t.Error("units inferred from family names")
	}
	from, to := st.Window(time.Minute)
	if from != 1 || to != 2 || st.Seq() != 2 || !st.LastTime().Equal(t0.Add(time.Second)) {
		t.Fatalf("window %d..%d seq %d", from, to, st.Seq())
	}
	if v := byID[reqID].Values(from, to); len(v) != 2 || v[0] != 100 || v[1] != 110 {
		t.Errorf("values = %v", v)
	}
	if ts := st.Times(from, to); len(ts) != 2 || !ts[0].Equal(t0) {
		t.Errorf("times = %v", ts)
	}
	fams := st.Families()
	if len(fams) != 5 || fams[1].Samples != 1 || strings.Join(fams[1].LabelNames, ",") != "method,code" ||
		len(fams[1].Series) != 1 || fams[4].Samples != 5 || strings.Join(fams[4].LabelNames, ",") != "le" || len(fams[4].Hists) != 1 {
		t.Errorf("families = %+v", fams)
	}
	if v, ok := st.PrevValue(reqKey); !ok || v != 100 {
		t.Errorf("PrevValue = %v %v", v, ok)
	}
	if st.Last() == nil || st.Scrape.State() != Live {
		t.Error("Last/State after Apply")
	}
}

func TestCounterResetAndRestart(t *testing.T) {
	st := New(time.Second)
	apply(t, st, 0, "# TYPE c counter\nc 10\nprocess_start_time_seconds 100\n")
	apply(t, st, 1, "# TYPE c counter\nc 4\nprocess_start_time_seconds 200\n")
	c := st.Series()[0]
	if !c.ResetAt.Equal(t0.Add(time.Second)) {
		t.Errorf("ResetAt = %v", c.ResetAt)
	}
	if st.Reset.Count != 1 || !st.Reset.Restarted || !st.Reset.At.Equal(t0.Add(time.Second)) {
		t.Errorf("Reset = %+v", st.Reset)
	}
}

func TestDisappearReturnAndForget(t *testing.T) {
	st := New(time.Second)
	apply(t, st, 0, "a 1\nb 1\n")
	apply(t, st, 1, "a 2\n")
	if len(st.Series()) != 1 || st.Series()[0].ID != "a" {
		t.Fatalf("absent series must leave the list: %v", st.Series())
	}
	apply(t, st, 2, "a 3\nb 3\n")
	b := st.Series()[1]
	if v := b.Values(1, 3); len(v) != 3 || v[0] != 1 || !math.IsNaN(v[1]) || v[2] != 3 {
		t.Errorf("returning series history = %v", v)
	}
	slow := New(time.Minute) // capacity 31
	apply(t, slow, 0, "gone 1\nstay 1\n")
	for i := 1; i <= 35; i++ {
		apply(t, slow, i*60, "stay 1\n")
	}
	if _, ok := slow.series["gone"]; ok {
		t.Error("series absent for a full window must be forgotten")
	}
}

func TestLastChange(t *testing.T) {
	st := New(time.Second)
	apply(t, st, 0, "g 5\n")
	apply(t, st, 1, "g 5\n")
	apply(t, st, 2, "g 6\n")
	apply(t, st, 3, "g 6\n")
	if lc := st.Series()[0].LastChange; !lc.Equal(t0.Add(2 * time.Second)) {
		t.Errorf("LastChange = %v", lc)
	}
}

func TestReorderedLabelsKeepSeriesHistoryAndPin(t *testing.T) {
	st := New(time.Second)
	apply(t, st, 0, "# TYPE m counter\nm{a=\"1\",b=\"2\"} 10\n")
	first := st.Series()[0]
	st.SetPinned(first.Key, true)
	apply(t, st, 1, "# TYPE m counter\nm{b=\"2\",a=\"1\"} 11\n")
	if got := st.Series(); len(got) != 1 || got[0] != first {
		t.Fatalf("series identity changed: %v", got)
	}
	if first.ID != `m{b="2",a="1"}` || first.Key != `m{a="1",b="2"}` ||
		fmt.Sprint(first.Values(1, 2)) != "[10 11]" {
		t.Errorf("series after reordering = %+v, values = %v", first, first.Values(1, 2))
	}
	if !st.Pinned(first.Key) || st.PinCount() != 1 {
		t.Error("pin did not follow the reordered series")
	}
	if v, ok := st.PrevValue(first.Key); !ok || v != 10 {
		t.Errorf("previous value = %v, %v", v, ok)
	}
}

func TestReorderedHistogramLabelsKeepOneGroup(t *testing.T) {
	st := New(time.Second)
	apply(t, st, 0, `# TYPE h histogram
h_bucket{a="1",b="2",le="1"} 2
h_bucket{le="+Inf",b="2",a="1"} 5
h_sum{b="2",a="1"} 10
h_count{a="1",b="2"} 5
`)
	if len(st.hists) != 1 || len(st.Families()[0].Hists) != 1 || len(st.Series()) != 1 {
		t.Fatalf("histogram split into groups: %d histograms, %d rows", len(st.hists), len(st.Series()))
	}
	h, row := st.Families()[0].Hists[0], st.Series()[0]
	if _, cum, sum, count := h.Totals(); fmt.Sprint(cum) != "[2 5]" || sum != 10 || count != 5 {
		t.Errorf("first totals = %v, %v, %v", cum, sum, count)
	}
	st.SetPinned(row.Key, true)
	apply(t, st, 1, `# TYPE h histogram
h_bucket{b="2",a="1",le="1"} 3
h_bucket{a="1",le="+Inf",b="2"} 7
h_sum{a="1",b="2"} 14
h_count{b="2",a="1"} 7
`)
	if len(st.hists) != 1 || st.Families()[0].Hists[0] != h || len(st.Series()) != 1 || st.Series()[0] != row {
		t.Fatal("histogram identity changed after labels were reordered")
	}
	if _, cum, sum, count := h.Totals(); fmt.Sprint(cum) != "[3 7]" || sum != 14 || count != 7 {
		t.Errorf("second totals = %v, %v, %v", cum, sum, count)
	}
	if v, ok := st.PrevValue(`h_bucket{a="1",b="2",le="1"}`); !ok || v != 2 {
		t.Errorf("previous bucket value = %v, %v", v, ok)
	}
	if got := row.Values(1, 2); !math.IsNaN(got[0]) || got[1] != 2 || !st.Pinned(row.Key) {
		t.Errorf("histogram row history or pin = %v, pinned %v", got, st.Pinned(row.Key))
	}
}

func TestWindow(t *testing.T) {
	st := New(time.Second)
	for i := range 400 {
		apply(t, st, i, fmt.Sprintf("a %d\n", i))
	}
	from, to := st.Window(5 * time.Minute)
	if to != 400 || to-from+1 != 300 {
		t.Errorf("Window(5m) = %d..%d, want 300 samples ending at 400", from, to)
	}
	slow := New(time.Minute)
	for i := range 35 {
		apply(t, slow, i*60, fmt.Sprintf("a %d\n", i))
	}
	from, to = slow.Window(30 * time.Minute)
	if to != 35 || to-from+1 != 30 {
		t.Errorf("Window(30m) = %d..%d, want 30 samples ending at 35", from, to)
	}
	for range 2 { // Series asks for two windows per frame; both stay cached until the next scrape
		if from, _ := st.Window(time.Minute); from != 341 {
			t.Errorf("Window(1m) from = %d, want 341", from)
		}
		if from, _ := st.Window(5 * time.Minute); from != 101 {
			t.Errorf("Window(5m) from = %d, want 101", from)
		}
	}
	apply(t, st, 400, "a 400\n")
	if from, to := st.Window(time.Minute); from != 342 || to != 401 {
		t.Errorf("Window(1m) after a scrape = %d..%d, want 342..401", from, to)
	}
}

func TestHighCardinality(t *testing.T) {
	var b strings.Builder
	b.WriteString("# TYPE x counter\n")
	for i := range HighCardinality + 1 {
		fmt.Fprintf(&b, "x{i=\"%d\"} 1\n", i)
	}
	st := New(time.Second)
	st.SetPinned(`x{i="0"}`, true)
	apply(t, st, 0, b.String())
	if !st.HighCard() {
		t.Fatal("more than 10,000 series must switch to high-cardinality mode")
	}
	if st.series[`x{i="1"}`].Capacity() != HighCardCap || st.series[`x{i="0"}`].Capacity() != st.capacity {
		t.Error("non-pinned rings capped, pinned rings full")
	}
	if name, n := st.TopFamily(); name != "x" || n != HighCardinality+1 {
		t.Errorf("TopFamily = %s %d", name, n)
	}
	st.SetPinned(`x{i="1"}`, true)
	if st.series[`x{i="1"}`].Capacity() != st.capacity || st.PinCount() != 2 || !st.Pinned(`x{i="1"}`) {
		t.Error("pinning restores full history")
	}
}

func TestHistoryBudgetAtExactThreshold(t *testing.T) {
	st := New(100 * time.Millisecond)
	p := &expo.Parsed{Samples: HighCardinality}
	st.setCapacityFor(p)
	if st.HighCard() {
		t.Fatal("exactly 10,000 samples must remain below high-cardinality mode")
	}
	if st.historyCap >= st.capacity {
		t.Fatalf("history capacity = %d, want less than the full %d slots", st.historyCap, st.capacity)
	}
	estimated := int64(st.ts.Cap())*timestampSlotBytes +
		int64(HighCardinality)*int64(st.historyCap)*historySlotBytes
	if estimated > HistoryBudgetBytes {
		t.Errorf("estimated ring storage = %d bytes, budget = %d", estimated, HistoryBudgetBytes)
	}
	if got := st.ringCap(`x{i="0"}`); got != st.historyCap {
		t.Errorf("first-scrape ring capacity = %d, budget capacity = %d", got, st.historyCap)
	}
	larger := NewWithBudget(100*time.Millisecond, 2<<30)
	larger.setCapacityFor(p)
	if larger.historyCap <= st.historyCap || larger.historyCap > larger.capacity {
		t.Errorf("2 GiB capacity = %d, 512 MiB capacity = %d", larger.historyCap, st.historyCap)
	}
}

func TestHistoryBudgetResizesExistingAndIncomingRings(t *testing.T) {
	st := New(time.Second)
	st.historyBudgetBytes = int64(st.capacity)*timestampSlotBytes + 8*historySlotBytes
	apply(t, st, 0, "a 1\n")
	a := st.Series()[0]
	if got := a.Capacity(); got != 8 {
		t.Fatalf("initial capacity = %d, want 8", got)
	}
	st.SetPinned(a.Key, true)
	apply(t, st, 1, "a 2\nb 3\n")
	if a.Capacity() != 2 || st.Series()[1].Capacity() != 2 || !st.Pinned(a.Key) {
		t.Errorf("resized capacities = %d, %d; pinned = %v", a.Capacity(), st.Series()[1].Capacity(), st.Pinned(a.Key))
	}
	if got := a.Values(1, 2); fmt.Sprint(got) != "[1 2]" {
		t.Errorf("resize lost recent history: %v", got)
	}
}

func TestHistoryBudgetCountsHistogramStorage(t *testing.T) {
	st := New(time.Second)
	st.historyBudgetBytes = int64(st.capacity)*timestampSlotBytes + 120*historySlotBytes
	gauges := &expo.Parsed{Samples: 3, Families: []*expo.Family{{Type: expo.Gauge, Samples: make([]expo.Sample, 3)}}}
	histogram := &expo.Parsed{Samples: 3, Families: []*expo.Family{{Type: expo.Histogram, Samples: make([]expo.Sample, 3)}}}
	if st.budgetCap(histogram) >= st.budgetCap(gauges) {
		t.Error("histogram bucket vectors and extra rings must count toward the budget")
	}
}

func TestHistogramData(t *testing.T) {
	st := New(time.Second)
	apply(t, st, 0, payload(100, 1, 2, 3, 5))
	apply(t, st, 1, payload(110, 2, 5, 7, 9))
	apply(t, st, 2, payload(120, 1, 1, 1, 1)) // histogram reset
	srcs := st.HeatSources(false)
	if len(srcs) != 1 || srcs[0].Family != "req_duration_seconds" || srcs[0].Unit != format.Seconds {
		t.Fatalf("sources = %+v", srcs)
	}
	from, to := st.Window(time.Minute)
	d := srcs[0].Data(from, to)
	if len(d.LE) != 3 || !math.IsInf(d.LE[2], 1) || d.Inc[0] != nil {
		t.Fatalf("data = %+v", d)
	}
	if fmt.Sprint(d.Inc[1]) != "[1 2 1]" || fmt.Sprint(d.Inc[2]) != "[1 0 0]" {
		t.Errorf("increments = %v %v, want [1 2 1] [1 0 0]", d.Inc[1], d.Inc[2])
	}
	if d.Count != 5 || d.Sum != 5 {
		t.Errorf("count/sum increase = %v/%v, want 5/5", d.Count, d.Sum)
	}
	if got := fmt.Sprint(srcs[0].Bucket(2, from, to)); got != "[3 7 1]" {
		t.Errorf("Bucket(+Inf) = %s", got)
	}
}

func TestHistogramResetAppliesToEveryComponent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		prev, cur [3]int
		prevSum   int
		curSum    int
	}{
		{"count drops while sum grows", [3]int{100, 100, 100}, [3]int{0, 1, 1}, 5, 10},
		{"empty bucket stays unchanged", [3]int{0, 100, 100}, [3]int{0, 1, 1}, 500, 10},
		{"nonempty bucket stays unchanged", [3]int{1, 100, 100}, [3]int{1, 2, 2}, 500, 10},
		{"bucket drops while count grows", [3]int{2, 2, 2}, [3]int{1, 2, 3}, 2, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := func(b [3]int, sum int) string {
				return fmt.Sprintf("# TYPE h histogram\nh_bucket{le=\"1\"} %d\nh_bucket{le=\"10\"} %d\nh_bucket{le=\"+Inf\"} %d\nh_sum %d\nh_count %d\n",
					b[0], b[1], b[2], sum, b[2])
			}
			st := New(time.Second)
			apply(t, st, 0, text(tc.prev, tc.prevSum))
			apply(t, st, 2, text(tc.cur, tc.curSum))
			row := st.Series()[0]
			wantCum := fmt.Sprint(tc.cur[:])
			wantCount, wantSum := float64(tc.cur[2]), float64(tc.curSum)
			if cum, sum, count := row.Hist.Increase(1, 2); fmt.Sprint(cum) != wantCum || sum != wantSum || count != wantCount {
				t.Errorf("reset increase = %v, %v, %v; want %s, %v, %v", cum, sum, count, wantCum, wantSum, wantCount)
			}
			if got := row.Last(); got != wantSum/wantCount {
				t.Errorf("reset interval mean = %v; want %v", got, wantSum/wantCount)
			}
			src := st.HeatSources(false)[0]
			d := src.Data(1, 2)
			wantInc := []int{tc.cur[0], tc.cur[1] - tc.cur[0], tc.cur[2] - tc.cur[1]}
			if fmt.Sprint(d.Inc[1]) != fmt.Sprint(wantInc) || d.Sum != wantSum || d.Count != wantCount || d.Rate != wantCount/2 {
				t.Errorf("reset heatmap = %+v; want increments %v, sum %v, count %v, rate %v", d, wantInc, wantSum, wantCount, wantCount/2)
			}
			for k, count := range tc.cur {
				wantRate := float64(count) / 2
				if rates := src.BucketRates(k, 1, 2, st.Times(1, 2)); rates[1] != wantRate {
					t.Errorf("bucket %d reset rates = %v; want last %v", k, rates, wantRate)
				}
				if rate := src.BucketRate1m(k, 1, 2, st.Times(1, 2)); rate != wantRate {
					t.Errorf("bucket %d reset one-minute rate = %v; want %v", k, rate, wantRate)
				}
			}
			// A later interval must resume normal subtraction from the reset state.
			next := tc.cur
			for k := range next {
				next[k]++
			}
			apply(t, st, 4, text(next, tc.curSum+1))
			if cum, sum, count := row.Hist.Increase(1, 3); fmt.Sprint(cum) != fmt.Sprint(next[:]) || sum != wantSum+1 || count != wantCount+1 {
				t.Errorf("window spanning reset and normal interval = %v, %v, %v", cum, sum, count)
			}
		})
	}
}

func TestHistogramSignedSumChanges(t *testing.T) {
	for _, tc := range []struct {
		name        string
		prev, cur   [3]int // bucket <= 0, count, sum
		wantSum     float64
		wantCount   float64
		wantBuckets string
	}{
		{"sum stays positive", [3]int{0, 1, 10}, [3]int{1, 2, 8}, -2, 1, "[1 1]"},
		{"sum crosses zero", [3]int{0, 1, 10}, [3]int{1, 2, -2}, -12, 1, "[1 1]"},
		{"sum stays negative", [3]int{1, 1, -2}, [3]int{2, 2, -5}, -3, 1, "[1 1]"},
		{"positive observation with negative sum", [3]int{1, 1, -5}, [3]int{1, 2, -2}, 3, 1, "[0 1]"},
		{"count reset with negative sum", [3]int{2, 2, -5}, [3]int{1, 1, -2}, -2, 1, "[1 1]"},
		{"bucket reset with growing count", [3]int{1, 1, -5}, [3]int{0, 2, 4}, 4, 2, "[0 2]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := func(v [3]int) string {
				return fmt.Sprintf("# TYPE h histogram\nh_bucket{le=\"0\"} %d\nh_bucket{le=\"+Inf\"} %d\nh_sum %d\nh_count %d\n", v[0], v[1], v[2], v[1])
			}
			st := New(time.Second)
			apply(t, st, 0, text(tc.prev))
			apply(t, st, 1, text(tc.cur))
			row := st.Series()[0]
			if cum, sum, count := row.Hist.Increase(2, 2); fmt.Sprint(cum) != tc.wantBuckets || sum != tc.wantSum || count != tc.wantCount {
				t.Errorf("signed interval changes = %v, %v, %v; want %s, %v, %v", cum, sum, count, tc.wantBuckets, tc.wantSum, tc.wantCount)
			}
			if got := row.Last(); got != tc.wantSum/tc.wantCount {
				t.Errorf("interval mean = %v; want %v", got, tc.wantSum/tc.wantCount)
			}
			// Observe -1 in the next interval to verify signed window accumulation.
			next := tc.cur
			next[0]++
			next[1]++
			next[2]--
			apply(t, st, 2, text(next))
			if got := row.Last(); got != -1 {
				t.Errorf("next negative interval mean = %v; want -1", got)
			}
			if _, sum, count := row.Hist.Increase(1, 3); sum != tc.wantSum-1 || count != tc.wantCount+1 {
				t.Errorf("signed window changes = %v, %v; want %v, %v", sum, count, tc.wantSum-1, tc.wantCount+1)
			}
			d := st.HeatSources(false)[0].Data(1, 3)
			if d.Sum != tc.wantSum-1 || d.Count != tc.wantCount+1 {
				t.Errorf("signed heatmap totals = %+v; want sum %v, count %v", d, tc.wantSum-1, tc.wantCount+1)
			}
		})
	}
}

func TestHistogramRatesAcrossResetAndGap(t *testing.T) {
	st := New(time.Second)
	text := func(bucket, count int) string {
		return fmt.Sprintf("# TYPE h histogram\nh_bucket{le=\"1\"} %d\nh_bucket{le=\"+Inf\"} %d\nh_count %d\n", bucket, count, count)
	}
	apply(t, st, 0, text(2, 2))
	apply(t, st, 10, text(3, 3))
	apply(t, st, 20, "up 1\n")
	apply(t, st, 30, text(1, 5)) // reset detected across the missing scrape
	apply(t, st, 40, text(2, 6))
	apply(t, st, 90, text(3, 7))
	h := st.Series()[0].Hist
	times := st.Times(1, 6)
	want := []float64{math.NaN(), 0.1, math.NaN(), 0.25, 0.1, 0.02}
	if got := h.CountRates(1, 6, times); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("observation rates across reset and gap = %v; want %v", got, want)
	}
	src := st.HeatSources(false)[0]
	for k := range 2 {
		// The reset interval ends exactly at the minute's first sample and
		// must be excluded; only the two subsequent observations count.
		if got := src.BucketRate1m(k, 1, 6, times); got != 2.0/60 {
			t.Errorf("bucket %d clipped one-minute rate = %v; want %v", k, got, 2.0/60)
		}
		if got := src.BucketRate1m(k, 1, 6, times[:2]); got != 0.1 {
			t.Errorf("bucket %d rate with truncated timestamps = %v; want 0.1", k, got)
		}
		if got := src.BucketRate1m(k, 6, 6, times[5:]); !math.IsNaN(got) {
			t.Errorf("bucket %d rate with one sample = %v; want unavailable", k, got)
		}
	}
	if got := h.CountRates(1, 6, nil); fmt.Sprint(got) != "[NaN NaN NaN NaN NaN NaN]" {
		t.Errorf("rates without timestamps = %v; want unavailable", got)
	}
}

func TestHistogramWindowSkipsIncompleteIntervals(t *testing.T) {
	st := New(time.Second)
	hist := func(bucket, count int) string {
		return fmt.Sprintf(`# TYPE duration_seconds histogram
duration_seconds_bucket{le="1"} %d
duration_seconds_bucket{le="+Inf"} %d
duration_seconds_sum %d
duration_seconds_count %d
`, bucket, count, count, count)
	}
	apply(t, st, 0, hist(8, 10))
	apply(t, st, 1, "up 1\n") // the histogram label set is absent
	apply(t, st, 2, hist(16, 20))
	h := st.Series()[0].Hist
	if got := st.Series()[0].Last(); !math.IsNaN(got) {
		t.Fatalf("returning histogram interval mean = %v, want unavailable", got)
	}
	if cum, sum, count := h.Increase(1, 3); cum != nil || sum != 0 || count != 0 {
		t.Fatalf("incomplete window increase = %v, %v, %v", cum, sum, count)
	}
	d := st.HeatSources(false)[0].Data(1, 3)
	if d.Count != 0 || d.Sum != 0 || d.Inc[2] != nil {
		t.Fatalf("incomplete heatmap interval = %+v", d)
	}
	apply(t, st, 3, hist(19, 24))
	if got := st.Series()[0].Last(); got != 1 {
		t.Errorf("next complete interval mean = %v, want 1", got)
	}
	if cum, sum, count := h.Increase(1, 4); fmt.Sprint(cum) != "[3 4]" || sum != 4 || count != 4 {
		t.Errorf("complete window increase = %v, %v, %v", cum, sum, count)
	}
	d = st.HeatSources(false)[0].Data(1, 4)
	if d.Count != 4 || d.Sum != 4 || fmt.Sprint(d.Inc[3]) != "[3 1]" {
		t.Errorf("complete heatmap interval = %+v", d)
	}
}

// OpenMetrics histograms may omit _sum and _count; their buckets still count.
func TestHistogramWindowWithoutSumAndCount(t *testing.T) {
	st := New(time.Second)
	hist := func(n int) string {
		return fmt.Sprintf("# TYPE h histogram\nh_bucket{le=\"1\"} %d\nh_bucket{le=\"+Inf\"} %d\n", n, n)
	}
	apply(t, st, 0, hist(10))
	apply(t, st, 1, hist(20))
	apply(t, st, 2, hist(30))
	if cum, sum, count := st.Series()[0].Hist.Increase(1, 3); fmt.Sprint(cum) != "[20 20]" || !math.IsNaN(sum) || !math.IsNaN(count) {
		t.Errorf("bucket-only window increase = %v, %v, %v", cum, sum, count)
	}
	if d := st.HeatSources(false)[0].Data(1, 3); fmt.Sprint(d.Inc) != "[[] [10 0] [10 0]]" || !math.IsNaN(d.Sum) || !math.IsNaN(d.Count) || !math.IsNaN(d.Rate) {
		t.Errorf("bucket-only heatmap data = %+v", d)
	}
}

func TestHistogramOptionalSumKeepsUnknownAndZeroDistinct(t *testing.T) {
	for _, withSum := range []bool{false, true} {
		t.Run(fmt.Sprintf("sum present %v", withSum), func(t *testing.T) {
			text := func(n int) string {
				sum := ""
				if withSum {
					sum = "h_sum 0\n"
				}
				return fmt.Sprintf("# TYPE h histogram\nh_bucket{le=\"0\"} %d\nh_bucket{le=\"+Inf\"} %d\nh_count %d\n%s# EOF\n", n, n, n, sum)
			}
			st := New(time.Second)
			apply(t, st, 0, text(10))
			apply(t, st, 1, text(20))
			apply(t, st, 2, text(30))
			row := st.Series()[0]
			want := math.NaN()
			if withSum {
				want = 0
			}
			if got := row.Last(); fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("interval mean = %v; want %v", got, want)
			}
			if cum, sum, count := row.Hist.Increase(1, 3); fmt.Sprint(cum) != "[20 20]" || fmt.Sprint(sum) != fmt.Sprint(want) || count != 20 {
				t.Errorf("optional sum window = %v, %v, %v", cum, sum, count)
			}
			d := st.HeatSources(false)[0].Data(1, 3)
			if fmt.Sprint(d.Sum) != fmt.Sprint(want) || d.Count != 20 || d.Rate != 10 {
				t.Errorf("optional sum heatmap = %+v; want sum %v, count 20, rate 10", d, want)
			}
		})
	}
}

func TestHistogramOptionalComponentChangesBetweenScrapes(t *testing.T) {
	for _, component := range []string{"sum", "count"} {
		for _, missing := range []int{10, 20} {
			t.Run(fmt.Sprintf("%s absent at count %d", component, missing), func(t *testing.T) {
				text := func(n int) string {
					payload := fmt.Sprintf("# TYPE h histogram\nh_bucket{le=\"0\"} %d\nh_bucket{le=\"+Inf\"} %d\n", n, n)
					if component != "sum" || n != missing {
						payload += "h_sum 0\n"
					}
					if component != "count" || n != missing {
						payload += fmt.Sprintf("h_count %d\n", n)
					}
					return payload
				}
				st := New(time.Second)
				apply(t, st, 0, text(10))
				apply(t, st, 1, text(20))
				row := st.Series()[0]
				cum, sum, count := row.Hist.Increase(1, 2)
				if fmt.Sprint(cum) != "[10 10]" || !math.IsNaN(row.Last()) ||
					(component == "sum" && (!math.IsNaN(sum) || count != 10)) ||
					(component == "count" && (sum != 0 || !math.IsNaN(count))) {
					t.Errorf("partly unavailable component: buckets %v, sum %v, count %v, mean %v", cum, sum, count, row.Last())
				}
				if d := st.HeatSources(false)[0].Data(1, 2); fmt.Sprint(d.Inc) != "[[] [10 0]]" {
					t.Errorf("partly unavailable component lost bucket activity: %+v", d)
				}
			})
		}
	}
}

func TestHeatDataWithMixedOptionalComponents(t *testing.T) {
	st := New(time.Second)
	text := func(n int) string {
		return fmt.Sprintf("# TYPE h histogram\nh_bucket{instance=\"a\",le=\"+Inf\"} %d\nh_sum{instance=\"a\"} %d\nh_count{instance=\"a\"} %d\nh_bucket{instance=\"b\",le=\"+Inf\"} %d\n", n, n, n, n)
	}
	apply(t, st, 0, text(10))
	apply(t, st, 1, text(20))
	d := st.HeatSources(false)[0].Data(1, 2)
	if !math.IsNaN(d.Sum) || !math.IsNaN(d.Count) || d.Rate != 10 || fmt.Sprint(d.Inc) != "[[] [20]]" {
		t.Errorf("mixed optional components = %+v; want unknown sum/count, known rate 10, bucket increase 20", d)
	}
}

// Intervals broken by a missing label set add no observations, so they must
// not add time to the rate either; a family's rate sums its label sets' rates.
func TestHeatRateSkipsIncompleteIntervals(t *testing.T) {
	st := New(time.Second)
	hist := func(label string, count int) string {
		return fmt.Sprintf("h_bucket{instance=%q,le=\"+Inf\"} %d\nh_sum{instance=%q} %d\nh_count{instance=%q} %d\n",
			label, count, label, count, label, count)
	}
	apply(t, st, 0, "# TYPE h histogram\n"+hist("a", 0)+hist("b", 0))
	apply(t, st, 10, "# TYPE h histogram\n"+hist("a", 10)+hist("b", 10))
	apply(t, st, 20, "# TYPE h histogram\n"+hist("a", 20)) // b is absent
	apply(t, st, 30, "# TYPE h histogram\n"+hist("a", 30)+hist("b", 30))
	apply(t, st, 40, "# TYPE h histogram\n"+hist("a", 40)+hist("b", 40))
	srcs := st.HeatSources(true)
	if a, b := srcs[0].Hists[0].Seconds(1, 5), srcs[1].Hists[0].Seconds(1, 5); a != 40 || b != 20 {
		t.Errorf("included seconds a/b = %v/%v, want 40/20", a, b)
	}
	if got := srcs[1].Data(1, 5).Rate; got != 1 {
		t.Errorf("label set rate across a gap = %v/s, want 1/s", got)
	}
	if got := st.HeatSources(false)[0].Data(1, 5).Rate; got != 2 {
		t.Errorf("family rate = %v/s, want the sum of its label sets' 1/s", got)
	}
	if got := srcs[1].Data(3, 4).Rate; !math.IsNaN(got) {
		t.Errorf("rate with no included interval = %v, want unavailable", got)
	}
}

func TestHeatDataSkipsMismatchedBuckets(t *testing.T) {
	text := func(n int) string {
		return fmt.Sprintf(`# TYPE h histogram
h_bucket{a="x",le="1"} %d
h_bucket{a="x",le="+Inf"} %d
h_sum{a="x"} 1
h_count{a="x"} %d
h_bucket{a="y",le="5"} %d
h_bucket{a="y",le="+Inf"} %d
h_sum{a="y"} 1
h_count{a="y"} %d
`, n, 2*n, 2*n, 100*n, 100*n, 100*n)
	}
	st := New(time.Second)
	apply(t, st, 0, text(1))
	apply(t, st, 1, text(2))
	fam := st.HeatSources(false)
	if len(fam) != 1 {
		t.Fatalf("by family = %d sources", len(fam))
	}
	d := fam[0].Data(st.Window(time.Minute))
	if fmt.Sprint(d.LE[:1]) != "[1]" || fmt.Sprint(d.Inc[1]) != "[1 1]" || d.Count != 2 {
		t.Errorf("only a=\"x\" (first layout) may be summed: %+v", d)
	}
	if bySeries := st.HeatSources(true); len(bySeries) != 2 || bySeries[1].Labels[0].Value != "y" {
		t.Errorf("by series = %+v", bySeries)
	}
}

func TestFamilyBucketRatesHandleDisappearingLabelSet(t *testing.T) {
	st := New(time.Second)
	series := func(label string, count int) string {
		return fmt.Sprintf("h_bucket{instance=\"%s\",le=\"+Inf\"} %d\nh_sum{instance=\"%s\"} %d\nh_count{instance=\"%s\"} %d\n",
			label, count, label, count, label, count)
	}
	apply(t, st, 0, "# TYPE h histogram\n"+series("a", 100)+series("b", 100))
	apply(t, st, 1, "# TYPE h histogram\n"+series("a", 110))
	apply(t, st, 2, "# TYPE h histogram\n"+series("a", 120)+series("b", 110))
	src := st.HeatSources(false)[0]
	times := st.Times(1, 3)
	rates := src.BucketRates(0, 1, 3, times)
	if len(rates) != 3 || !math.IsNaN(rates[0]) || rates[1] != 10 || rates[2] != 15 {
		t.Errorf("per-scrape family bucket rates = %v, want [NaN 10 15]", rates)
	}
	if got := src.BucketRate1m(0, 1, 3, times); got != 15 {
		t.Errorf("one-minute family bucket rate = %v, want 15", got)
	}
}

// The reset banner covers every counter reset in the last ResetHold, not just
// the latest resetting scrape, and a later reset keeps an earlier restart.
func TestResetStateAccumulatesWithinHold(t *testing.T) {
	st := New(time.Second)
	text := func(a, b, start int) string {
		return fmt.Sprintf("# TYPE a counter\na %d\n# TYPE b counter\nb %d\nprocess_start_time_seconds %d\n", a, b, start)
	}
	apply(t, st, 0, text(10, 10, 100))
	apply(t, st, 1, text(4, 11, 200)) // a resets, target restarted
	apply(t, st, 2, text(5, 3, 200))  // b resets one scrape later
	if rs := st.Reset; rs.Count != 2 || !rs.Restarted || !rs.At.Equal(t0.Add(2*time.Second)) || !rs.RestartAt.Equal(t0.Add(time.Second)) {
		t.Errorf("after the second reset: %+v, want 2 counters, restarted at 1s, latest at 2s", rs)
	}
	apply(t, st, 30, text(6, 4, 200))
	if rs := st.Reset; rs.Count != 2 || !rs.Restarted {
		t.Errorf("within the hold: %+v", rs)
	}
	apply(t, st, 61, text(7, 5, 200)) // a's reset (1s) and the restart are 60s old
	if rs := st.Reset; rs.Count != 1 || rs.Restarted || !rs.At.Equal(t0.Add(2*time.Second)) {
		t.Errorf("after a's reset aged out: %+v, want 1 counter, not restarted", rs)
	}
}

// Histogram samples count towards high cardinality (they cost the most
// memory), histogram rings are capped in that mode and restored after it.
func TestHighCardinalityFromHistograms(t *testing.T) {
	hist := func(sets int) string {
		var b strings.Builder
		b.WriteString("# TYPE g gauge\ng 1\n# TYPE h_seconds histogram\n")
		for i := range sets {
			for _, le := range []string{"0.1", "0.5", "1", "5", "10", "30", "60", "120", "+Inf"} {
				fmt.Fprintf(&b, "h_seconds_bucket{i=\"%d\",le=\"%s\"} 1\n", i, le)
			}
			fmt.Fprintf(&b, "h_seconds_sum{i=\"%d\"} 1\nh_seconds_count{i=\"%d\"} 1\n", i, i)
		}
		return b.String()
	}
	st := New(time.Second)
	apply(t, st, 0, hist(1000)) // 1 Table series + 11,000 histogram samples
	if !st.HighCard() {
		t.Fatal("more than 10,000 Table series plus histogram samples must switch to high-cardinality mode")
	}
	h := st.hists[`h_seconds{i="7"}`]
	if h.cum.Cap() != HighCardCap || h.sum.Cap() != HighCardCap || h.count.Cap() != HighCardCap {
		t.Errorf("histogram rings = %d/%d/%d, want %d", h.cum.Cap(), h.sum.Cap(), h.count.Cap(), HighCardCap)
	}
	if st.series["g"].Capacity() != HighCardCap {
		t.Errorf("series ring = %d, want %d", st.series["g"].Capacity(), HighCardCap)
	}
	if name, n := st.TopFamily(); name != "h_seconds" || n != 11000 || len(st.Series()) != 1001 {
		t.Errorf("TopFamily = %s %d with %d series, want h_seconds 11000 with 1001 (1 gauge + 1000 histogram rows)", name, n, len(st.Series()))
	}
	if c := st.series[`h_seconds{i="7"}`].Capacity(); c != HighCardCap {
		t.Errorf("histogram row ring = %d, want %d", c, HighCardCap)
	}
	apply(t, st, 1, hist(10))
	if st.HighCard() {
		t.Fatal("110 histogram samples leave high-cardinality mode")
	}
	if h = st.hists[`h_seconds{i="7"}`]; h.cum.Cap() != st.capacity || h.sum.Cap() != st.capacity || h.count.Cap() != st.capacity {
		t.Errorf("histogram rings after leaving = %d/%d/%d, want %d", h.cum.Cap(), h.sum.Cap(), h.count.Cap(), st.capacity)
	}
	if v := h.cum.Get(2); v == nil {
		t.Error("the resized ring keeps the latest bucket vector")
	}
}

func TestSummarySampleUnits(t *testing.T) {
	for _, tc := range []struct {
		name, unit string
		want       format.Unit
	}{
		{"latency_seconds", "", format.Seconds},
		{"payload_bytes", "", format.Bytes},
		{"latency", "seconds", format.Seconds},
		{"payload", "bytes", format.Bytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := New(time.Second)
			for i := range 2 {
				text := fmt.Sprintf("# TYPE %s summary\n", tc.name)
				if tc.unit != "" {
					text += fmt.Sprintf("# UNIT %s %s\n", tc.name, tc.unit)
				}
				text += fmt.Sprintf("%s{quantile=\"0.5\"} 0.01\n%s_sum 1\n%s_count %d\n", tc.name, tc.name, tc.name, 100+10*i)
				apply(t, st, i, text)
				if got := st.Families()[0].Unit; got != tc.want {
					t.Errorf("family unit = %v, want %v", got, tc.want)
				}
				want := map[string]struct {
					unit format.Unit
					typ  expo.MetricType
				}{
					tc.name:            {tc.want, expo.Gauge},
					tc.name + "_sum":   {tc.want, expo.Counter},
					tc.name + "_count": {format.None, expo.Counter},
				}
				for _, s := range st.Series() {
					w, ok := want[s.Name]
					if !ok {
						t.Errorf("unexpected series %s", s.ID)
						continue
					}
					delete(want, s.Name)
					if s.Unit != w.unit || s.Type != w.typ {
						t.Errorf("%s unit/type = %v/%v, want %v/%v", s.ID, s.Unit, s.Type, w.unit, w.typ)
					}
				}
				for name := range want {
					t.Errorf("series %s is missing", name)
				}
			}
		})
	}
}

func TestSeriesUnitFollowsMetadataChanges(t *testing.T) {
	st := New(time.Second)
	body := "latency{quantile=\"0.5\"} 0.01\nlatency_sum 1\nlatency_count 100\n"
	apply(t, st, 0, "# TYPE latency summary\n"+body)
	apply(t, st, 1, "# TYPE latency summary\n# UNIT latency seconds\n"+body)
	want := map[string]format.Unit{"latency": format.Seconds, "latency_sum": format.Seconds, "latency_count": format.None}
	for _, s := range st.Series() {
		if s.Unit != want[s.Name] || fmt.Sprint(s.Values(1, 2)) == fmt.Sprint([]float64{math.NaN(), s.Last()}) {
			t.Errorf("%s unit = %v, history %v; want %v with history kept", s.ID, s.Unit, s.Values(1, 2), want[s.Name])
		}
	}
	apply(t, st, 2, "# TYPE latency summary\n"+body)
	for _, s := range st.Series() {
		if s.Unit != format.None {
			t.Errorf("%s unit = %v after # UNIT was dropped; want none", s.ID, s.Unit)
		}
	}
}

func TestTimestampUnit(t *testing.T) {
	st := New(time.Second)
	apply(t, st, 0, "process_start_time_seconds 1790000000\n"+
		"node_boot_time_seconds 1789000000\n"+
		"job_last_success_timestamp_seconds 1790001000\n"+
		"# TYPE busy_time_seconds_total counter\nbusy_time_seconds_total 12\n"+
		"request_seconds 0.2\n")
	want := map[string]format.Unit{
		"process_start_time_seconds":         format.Timestamp,
		"node_boot_time_seconds":             format.Timestamp,
		"job_last_success_timestamp_seconds": format.Timestamp,
		"busy_time_seconds_total":            format.Seconds,
		"request_seconds":                    format.Seconds,
	}
	for _, s := range st.Series() {
		if s.Unit != want[s.ID] {
			t.Errorf("%s unit = %v, want %v", s.ID, s.Unit, want[s.ID])
		}
	}
}

func TestHistogramRows(t *testing.T) {
	st := New(time.Second)
	apply(t, st, 0, payload(100, 1, 2, 3, 5))
	apply(t, st, 1, payload(110, 2, 5, 7, 9))
	apply(t, st, 2, payload(120, 1, 1, 1, 1)) // histogram reset
	var h *Series
	for _, s := range st.Series() {
		if s.ID == "req_duration_seconds" {
			h = s
		}
	}
	if h == nil || h.Type != expo.Histogram || h.Hist == nil || h.Unit != format.Seconds {
		t.Fatalf("histogram row = %+v", h)
	}
	// per-scrape interval mean: first scrape unknown, then (9-5)/(7-3), then across the reset 1/1
	if v := h.Values(1, 3); !math.IsNaN(v[0]) || v[1] != 1 || v[2] != 1 {
		t.Errorf("interval means = %v", v)
	}
	le, cum, sum, count := h.Hist.Totals()
	if len(le) != 3 || fmt.Sprint(cum) != "[1 1 1]" || sum != 1 || count != 1 {
		t.Errorf("totals = %v %v %v %v", le, cum, sum, count)
	}
	if cum, sum, count := h.Hist.Increase(2, 3); fmt.Sprint(cum) != "[2 4 5]" || sum != 5 || count != 5 {
		t.Errorf("increase over the reset = %v %v %v", cum, sum, count)
	}
	if cum, sum, count := h.Hist.At(2); fmt.Sprint(cum) != "[2 5 7]" || sum != 9 || count != 7 {
		t.Errorf("at 2 = %v %v %v", cum, sum, count)
	}
	src := st.HeatSources(false)[0]
	if le, cum, sum, count := src.Totals(); len(le) != 3 || fmt.Sprint(cum) != "[1 1 1]" || sum != 1 || count != 1 {
		t.Errorf("source totals = %v %v %v %v", le, cum, sum, count)
	}
}

func TestGaugeHistogramSnapshots(t *testing.T) {
	st := New(time.Second)
	for i, v := range []struct {
		bucket, count int
		sum           float64
	}{
		{10, 20, 15},
		{10, 20, 15}, // an unchanged distribution still has a mean
		{5, 10, 10},  // a decrease is a current distribution, not a reset
		{6, 12, 18},  // growth must not become an interval mean of 4
	} {
		apply(t, st, i, fmt.Sprintf("# TYPE h gaugehistogram\nh_bucket{le=\"1\"} %d\nh_bucket{le=\"+Inf\"} %d\nh_gsum %g\nh_gcount %d\n# EOF\n", v.bucket, v.count, v.sum, v.count))
		row := st.Series()[0]
		h := row.Hist
		if h == nil {
			t.Fatal("gauge histogram was not stored as a distribution")
		}
		if row.Type.String() != "gaugehistogram" || row.Last() != v.sum/float64(v.count) {
			t.Errorf("snapshot %d row = type %v, mean %v; want gaugehistogram, %v", i, row.Type, row.Last(), v.sum/float64(v.count))
		}
		if _, cum, sum, count := h.Totals(); fmt.Sprint(cum) != fmt.Sprint([]int{v.bucket, v.count}) || sum != v.sum || count != float64(v.count) {
			t.Errorf("snapshot %d totals = %v, %v, %v", i, cum, sum, count)
		}
		seq := uint64(i + 1)
		if cum, _, _ := h.Increase(1, seq); cum != nil {
			t.Errorf("snapshot %d invented counter increases: %v", i, cum)
		}
		if h.resetBetween(seq-1, seq) {
			t.Errorf("snapshot %d treated a gauge decrease as a counter reset", i)
		}
		for _, rate := range h.CountRates(1, seq, st.Times(1, seq)) {
			if !math.IsNaN(rate) {
				t.Errorf("gauge observation rate = %v; want unavailable", rate)
			}
		}
		src := st.HeatSources(false)[0]
		d := src.Data(1, seq)
		if d.Sum != v.sum || d.Count != float64(v.count) || !math.IsNaN(d.Rate) || fmt.Sprint(d.Inc[i]) != fmt.Sprint([]int{v.bucket, v.count - v.bucket}) {
			t.Errorf("snapshot %d heat data = %+v; want current sum/count and snapshot buckets", i, d)
		}
		if rate := src.BucketRate1m(0, 1, seq, st.Times(1, seq)); !math.IsNaN(rate) {
			t.Errorf("gauge bucket rate = %v; want unavailable", rate)
		}
	}
	if got := fmt.Sprint(st.Series()[0].Values(1, 4)); got != "[0.75 0.75 1 1.5]" {
		t.Errorf("gauge histogram snapshot means = %s", got)
	}
}

func TestGaugeHistogramOmittedTotalsDoNotReusePreviousSnapshot(t *testing.T) {
	st := New(time.Second)
	apply(t, st, 0, "# TYPE h gaugehistogram\nh_bucket{le=\"1\"} 10\nh_bucket{le=\"+Inf\"} 20\nh_gsum 15\nh_gcount 20\n# EOF\n")
	apply(t, st, 1, "# TYPE h gaugehistogram\nh_bucket{le=\"1\"} 5\nh_bucket{le=\"+Inf\"} 10\n# EOF\n")
	row := st.Series()[0]
	if _, cum, sum, count := row.Hist.Totals(); fmt.Sprint(cum) != "[5 10]" || !math.IsNaN(sum) || !math.IsNaN(count) || !math.IsNaN(row.Last()) {
		t.Errorf("omitted current totals = %v, %v, %v; mean %v", cum, sum, count, row.Last())
	}
	if d := st.HeatSources(false)[0].Data(1, 2); !math.IsNaN(d.Sum) || !math.IsNaN(d.Count) || fmt.Sprint(d.Inc[1]) != "[5 5]" {
		t.Errorf("omitted current heatmap totals = %+v", d)
	}
	apply(t, st, 2, "# TYPE h gaugehistogram\nh_bucket{le=\"1\"} 5\nh_bucket{le=\"+Inf\"} 10\nh_gsum 10\nh_gcount 10\n# EOF\n")
	if got := row.Last(); got != 1 {
		t.Errorf("reappearing gauge totals mean = %v; want current mean 1", got)
	}
}

func TestHistogramPlainTypeChangeStartsFreshHistory(t *testing.T) {
	st := New(time.Second)
	gauge := "# TYPE h gaugehistogram\nh_bucket{le=\"1\"} 5\nh_bucket{le=\"+Inf\"} 10\nh_gsum 10\nh_gcount 10\n# EOF\n"
	apply(t, st, 0, gauge)
	apply(t, st, 1, "# TYPE h gauge\nh 5\n")
	row := st.Series()[0]
	if row.Hist != nil || row.Type != expo.Gauge || fmt.Sprint(row.Values(1, 2)) != "[NaN 5]" {
		t.Errorf("histogram to gauge = hist %v, type %v, history %v", row.Hist != nil, row.Type, row.Values(1, 2))
	}
	apply(t, st, 2, gauge)
	row = st.Series()[0]
	if row.Hist == nil || row.Type != expo.GaugeHistogram || fmt.Sprint(row.Values(1, 3)) != "[NaN NaN 1]" {
		t.Errorf("gauge to histogram = hist %v, type %v, history %v", row.Hist != nil, row.Type, row.Values(1, 3))
	}
	if cum, _, _ := row.Hist.At(1); cum != nil {
		t.Errorf("histogram kept buckets from before the gauge: %v", cum)
	}
	apply(t, st, 3, "# TYPE h counter\nh 1\n")
	apply(t, st, 4, "# TYPE h gauge\nh 0\n")
	if row = st.Series()[0]; row.Type != expo.Gauge || !row.ResetAt.IsZero() || fmt.Sprint(row.Values(4, 5)) != "[NaN 0]" {
		t.Errorf("counter to gauge = type %v, reset %v, history %v", row.Type, row.ResetAt, row.Values(4, 5))
	}
}

func TestHistogramTypeChangeStartsFreshHistory(t *testing.T) {
	st := New(time.Second)
	classic := "# TYPE h histogram\nh_bucket{le=\"1\"} 10\nh_bucket{le=\"+Inf\"} 20\nh_sum 15\nh_count 20\n"
	apply(t, st, 0, classic)
	st.SetPinned("h", true)
	apply(t, st, 1, "# TYPE h gaugehistogram\nh_bucket{le=\"1\"} 5\nh_bucket{le=\"+Inf\"} 10\nh_gsum 10\nh_gcount 10\n# EOF\n")
	row := st.Series()[0]
	if row.Type != expo.GaugeHistogram || fmt.Sprint(row.Values(1, 2)) != "[NaN 1]" || !st.Pinned("h") {
		t.Errorf("gauge type transition = type %v, history %v, pinned %v", row.Type, row.Values(1, 2), st.Pinned("h"))
	}
	apply(t, st, 2, classic)
	row = st.Series()[0]
	if row.Type != expo.Histogram || !math.IsNaN(row.Last()) || !st.Pinned("h") {
		t.Errorf("counter type transition = type %v, mean %v, pinned %v", row.Type, row.Last(), st.Pinned("h"))
	}
}

func TestExposedNaN(t *testing.T) {
	st := New(time.Second)
	hist := "# TYPE h histogram\nh_bucket{le=\"+Inf\"} 1\nh_sum 1\nh_count 1\n"
	apply(t, st, 0, "# TYPE g gauge\ng 7\n"+hist)
	apply(t, st, 1, "# TYPE g gauge\ng NaN\n"+hist) // h is idle: its interval mean is NaN
	byName := map[string]*Series{}
	for _, s := range st.Series() {
		byName[s.Name] = s
	}
	if g := byName["g"]; !g.ExposedNaN(2) || g.ExposedNaN(1) {
		t.Errorf("gauge exposed NaN at 2/1 = %v/%v; want true/false", g.ExposedNaN(2), g.ExposedNaN(1))
	}
	if h := byName["h"]; !math.IsNaN(h.Last()) || h.ExposedNaN(2) {
		t.Errorf("idle histogram interval = %v, exposed NaN %v; want NaN mean that is not exposed", h.Last(), h.ExposedNaN(2))
	}
	g := byName["g"]
	apply(t, st, 2, hist) // g is absent, not NaN
	if g.ExposedNaN(3) {
		t.Error("an absent sample is not an exposed NaN")
	}
}

func TestCounterResetAcrossNaN(t *testing.T) {
	st := New(time.Second)
	apply(t, st, 0, "# TYPE c counter\nc 100\n")
	apply(t, st, 1, "# TYPE c counter\nc NaN\n")
	apply(t, st, 2, "# TYPE c counter\nc 5\n")
	if c := st.Series()[0]; !c.ResetAt.Equal(t0.Add(2*time.Second)) || st.Reset.Count != 1 {
		t.Errorf("a drop across an exposed NaN is a reset: ResetAt %v, count %d", c.ResetAt, st.Reset.Count)
	}
}

func TestLastChangeIgnoresRepeatedNaN(t *testing.T) {
	st := New(time.Second)
	apply(t, st, 0, "g NaN\n")
	apply(t, st, 1, "g NaN\n")
	if lc := st.Series()[0].LastChange; !lc.Equal(t0) {
		t.Errorf("a new NaN series changes on first sight only: LastChange = %v", lc)
	}
	apply(t, st, 2, "g 1\n")
	apply(t, st, 3, "g NaN\n")
	apply(t, st, 4, "g NaN\n")
	if lc := st.Series()[0].LastChange; !lc.Equal(t0.Add(3 * time.Second)) {
		t.Errorf("NaN repeated is no change: LastChange = %v", lc)
	}
}

func TestNaNStartTimeIsNoRestart(t *testing.T) {
	st := New(time.Second)
	apply(t, st, 0, "process_start_time_seconds 100\n")
	apply(t, st, 1, "process_start_time_seconds NaN\n")
	apply(t, st, 2, "process_start_time_seconds NaN\n")
	apply(t, st, 3, "process_start_time_seconds 100\n")
	if st.Reset.Restarted || !st.Reset.RestartAt.IsZero() {
		t.Errorf("NaN start times are not restarts: %+v", st.Reset)
	}
}

func TestFailuresBeforeEachScrape(t *testing.T) {
	st := New(time.Second)
	apply(t, st, 0, "a 1\n")
	for i := range 3 {
		st.Fail(&scrape.Error{At: t0.Add(time.Duration(i+1) * time.Second)})
	}
	apply(t, st, 10, "a 2\n")
	apply(t, st, 11, "a 3\n")
	if got := fmt.Sprint(st.Failures(1, 3)); got != "[0 3 0]" {
		t.Errorf("Failures = %s", got)
	}
}
