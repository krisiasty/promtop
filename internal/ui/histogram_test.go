package ui

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/stats"
)

// ghwFixture holds two identical scrapes of a real exporter's payload, so the
// window has no new observations and histogram rows fall back to lifetime values.
func ghwFixture(t *testing.T) *fixture {
	t.Helper()
	payload, err := os.ReadFile("testdata/ghw_agent.prom")
	if err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, 0)
	f.apply(string(payload))
	f.apply(string(payload))
	return f
}

const cpuProbe = `ghw_agent_probe_duration_seconds{subsystem="cpu"}`

func TestHistoryHistogramPanelsMatchListedValues(t *testing.T) {
	m := ghwFixture(t).model(146, 36)
	m.cursor = rowIndex(m, cpuProbe)
	m.view = ViewHistory
	out := plain(m.render())
	if !strings.Contains(out, "window 5m · 0 samples · histogram") || strings.Contains(out, "0.90ms") {
		t.Errorf("History must show the empty window rather than lifetime histogram stats:\n%s", out)
	}
}

func TestHistogramRowsInTable(t *testing.T) {
	f := ghwFixture(t)
	m := f.model(200, 36)
	i := rowIndex(m, cpuProbe)
	if i < 0 {
		t.Fatal("no Table row for the cpu probe histogram")
	}
	out := m.render()
	assertFrame(t, out, 200, 36)
	row := plainLines(out)[4+i]
	// lifetime fallback: mean 0.838/928, p50/p99 from the buckets, max = upper edge of the highest non-empty bucket
	for _, want := range []string{cpuProbe, " his ", "0.90ms", "0.61ms", "4.80ms", "10.0ms"} {
		if !strings.Contains(row, want) {
			t.Errorf("cpu row lacks %q: %q", want, row)
		}
	}
	for _, id := range []string{`ghw_agent_probe_duration_seconds{subsystem="gpu"}`, `ghw_agent_probe_duration_seconds{subsystem="network"}`, "go_gc_pauses_seconds"} {
		if rowIndex(m, id) < 0 {
			t.Errorf("missing histogram row %s", id)
		}
	}
	for _, w := range []int{80, 100, 146} {
		assertFrame(t, f.model(w, 24).render(), w, 24)
	}
}

func TestHistogramPreviousMeanDuringFirstWindow(t *testing.T) {
	f := newFixture(t, 0)
	text := func(sum, count int) string {
		return fmt.Sprintf("# TYPE h histogram\nh_bucket{le=\"10\"} %d\nh_bucket{le=\"+Inf\"} %d\nh_sum %d\nh_count %d\n",
			count, count, sum, count)
	}
	f.apply(text(10, 10))
	f.apply(text(30, 20))
	f.apply(text(60, 30))
	m := f.model(146, 36)
	x, lifetime := m.histStats(m.st.Series()[0])
	if lifetime || x.Cur != 2.5 || x.Prev != 2 || x.Delta != 0.5 {
		t.Errorf("histogram stats during first window = %+v, lifetime %v; want current 2.5, previous 2, delta 0.5", x, lifetime)
	}
}

func TestHistogramStatsAndTrendAfterReset(t *testing.T) {
	f := newFixture(t, 0)
	f.apply("# TYPE h histogram\nh_bucket{le=\"1\"} 2\nh_bucket{le=\"+Inf\"} 2\nh_sum 2\nh_count 2\n")
	f.apply("# TYPE h histogram\nh_bucket{le=\"1\"} 1\nh_bucket{le=\"+Inf\"} 3\nh_sum 12\nh_count 3\n")
	m := f.model(146, 36)
	row := m.st.Series()[0]
	x, lifetime := m.histStats(row)
	if lifetime || x.Cur != 4 || x.N != 3 {
		t.Errorf("histogram reset window stats = %+v, lifetime %v; want current 4, observations 3", x, lifetime)
	}
	if rates := m.histRates(row.Hist); len(rates) != 2 || rates[1] != 3 {
		t.Errorf("histogram reset trend = %v; want last rate 3", rates)
	}
}

