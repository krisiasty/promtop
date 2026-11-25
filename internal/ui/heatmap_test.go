package ui

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/store"
)

// heatIndent is the grid's label column plus its gap; heatLabel is a row's
// right-aligned label in it.
var heatIndent = strings.Repeat(" ", rangeW+1)

func heatLabel(s string) string { return format.PadLeft(s, rangeW) + " " }

func TestHeatmapLayout(t *testing.T) {
	f := newFixture(t, fixtureSteps)
	m := f.model(146, 36)
	m.view = ViewHeatmap
	out := m.render()
	assertFrame(t, out, 146, 36)
	lines := plainLines(out)
	if !strings.HasPrefix(lines[3], "http_request_duration_seconds  histogram · 11 buckets · columns normalised") {
		t.Errorf("title = %q", lines[3])
	}
	for row, label := range map[int]string{5: ">5s", 6: "2.5–5s", 9: "250–500ms", 15: "≤5ms"} {
		if label = heatLabel(label); !strings.HasPrefix(lines[row], label) {
			t.Errorf("row %d = %q, want prefix %q", row, lines[row], label)
		}
	}
	cells := []rune(lines[11]) // 50–100ms row: cells 15..98 are heat cells
	for _, r := range cells[rangeW+1 : min(len(cells), 99)] {
		if !strings.ContainsRune(" ░▒▓█", r) {
			t.Fatalf("unexpected heat glyph %q in %q", r, lines[11])
		}
	}
	ticks, labels := format.Axis(99-(rangeW+1), 5*time.Minute)
	if !strings.HasPrefix(lines[16], heatIndent+ticks) || !strings.HasPrefix(lines[17], heatIndent+strings.TrimRight(labels, " ")) {
		t.Errorf("axis:\n%s\n%s", lines[16], lines[17])
	}
	for i, q := range []string{"p50", "p90", "p99"} {
		if !strings.HasPrefix(lines[19+i], heatLabel(q)) {
			t.Errorf("quantile row %d = %q", i, lines[19+i])
		}
	}
	if i := strings.Index(lines[4], "╭─ buckets · window 5m ─"); i < 0 || format.Width(lines[4][:i]) != 102 {
		t.Errorf("buckets panel = %q", lines[4])
	}
	if !strings.Contains(lines[5], "│ ≤5ms ") || !strings.Contains(lines[15], "│ >5s ") || !strings.Contains(lines[17], "╭─ histogram_quantile ─") {
		t.Errorf("panels:\n%s", strings.Join(lines[4:19], "\n"))
	}
	if !strings.HasPrefix(lines[34], "last scrape 12:36:59 · changed samples highlighted") {
		t.Errorf("status = %q", lines[34])
	}

	m = f.model(80, 36)
	m.view = ViewHeatmap
	lines = plainLines(m.render())
	if !strings.HasPrefix(strings.TrimLeft(lines[23], " "), "p50 ") || !strings.HasSuffix(strings.TrimRight(lines[23], " "), "requests/s 232") || !strings.Contains(lines[23], "   requests/s ") || strings.Contains(lines[4], "╭") {
		t.Errorf("narrow summary = %q", lines[23])
	}
	press(m, "m")
	if !strings.HasPrefix(plainLines(m.render())[3], "http_request_duration_seconds") {
		t.Error("by-series mode keeps the family name in the title")
	}
}

// TestHeatmapNarrowPanelCounts covers the w=142..145 range (pw=40..43): the
// buckets panel is shown (heatPanelMin=40) but was narrow enough that the SI
// count column used to collapse to 1..4 cells and truncate to "…".
func TestHeatmapNarrowPanelCounts(t *testing.T) {
	f := newFixture(t, fixtureSteps)
	m := f.model(142, 36)
	m.view = ViewHeatmap
	out := m.render()
	assertFrame(t, out, 142, 36)
	rows := 0
	for _, ln := range plainLines(out) {
		if !strings.Contains(ln, "%") { // only the panel's bucket rows have a share column
			continue
		}
		rows++
		if strings.Contains(ln, "… │") {
			t.Errorf("count column truncated: %q", ln)
		}
	}
	if rows < 11 {
		t.Errorf("found %d bucket panel rows, want 11", rows)
	}
}

