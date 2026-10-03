package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/store"
)

func cellOf(line, sub string) int {
	i := strings.Index(line, sub)
	if i < 0 {
		return -1
	}
	return format.Width(line[:i])
}

func TestSeriesWide(t *testing.T) {
	f := newFixture(t, 120)
	m := f.model(146, 36)
	m.view = ViewSeries
	m.famIdx = familyIndex(m, "http_requests_total")
	out := m.render()
	assertFrame(t, out, 146, 36)
	lines := plainLines(out)
	if !strings.HasPrefix(lines[3], "╭─ families · 8/") || cellOf(lines[3], "╮") != 43 {
		t.Errorf("families box top = %q", lines[3])
	}
	selected := false
	for _, ln := range lines[4:30] {
		if strings.HasPrefix(ln, "│▸ http_requests_total ") && strings.Contains(ln, " ctr ") {
			selected = true
		}
	}
	if !selected {
		t.Error("selected family row missing")
	}
	if cellOf(lines[4], "http_requests_total  counter  8 series") != 47 ||
		cellOf(lines[5], "# Total HTTP requests processed, by method and status code.") != 47 ||
		cellOf(lines[7], "╭─ label cardinality ─") != 47 {
		t.Errorf("right pane:\n%s", strings.Join(lines[4:8], "\n"))
	}
	if !strings.Contains(lines[8], "│ method        4   ████        GET POST PUT DELETE") ||
		!strings.Contains(lines[9], "│ code          5   █████       200 404 500 400 204") {
		t.Errorf("label rows:\n%s\n%s", lines[8], lines[9])
	}
	if !strings.Contains(lines[12], "╭─ rate(http_requests_total[1m]) by method × code ─") ||
		!strings.Contains(lines[13], "│ method\\code      200       204       400       404       500          Σ │") {
		t.Errorf("pivot head:\n%s\n%s", lines[12], lines[13])
	}
	var del, sum string
	for _, ln := range lines[14:22] {
		if strings.Contains(ln, "│ DELETE ") {
			del = ln
		}
		if strings.Contains(ln, "│ Σ ") {
			sum = ln
		}
	}
	if strings.Count(del, "·") != 4 || !strings.HasSuffix(strings.TrimRight(sum, " "), "/s │") {
		t.Errorf("pivot rows: %q / %q", del, sum)
	}
	if !strings.Contains(lines[30], " more ─╯") {
		t.Errorf("families bottom marker = %q", lines[30])
	}
	if want := " families · "; !strings.Contains(lines[34], want) {
		t.Errorf("status = %q", lines[34])
	}
}

func TestSeriesNarrow(t *testing.T) {
	f := newFixture(t, 120)
	m := f.model(80, 36)
	m.view = ViewSeries
	m.famIdx = familyIndex(m, "http_requests_total")
	lines := plainLines(m.render())
	found := map[string]bool{}
	for _, ln := range lines[4:30] {
		if strings.HasPrefix(ln, "│▸ http_requests_total ") && strings.Contains(ln, "ctr  method, code") && strings.HasSuffix(ln, "8│") {
			found["http"] = true
		}
		if strings.Contains(ln, "http_request_duration_seconds") && strings.Contains(ln, "his  le ") {
			found["hist"] = true
		}
	}
	if !found["http"] || !found["hist"] {
		t.Errorf("narrow list rows missing: %v\n%s", found, strings.Join(lines[3:31], "\n"))
	}
	press(m, "enter")
	lines = plainLines(m.render())
	if lines[3] != "esc ‹ families" || !strings.HasPrefix(lines[4], "http_requests_total  counter  8 series") ||
		!strings.HasPrefix(lines[6], "╭─ label cardinality ─") || !strings.HasPrefix(lines[10], "╭─ rate(http_requests_total[1m]) by method × code ─") ||
		!strings.HasPrefix(lines[11], "│ method\\code      200") {
		t.Errorf("detail:\n%s", strings.Join(lines[3:12], "\n"))
	}
	press(m, "down")
	lines = plainLines(m.render())
	if !strings.HasPrefix(lines[4], "http_request_duration_seconds  histogram  13 series") || !strings.Contains(plain(m.render()), "rate(_bucket[1m]) — cumulative") ||
		!strings.Contains(plain(m.render()), `│ le="0.005" `) {
		t.Errorf("histogram detail:\n%s", strings.Join(lines[3:12], "\n"))
	}
	press(m, "esc")
	if m.famOpen {
		t.Error("esc returns to the list")
	}
	m.famIdx = familyIndex(m, "go_gc_duration_seconds")
	press(m, "enter")
	all := plain(m.render())
	if !strings.Contains(all, "series by quantile") || !strings.Contains(all, `quantile="0.25"`) || !strings.Contains(all, "go_gc_duration_seconds_sum") {
		t.Errorf("summary family list:\n%s", all)
	}
}