func TestHistogramWindowMeanWithNegativeObservations(t *testing.T) {
	f := newFixture(t, 0)
	f.apply("# TYPE h histogram\nh_bucket{le=\"0\"} 0\nh_bucket{le=\"+Inf\"} 1\nh_sum 10\nh_count 1\n")
	f.apply("# TYPE h histogram\nh_bucket{le=\"0\"} 1\nh_bucket{le=\"+Inf\"} 2\nh_sum 8\nh_count 2\n")
	m := f.model(146, 36)
	x, lifetime := m.histStats(m.st.Series()[0])
	if lifetime || x.Cur != -2 || x.N != 1 {
		t.Errorf("first negative window stats = %+v, lifetime %v; want current -2, observations 1", x, lifetime)
	}
	f.apply("# TYPE h histogram\nh_bucket{le=\"0\"} 2\nh_bucket{le=\"+Inf\"} 3\nh_sum -4\nh_count 3\n")
	m = f.model(146, 36)
	row := m.st.Series()[0]
	x, lifetime = m.histStats(row)
	if lifetime || x.Cur != -7 || x.Prev != -2 || x.Delta != -5 || x.N != 2 {
		t.Errorf("signed window stats = %+v, lifetime %v; want current -7, previous -2, delta -5, observations 2", x, lifetime)
	}
	if got := fmt.Sprint(row.Values(1, 3)); got != "[NaN -2 -12]" {
		t.Errorf("histogram interval mean history = %s; want [NaN -2 -12]", got)
	}
}

func TestHistogramOptionalComponentsAcrossViews(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sum, count bool
		wantMean   string
	}{
		{"missing sum", false, true, "—"},
		{"missing sum and count", false, false, "—"},
		{"measured zero sum", true, true, "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 0)
			text := func(bucket, n int) string {
				payload := fmt.Sprintf("# TYPE h histogram\nh_bucket{le=\"0\"} %d\nh_bucket{le=\"10\"} %d\nh_bucket{le=\"+Inf\"} %d\n", bucket, n, n)
				if tc.sum {
					payload += "h_sum 0\n"
				}
				if tc.count {
					payload += fmt.Sprintf("h_count %d\n", n)
				}
				return payload + "# EOF\n"
			}
			f.apply(text(10, 10))
			f.apply(text(12, 20))
			m := f.model(146, 36)
			x, lifetime := m.histStats(m.st.Series()[0])
			if lifetime || x.P50 != 3.75 || format.Num(x.Cur, format.None) != tc.wantMean {
				t.Errorf("optional component stats = %+v, lifetime %v; want window p50 3.75, mean %s", x, lifetime, tc.wantMean)
			}
			for _, view := range []View{ViewTable, ViewHistory, ViewGraph, ViewHeatmap} {
				m.view = view
				out := m.render()
				assertFrame(t, out, 146, 36)
				key := "mean"
				switch view {
				case ViewTable:
					key = "his"
				case ViewGraph:
					key = "avg"
				}
				found := false
				for _, line := range plainLines(out) {
					fields := strings.Fields(line)
					for i := 0; i+1 < len(fields); i++ {
						if fields[i] == key {
							found = true
							if fields[i+1] != tc.wantMean {
								t.Errorf("view %v %s = %s; want %s", view, key, fields[i+1], tc.wantMean)
							}
						}
					}
				}
				if !found {
					t.Errorf("view %v has no %s value:\n%s", view, key, plain(out))
				}
				if view == ViewHeatmap && heatLife(m) {
					t.Error("heatmap must retain window bucket activity when count is omitted")
				}
			}
		})
	}
}