func TestHeatmapWithoutHistograms(t *testing.T) {
	f := newFixture(t, 0)
	f.apply("up 1\n")
	m := f.model(146, 36)
	m.view = ViewHeatmap
	if !strings.Contains(plain(m.render()), "no histogram metrics in this target") {
		t.Error("missing empty-heatmap message")
	}
}

func TestHeatColumnsUseScrapeTimes(t *testing.T) {
	latest := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	d := store.HeatData{LE: []float64{1}, Inc: [][]float64{{2}, {3}, {5}}}
	times := []time.Time{latest.Add(-4 * time.Minute), latest.Add(-time.Minute), latest}
	cols := heatColumns(d, times, latest, 5*time.Minute, 11)
	for c, col := range cols {
		want := 0.0
		switch c {
		case 2:
			want = 2
		case 8:
			want = 3
		case 10:
			want = 5
		}
		if col[0] != want {
			t.Errorf("column %d = %v, want %v", c, col[0], want)
		}
	}
	startup := heatColumns(store.HeatData{LE: []float64{1}, Inc: [][]float64{{5}}},
		[]time.Time{latest}, latest, 5*time.Minute, 11)
	for c, col := range startup {
		if c < 10 && col[0] != 0 || c == 10 && col[0] != 5 {
			t.Errorf("startup column %d = %v", c, col[0])
		}
	}
}

func TestHeatmapRateSpansIncludedIncreases(t *testing.T) {
	f := newFixture(t, 0)
	text := func(count int) string {
		return fmt.Sprintf("# TYPE h histogram\nh_bucket{le=\"1\"} %d\nh_bucket{le=\"+Inf\"} %d\nh_sum %d\nh_count %d\n",
			count, count, count, count)
	}
	f.apply(text(10))
	f.n = 10
	f.apply(text(20))
	f.n = 20
	f.apply(text(30))
	m := f.model(146, 36)
	m.view = ViewHeatmap
	for _, window := range []time.Duration{15 * time.Second, 5 * time.Second} {
		m.window = window
		out := plain(m.render())
		found := false
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "requests/s") {
				found = true
				if !strings.Contains(line, "1.00") {
					t.Errorf("window %s rate should use the predecessor scrape: %q", window, line)
				}
				break
			}
		}
		if !found {
			t.Errorf("window %s did not show a requests/s summary", window)
		}
	}
}

// An interval broken by a missing histogram adds no observations, so neither
// the heatmap's requests/s nor the popup's bucket rates may count its time.
func TestHistogramRatesSkipIncompleteIntervals(t *testing.T) {
	f := newFixture(t, 0)
	text := func(count int) string {
		return fmt.Sprintf("# TYPE h histogram\nh_bucket{le=\"1\"} %d\nh_bucket{le=\"+Inf\"} %d\nh_sum %d\nh_count %d\n",
			count, count, count, count)
	}
	for i, payload := range []string{text(0), text(10), "up 1\n", text(30), text(40)} {
		f.n = i * 10
		f.apply(payload)
	}
	m := f.model(146, 36)
	m.window = time.Minute
	m.view = ViewHeatmap
	rate := ""
	for _, line := range strings.Split(plain(m.render()), "\n") {
		if strings.Contains(line, "requests/s") {
			rate = line
		}
	}
	if !strings.Contains(rate, "1.00") {
		t.Errorf("requests/s should span only the complete intervals: %q", rate)
	}
	var hist *store.Series
	for _, s := range m.st.Series() {
		if s.Hist != nil {
			hist = s
		}
	}
	box := plain(strings.Join(m.bucketBox(hist, 60, 4).render(), "\n"))
	seen := false
	for _, line := range strings.Split(box, "\n") {
		if strings.Contains(line, "│ ≤1 ") { // every observation is in this bucket
			seen = true
			if f := strings.Fields(strings.Trim(line, "│ ")); len(f) < 4 || f[3] != "1.00" { // range count share rate bar
				t.Errorf("popup bucket rate should span only the complete intervals: %q", line)
			}
		}
	}
	if !seen {
		t.Errorf("popup lacks the ≤1 bucket row:\n%s", box)
	}
}