func TestSeriesUsesWideTerminal(t *testing.T) {
	const name = "ghw_agent_http_request_last_duration_seconds"
	f := newFixture(t, 0)
	f.apply("# TYPE " + name + " gauge\n" + name +
		`{endpoint="GET /metrics/with/a/long/path",request_listener_identifier="operations",method="GET"} 0.00238` +
		"\n# TYPE ghw_agent_http_request_last_success_timestamp_seconds gauge\n" +
		"ghw_agent_http_request_last_success_timestamp_seconds 123\n")
	m := f.model(260, 36)
	m.view = ViewSeries
	paneW := familyPaneWidth(m.w, m.st.Families())
	if paneW <= familyPaneW {
		t.Fatalf("wide terminal kept the %d-cell family pane: %d", familyPaneW, paneW)
	}
	lines := plainLines(m.render())
	assertFrame(t, m.render(), 260, 36)
	left := format.Cut(lines[4], paneW)
	if !strings.Contains(left, name) || strings.Contains(left, "…") {
		t.Errorf("family name clipped at %d cells: %q", paneW, lines[4])
	}
	if !strings.Contains(plain(m.labelBox(m.st.Families()[0], 260-paneW-3)[2]), "request_listener_identifier") {
		t.Error("label cardinality box clipped a label name")
	}
	list := m.seriesListBox(m.st.Families()[0], 260-paneW-3)
	row := []rune(plain(list[1]))
	if !strings.Contains(string(row), `endpoint="GET /metrics/with/a/long/path"`) || len(row) < 3 || row[len(row)-3] == ' ' {
		t.Errorf("series label or trend clipped: %q", string(row))
	}
	click(m, familyPaneW+5, m.bodyTop()+2)
	if m.famIdx != 1 {
		t.Errorf("click inside expanded family pane selected row %d, want 1", m.famIdx)
	}
	m = f.model(800, 36)
	m.view = ViewSeries
	assertFrame(t, m.render(), 800, 36)
	if !strings.Contains(format.Cut(plainLines(m.render())[4], familyPaneWidth(800, m.st.Families())), name) {
		t.Error("family name clipped on an extra-wide terminal")
	}

	p := newFixture(t, 120).model(260, 36)
	p.view = ViewSeries
	p.famIdx = familyIndex(p, "http_requests_total")
	for _, w := range []int{260, 800} {
		p.w = w
		rows := plainLines(p.render())
		if got := format.Width(strings.TrimRight(rows[12], " ")); got != w {
			t.Errorf("%d-column pivot box ends at cell %d: %q", w, got, rows[12])
		}
	}
}