func TestHistogramPopupListsBuckets(t *testing.T) {
	f := ghwFixture(t)
	m := f.model(146, 36)
	m.cursor = rowIndex(m, cpuProbe)
	press(m, "enter")
	out := m.render()
	assertFrame(t, out, 146, 36)
	text := plain(out)
	for _, want := range []string{"window 5m · since start", "╭─ buckets ", "↑ ↓ scroll buckets"} {
		if !strings.Contains(text, want) {
			t.Errorf("popup lacks %q:\n%s", want, text)
		}
	}
	var le1 string
	for _, ln := range plainLines(out) {
		if strings.Contains(ln, "│ ≤1.00ms ") {
			le1 = ln
		}
	}
	if !strings.Contains(le1, " 765 ") || !strings.Contains(le1, "82.4%") {
		t.Errorf("≤1.00ms bucket row = %q", le1)
	}
	press(m, "down", "down", "down", "down", "down", "down", "down", "down", "down", "down")
	if !strings.Contains(plain(m.render()), "│ +Inf ") {
		t.Error("scrolling the bucket list must reach +Inf")
	}
	if got := format.Num(1.024e-6, format.Seconds); got != "1.02µs" {
		t.Errorf("µs formatting = %q", got)
	}
}

func TestHeatmapSinceStart(t *testing.T) {
	f := ghwFixture(t)
	m := f.model(146, 36)
	press(m, "4", "m", "down") // by series; second source is the cpu probe
	text := plain(m.render())
	if !strings.Contains(text, `subsystem="cpu"`) || !strings.Contains(text, "buckets · since start") {
		t.Fatalf("heatmap must fall back to lifetime buckets:\n%s", text)
	}
	if !strings.Contains(text, "c window") {
		t.Error("hint for the c key missing")
	}
	press(m, "c")
	if text = plain(m.render()); !strings.Contains(text, "buckets · window 5m") || !strings.Contains(text, "c since start") {
		t.Errorf("c must switch to the window view:\n%s", text)
	}
	press(m, "c")
	if !strings.Contains(plain(m.render()), "buckets · since start") {
		t.Error("c must switch back to since start")
	}
}

func TestHistogramPopupShortTerminal(t *testing.T) {
	f := ghwFixture(t)
	m := f.model(80, 24)
	m.cursor = rowIndex(m, cpuProbe)
	press(m, "enter")
	out := m.render()
	assertFrame(t, out, 80, 24)
	if strings.Contains(plain(out), "buckets ·") || m.labMax() != 0 {
		t.Error("without room for a bucket row the popup must leave the buckets box out")
	}
}

func TestObservedRange(t *testing.T) {
	inf := math.Inf(1)
	le := []float64{0.1, 1, 10, inf}
	for _, c := range []struct {
		cum    []float64
		lo, hi string
	}{
		{[]float64{0, 2, 2, 2}, "0.1", "1"},
		{[]float64{3, 3, 5, 5}, "0", "10"},
		{[]float64{0, 0, 0, 4}, "10", "+Inf"},
		{[]float64{0, 0, 0, 0}, "NaN", "NaN"},
	} {
		lo, hi := observedRange(le, c.cum)
		if fmt.Sprint(lo) != c.lo || fmt.Sprint(hi) != c.hi {
			t.Errorf("observedRange(%v) = %v, %v; want %s, %s", c.cum, lo, hi, c.lo, c.hi)
		}
	}
	if got := fmt.Sprint(perBucket([]float64{1, 3, 3, 7})); got != "[1 2 0 4]" {
		t.Errorf("perBucket = %s", got)
	}
}