// A NaN bucket (here +Inf, always NaN) must neither poison its column's
// maximum nor reach int(): int(NaN) is a negative index on amd64.
func TestHeatmapNaNBuckets(t *testing.T) {
	f := newFixture(t, 0)
	for i := range 5 {
		f.apply(fmt.Sprintf("# TYPE h histogram\nh_bucket{le=\"1\"} %d\nh_bucket{le=\"+Inf\"} NaN\nh_sum 1\nh_count %d\n", 10*i, 10*i))
	}
	m := f.model(80, 36)
	m.view = ViewHeatmap
	out := m.render()
	assertFrame(t, out, 80, 36)
	lines := plainLines(out)
	inf, infOK := strings.CutPrefix(lines[5], format.PadLeft(">1", rangeW)) // a blank row has no trailing space
	le1, le1OK := strings.CutPrefix(lines[6], heatLabel("≤1"))
	if !infOK || !le1OK {
		t.Fatalf("bucket rows:\n%s\n%s", lines[5], lines[6])
	}
	if got := strings.TrimSpace(inf); got != "" {
		t.Errorf("NaN bucket cells must stay blank: %q", got)
	}
	if !strings.ContainsRune(le1, '█') || strings.ContainsAny(le1, "░▒▓") {
		t.Errorf("the finite bucket is its columns' maximum: %q", le1)
	}
}

// Frames between scrapes reuse the heatmap data; a scrape or a window, mode
// or source change recomputes it.
func TestHeatDataCachedBetweenScrapes(t *testing.T) {
	f := newFixture(t, 30)
	m := f.model(146, 36)
	m.view = ViewHeatmap
	first := func() *[]float64 { m.render(); return &m.heat.d.Inc[0] }
	a := first()
	if b := first(); b != a {
		t.Fatal("a second frame at the same scrape recomputed the heatmap data")
	}
	f.step()
	b := first()
	if b == a {
		t.Fatal("a new scrape must recompute the heatmap data")
	}
	press(m, "w")
	if c := first(); c == b {
		t.Error("a window change must recompute the heatmap data")
	}
}

func TestHighCardinalityBannerNamesHistogramFamily(t *testing.T) {
	f := newFixture(t, 0)
	var b strings.Builder
	b.WriteString("up 1\n# TYPE h histogram\n")
	for i := range 1000 {
		for _, le := range []string{"1", "5", "10", "50", "100", "500", "1000", "5000", "+Inf"} {
			fmt.Fprintf(&b, "h_bucket{i=\"%d\",le=\"%s\"} 1\n", i, le)
		}
		fmt.Fprintf(&b, "h_sum{i=\"%d\"} 1\nh_count{i=\"%d\"} 1\n", i, i)
	}
	f.apply(b.String())
	m := f.model(146, 36)
	if got := m.banner(146).text; !strings.HasPrefix(got, "⚠ high cardinality: 1,001 series, 11,000 samples from h · ") {
		t.Errorf("banner = %q", got)
	}
}