func TestSeriesOneMinuteRatesWithThirtySecondWindow(t *testing.T) {
	f := newFixture(t, 0)
	f.apply(`# TYPE requests_total counter
requests_total{method="GET",code="200"} 10
# TYPE jobs_total counter
jobs_total{queue="alpha"} 20
# TYPE latency_seconds histogram
latency_seconds_bucket{le="1"} 30
latency_seconds_bucket{le="+Inf"} 40
latency_seconds_sum 5
latency_seconds_count 40
`)
	f.n = 60
	f.apply(`# TYPE requests_total counter
requests_total{method="GET",code="200"} 70
# TYPE jobs_total counter
jobs_total{queue="alpha"} 80
# TYPE latency_seconds histogram
latency_seconds_bucket{le="1"} 90
latency_seconds_bucket{le="+Inf"} 100
latency_seconds_sum 15
latency_seconds_count 100
`)
	m := f.model(146, 36)
	m.window = 30 * time.Second
	families := m.st.Families()
	for _, tc := range []struct {
		name, family string
		box          func(*store.FamilyInfo) []string
		rateRows     int
	}{
		{"counter pivot", "requests_total", func(fi *store.FamilyInfo) []string { return m.pivotBox(fi, 80) }, 1},
		{"counter list", "jobs_total", func(fi *store.FamilyInfo) []string { return m.seriesListBox(fi, 80) }, 1},
		{"histogram buckets", "latency_seconds", func(fi *store.FamilyInfo) []string { return m.bucketRateBox(fi, 80) }, 2},
	} {
		var fi *store.FamilyInfo
		for _, family := range families {
			if family.Name == tc.family {
				fi = family
				break
			}
		}
		if fi == nil {
			t.Fatalf("%s family missing", tc.name)
		}
		text := plain(strings.Join(tc.box(fi), "\n"))
		if got := strings.Count(text, "1.00/s"); got != tc.rateRows {
			t.Errorf("%s should use the sample exactly one minute old:\n%s", tc.name, text)
		}
	}
}

func TestSeriesFamilyBucketRateIgnoresLabelSetChurn(t *testing.T) {
	f := newFixture(t, 0)
	series := func(label string, count int) string {
		return fmt.Sprintf("h_bucket{instance=\"%s\",le=\"+Inf\"} %d\nh_sum{instance=\"%s\"} %d\nh_count{instance=\"%s\"} %d\n",
			label, count, label, count, label, count)
	}
	f.apply("# TYPE h histogram\n" + series("a", 100) + series("b", 100))
	f.apply("# TYPE h histogram\n" + series("a", 110))
	f.apply("# TYPE h histogram\n" + series("a", 120) + series("b", 110))
	m := f.model(146, 36)
	box := plain(strings.Join(m.bucketRateBox(m.st.Families()[0], 80), "\n"))
	if !strings.Contains(box, "15.0/s") || strings.Contains(box, "115/s") {
		t.Errorf("family bucket rate should sum label-set rates:\n%s", box)
	}
}

func TestSeriesPivotTotalsStayUnavailableWithoutRates(t *testing.T) {
	pivot := func(m *Model) []string {
		for _, fi := range m.st.Families() {
			if fi.Name == "requests_total" {
				return plainLines(strings.Join(m.pivotBox(fi, 80), "\n"))
			}
		}
		t.Fatal("requests_total family missing")
		return nil
	}
	fields := func(line string) []string { return strings.Fields(strings.Trim(line, "│ ")) }

	f := newFixture(t, 0)
	f.apply(`# TYPE requests_total counter
requests_total{method="GET",code="200"} 10
requests_total{method="POST",code="500"} 4
`)
	lines := pivot(f.model(146, 36))
	for i, want := range map[int][]string{2: {"GET", "—", "·", "—"}, 3: {"POST", "·", "—", "—"}, 5: {"Σ", "—", "—", "—"}} {
		if got := fields(lines[i]); !slices.Equal(got, want) {
			t.Errorf("one scrape, line %d = %q, want %q", i, got, want)
		}
	}

	f.n = 60
	f.apply(`# TYPE requests_total counter
requests_total{method="GET",code="200"} 10
requests_total{method="GET",code="500"} 3
requests_total{method="POST",code="500"} 4
`)
	lines = pivot(f.model(146, 36))
	for i, want := range map[int][]string{2: {"GET", "0", "—", "0"}, 3: {"POST", "·", "0", "0"}, 5: {"Σ", "0", "0", "0/s"}} {
		if got := fields(lines[i]); !slices.Equal(got, want) {
			t.Errorf("two scrapes, line %d = %q, want %q", i, got, want)
		}
	}
}