func TestGaugeHistogramCurrentDistributionAcrossViews(t *testing.T) {
	f := newFixture(t, 0)
	for i, v := range []struct {
		bucket, count int
		sum           float64
	}{{10, 20, 15}, {10, 20, 15}, {5, 10, 10}} {
		f.apply(fmt.Sprintf("# TYPE h gaugehistogram\nh_bucket{le=\"1\"} %d\nh_bucket{le=\"+Inf\"} %d\nh_gsum %g\nh_gcount %d\n# EOF\n", v.bucket, v.count, v.sum, v.count))
		m := f.model(146, 36)
		x, lifetime := m.histStats(m.st.Series()[0])
		vals, _ := m.values(m.st.Series()[0])
		if lifetime || x.Cur != v.sum/float64(v.count) || x.Mean != stats.Summarize(vals).Mean || x.N != v.count {
			t.Errorf("snapshot %d statistics = %+v, lifetime %v", i, x, lifetime)
		}
		if i > 0 && x.Prev != 0.75 {
			t.Errorf("snapshot %d previous mean = %v; want 0.75", i, x.Prev)
		}
		for _, view := range []View{ViewTable, ViewHistory, ViewGraph, ViewHeatmap, ViewSeries} {
			m.view = view
			out := m.render()
			assertFrame(t, out, 146, 36)
			text := plain(out)
			switch view {
			case ViewTable:
				if !strings.Contains(text, " ghs ") || !strings.Contains(text, format.Num(x.Cur, format.None)) {
					t.Errorf("gauge table row lacks type or current mean:\n%s", text)
				}
			case ViewGraph:
				if !strings.Contains(text, "gaugehistogram · mean") || strings.Contains(text, "interval mean") {
					t.Errorf("gauge graph describes counter intervals:\n%s", text)
				}
			case ViewHistory:
				if !strings.Contains(text, "gaugehistogram") || strings.Contains(text, "0 samples") {
					t.Errorf("gauge history lost snapshot means:\n%s", text)
				}
			case ViewHeatmap:
				if !strings.Contains(text, "current distribution") || !strings.Contains(text, "observed") || strings.Contains(text, "requests/s") || strings.Contains(text, "since start") {
					t.Errorf("gauge heatmap describes counter activity:\n%s", text)
				}
				for _, line := range plainLines(out) {
					fields := strings.Fields(line)
					for j := 0; j+1 < len(fields); j++ {
						if fields[j] == "mean" && fields[j+1] != format.Num(x.Cur, format.None) {
							t.Errorf("gauge heatmap mean = %s; want current mean %v", fields[j+1], x.Cur)
						}
					}
				}
				from, to := m.st.Window(m.window)
				d := m.st.HeatSources(false)[0].Data(from, to)
				cols := heatColumns(d, m.st.Times(from, to), m.st.LastTime(), m.window, 1)
				if got := fmt.Sprint(cols[0]); got != fmt.Sprint([]int{v.bucket, v.count - v.bucket}) {
					t.Errorf("folded gauge heat column = %s; want the latest snapshot", got)
				}
				if got := fmt.Sprint(heatBuckets(d)); got != fmt.Sprint([]int{v.bucket, v.count - v.bucket}) {
					t.Errorf("gauge heat panel buckets = %s; want the latest snapshot, not a sum of snapshots", got)
				}
				press(m, "c")
				if m.heatSet || heatLife(m) {
					t.Error("gauge distribution must not acquire counter lifetime scope")
				}
			case ViewSeries:
				if !strings.Contains(text, "bucket counts") || strings.Contains(text, "rate(_bucket") {
					t.Errorf("gauge family panel describes counter rates:\n%s", text)
				}
			}
		}
		for _, view := range []View{ViewTable, ViewHistory, ViewGraph, ViewHeatmap, ViewSeries} {
			narrow := f.model(80, 24)
			narrow.view = view
			assertFrame(t, narrow.render(), 80, 24)
		}
		m.view = ViewTable
		press(m, "enter")
		if text := plain(m.render()); !strings.Contains(text, "current distribution") || strings.Contains(text, "since start") || strings.Contains(text, "share    rate/s") {
			t.Errorf("gauge popup describes counter intervals:\n%s", text)
		}
	}
}

// heatLife reports whether the selected heat source's panels show lifetime totals.
func heatLife(m *Model) bool {
	_, life := m.heatScope()
	return life
}