// Every view scales to the body height: the axis, the quantile rows and the
// summary are reserved first and buckets are dropped from the +Inf side.
func TestHeatmapBudgetsRowsToBodyHeight(t *testing.T) {
	f := newFixture(t, fixtureSteps)
	m := f.model(80, 24)
	m.view = ViewHeatmap
	out := m.render()
	assertFrame(t, out, 80, 24)
	body := strings.Join(plainLines(out), "\n")
	for _, want := range []string{"\n" + heatLabel("p50") + "▁", "\n" + heatLabel("p90"), "\n" + heatLabel("p99"), "   requests/s ", "▲ 4 more buckets", "\n" + heatLabel("250–500ms"), "\n" + heatLabel("≤5ms")} {
		if !strings.Contains(body, want) {
			t.Errorf("80×24 heatmap lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, ">5s") || strings.Contains(body, "–1s") {
		t.Errorf("the top buckets must be the ones dropped:\n%s", body)
	}

	f = newFixture(t, 0)
	var les []string
	for i := 1; i <= 39; i++ {
		les = append(les, fmt.Sprintf("%d", i))
	}
	les = append(les, "+Inf")
	for s := range 20 {
		var b strings.Builder
		b.WriteString("# TYPE big_seconds histogram\n")
		for i, le := range les {
			fmt.Fprintf(&b, "big_seconds_bucket{le=\"%s\"} %d\n", le, s*(i+1))
		}
		fmt.Fprintf(&b, "big_seconds_sum %d\nbig_seconds_count %d\n", s*100, s*40)
		f.apply(b.String())
	}
	m = f.model(146, 36)
	m.view = ViewHeatmap
	out = m.render()
	assertFrame(t, out, 146, 36)
	lines := plainLines(out)
	if !strings.HasPrefix(lines[5], heatIndent+"▲ 21 more buckets") || !strings.HasPrefix(lines[6], heatLabel("18–19s")) || !strings.HasPrefix(lines[24], heatLabel("≤1s")) {
		t.Errorf("bucket rows:\n%s", strings.Join(lines[4:26], "\n"))
	}
	ticks, _ := format.Axis(99-(rangeW+1), 5*time.Minute)
	if !strings.HasPrefix(lines[25], heatIndent+ticks) || !strings.HasPrefix(lines[26], heatIndent+"-5m") {
		t.Errorf("axis:\n%s\n%s", lines[25], lines[26])
	}
	for i, q := range []string{"p50", "p90", "p99"} {
		if !strings.HasPrefix(lines[28+i], heatLabel(q)) {
			t.Errorf("quantile row %d = %q", i, lines[28+i])
		}
	}
	if !strings.Contains(lines[5], "│ ≤1s ") || !strings.Contains(lines[23], "│ 18–19s ") || !strings.Contains(lines[24], "│ ▼ 21 more buckets") ||
		!strings.Contains(lines[25], "╰──") || !strings.Contains(lines[26], "╭─ histogram_quantile ─") || !strings.Contains(lines[32], "╰──") {
		t.Errorf("panels:\n%s", strings.Join(lines[4:34], "\n"))
	}
}

func TestHeatmapMeanWithoutObservations(t *testing.T) {
	f := newFixture(t, 0)
	g := "# TYPE g gaugehistogram\ng_bucket{le=\"+Inf\"} 0\ng_gsum 1\ng_gcount 0\n" // an empty snapshot with a stale sum
	f.apply(g)
	f.apply(g)
	m := f.model(80, 36)
	m.view = ViewHeatmap
	for _, ln := range plainLines(m.render()) {
		if i := strings.Index(ln, "mean"); i >= 0 && strings.Contains(ln[i:], "Inf") {
			t.Errorf("mean without observations must be unavailable: %q", ln)
		}
	}
}

func TestHeatmapFilter(t *testing.T) {
	f := newFixture(t, 0)
	f.apply(`# TYPE http_seconds histogram
http_seconds_bucket{handler="api",le="1"} 1
http_seconds_bucket{handler="api",le="+Inf"} 1
http_seconds_sum{handler="api"} 1
http_seconds_count{handler="api"} 1
http_seconds_bucket{handler="web",le="1"} 1
http_seconds_bucket{handler="web",le="+Inf"} 1
http_seconds_sum{handler="web"} 1
http_seconds_count{handler="web"} 1
# TYPE rpc_seconds histogram
rpc_seconds_bucket{le="1"} 1
rpc_seconds_bucket{le="+Inf"} 1
rpc_seconds_sum 1
rpc_seconds_count 1
`)
	m := f.model(146, 36)
	m.view = ViewHeatmap
	press(m, "/", "R", "P", "C", "enter")
	lines := plainLines(m.render())
	if !strings.HasPrefix(lines[3], "rpc_seconds  histogram") || !strings.HasPrefix(lines[34], `filter "RPC" · 1 match · esc to clear`) {
		t.Errorf("rpc filter: %q / %q", lines[3], lines[34])
	}
	press(m, "esc")
	if lines = plainLines(m.render()); !strings.HasPrefix(lines[3], "rpc_seconds  histogram") {
		t.Errorf("clearing the filter keeps rpc_seconds selected: %q", lines[3])
	}

	press(m, "m", "/", "w", "e", "b", "enter")
	if lines = plainLines(m.render()); !strings.HasPrefix(lines[3], `http_seconds{handler="web"}  histogram`) ||
		!strings.HasPrefix(lines[34], `filter "web" · 1 match`) {
		t.Errorf("label filter: %q / %q", lines[3], lines[34])
	}
	press(m, "esc", "/", "h", "t", "t", "p", "enter", "G")
	if lines = plainLines(m.render()); !strings.HasPrefix(lines[3], `http_seconds{handler="web"}  histogram`) ||
		!strings.HasPrefix(lines[34], `filter "http" · 2 match`) {
		t.Errorf("G stops at the last match: %q / %q", lines[3], lines[34])
	}

	press(m, "esc", "/", "z", "enter")
	if lines = plainLines(m.render()); lineWith(lines, "No histograms match. Esc clears the filter.") == "" {
		t.Errorf("empty filter result:\n%s", strings.Join(lines, "\n"))
	}
}

func TestBucketRange(t *testing.T) {
	inf := math.Inf(1)
	for _, c := range []struct {
		le   []float64
		k    int
		u    format.Unit
		want string
	}{
		{[]float64{0.00015, 0.0002, 0.0003, inf}, 0, format.Seconds, "≤0.15ms"},
		{[]float64{0.00015, 0.0002, 0.0003, inf}, 1, format.Seconds, "0.15–0.2ms"},
		{[]float64{0.00015, 0.0002, 0.0003, inf}, 3, format.Seconds, ">0.3ms"},
		{[]float64{7.5e-5, 1e-4}, 1, format.Seconds, "75µs–0.1ms"},
		{[]float64{0, 0.005}, 1, format.Seconds, "0–5ms"},
		{[]float64{0.05, 0.1}, 1, format.Seconds, "50–100ms"},
		{[]float64{1200, 1800, 3600}, 2, format.Seconds, "1800–3600s"},
		{[]float64{-0.005, -0.001}, 1, format.Seconds, "-5–-1ms"},
		{[]float64{512 * 1024, 1 << 30}, 1, format.Bytes, "512Ki–1Gi"},
		{[]float64{2500, 5000, inf}, 1, format.None, "2500–5000"},
		{[]float64{2500, 5000, inf}, 2, format.None, ">5000"},
		{[]float64{0.25, 0.5}, 1, format.None, "0.25–0.5"},
		{[]float64{inf}, 0, format.None, "all"},
	} {
		if got := bucketRange(c.le, c.k, c.u); got != c.want {
			t.Errorf("bucketRange(%v, %d) = %q, want %q", c.le, c.k, got, c.want)
		}
	}
}

// On a wide terminal the extra width goes to the grid's time columns; the
// buckets panel stops at heatPanelMax instead of stretching.
func TestHeatmapGridGrowsOnWideTerminal(t *testing.T) {
	f := newFixture(t, fixtureSteps)
	m := f.model(240, 36)
	m.view = ViewHeatmap
	out := m.render()
	assertFrame(t, out, 240, 36)
	lines := plainLines(out)
	lw := 240 - heatPanelMax - 3
	i := strings.Index(lines[4], "╭─ buckets")
	if i < 0 || format.Width(lines[4][:i]) != lw+3 || format.Width(strings.TrimRight(lines[4][i:], " ")) != heatPanelMax {
		t.Errorf("buckets panel should start at %d and be %d wide: %q", lw+3, heatPanelMax, lines[4])
	}
	ticks, _ := format.Axis(lw-(rangeW+1), 5*time.Minute)
	if !strings.HasPrefix(lines[16], heatIndent+ticks+" ") {
		t.Errorf("axis should span %d columns: %q", lw-(rangeW+1), lines[16])
	}
}
