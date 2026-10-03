package ui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/krisiasty/promtop/internal/format"
)

func TestGraphLayout(t *testing.T) {
	f := newFixture(t, fixtureSteps)
	m := f.model(146, 36)
	m.view = ViewGraph
	out := m.render()
	assertFrame(t, out, 146, 36)
	lines := plainLines(out)
	if !strings.HasPrefix(lines[3], "go_goroutines  gauge · window 5m ") {
		t.Errorf("title = %q", lines[3])
	}
	for _, row := range []int{0, 5, 11, 16, 21} { // labelled chart rows
		if ln := lines[5+row]; strings.Index(ln, " ┤") != 10 {
			t.Errorf("chart row %d not labelled: %q", row, ln)
		}
	}
	if !strings.HasPrefix(lines[6], strings.Repeat(" ", 11)+"│") {
		t.Errorf("unlabelled row = %q", lines[6])
	}
	ticks, _ := format.Axis(132, 5*time.Minute)
	if lines[27] != strings.Repeat(" ", 11)+"└"+ticks {
		t.Errorf("x axis = %q", lines[27])
	}
	wantLabels := "            -5m                           -3m45s                           -2m30s                          -1m15s                            now"
	if lines[28] != wantLabels {
		t.Errorf("x labels\n got %q\nwant %q", lines[28], wantLabels)
	}
	if !strings.HasPrefix(lines[30], "            cur ") || !strings.Contains(lines[30], "    Δ ") || !strings.Contains(lines[30], "    p99 ") {
		t.Errorf("legend = %q", lines[30])
	}

	m = f.model(80, 36)
	m.view = ViewGraph
	lines = plainLines(m.render())
	if format.Width(lines[27]) != 12+66 || format.Width(lines[30]) > 80 || !strings.HasPrefix(lines[30], "            cur ") {
		t.Errorf("80 cols: axis %d cells, legend %q (cut at the edge)", format.Width(lines[27]), lines[30])
	}
	f.fail(1)
	m = f.model(146, 36)
	m.view = ViewGraph
	lines = plainLines(m.render())
	// With a banner the title is screen row 5, the chart starts at row 7 and
	// has 20 rows, so its last (labelled) row is screen row 26.
	if strings.Index(lines[7+19], " ┤") != 10 || strings.Index(lines[7+20], "└") != 11 {
		t.Errorf("banner must shrink the chart to 20 rows:\n%s\n%s", lines[7+19], lines[7+20])
	}
}

func TestGraphUsesWideTerminal(t *testing.T) {
	f := newFixture(t, fixtureSteps)
	for _, w := range []int{200, 240, 320} {
		m := f.model(w, 36)
		m.view = ViewGraph
		out := m.render()
		assertFrame(t, out, w, 36)
		lines := plainLines(out)
		ticks, labels := format.Axis(w-14, 5*time.Minute)
		if want := strings.Repeat(" ", 11) + "└" + ticks; lines[27] != want {
			t.Errorf("%d-column x axis ends at %d, want %d", w, format.Width(lines[27]), format.Width(want))
		}
		if want := strings.Repeat(" ", 12) + labels; lines[28] != want {
			t.Errorf("%d-column x labels end at %d, want %d", w, format.Width(lines[28]), format.Width(want))
		}
	}
}

func TestGraphPlacesStartupAndGapAtScrapeTimes(t *testing.T) {
	f := newFixture(t, 0)
	f.apply("# TYPE g gauge\ng 5\n")
	m := f.model(146, 36)
	m.view = ViewGraph
	plotCells := func() []bool {
		lines := plainLines(m.render())
		cells := make([]bool, 132)
		for _, line := range lines[5:27] {
			runes := []rune(line)
			for c := range cells {
				cells[c] = cells[c] || runes[12+c] != 0x2800
			}
		}
		return cells
	}
	for c, plotted := range plotCells() {
		if c < 131 && plotted || c == 131 && !plotted {
			t.Errorf("startup sample at chart cell %d: plotted = %v", c, plotted)
		}
	}
	f.n = 240 // next successful scrape is four minutes later
	f.apply("# TYPE g gauge\ng 7\n")
	for c, plotted := range plotCells() {
		if (c == 26 || c == 131) != plotted {
			t.Errorf("scrapes four minutes apart at chart cell %d: plotted = %v", c, plotted)
		}
	}
}

func TestHistogramGraphUsesPlottedIntervalMeansForScale(t *testing.T) {
	f := newFixture(t, 0)
	f.apply(`# TYPE h_seconds histogram
h_seconds_bucket{le="1"} 1
h_seconds_bucket{le="+Inf"} 2
h_seconds_sum 6
h_seconds_count 2
`)
	f.apply(`# TYPE h_seconds histogram
h_seconds_bucket{le="1"} 1
h_seconds_bucket{le="+Inf"} 4
h_seconds_sum 14
h_seconds_count 4
`)
	m := f.model(146, 36)
	m.view = ViewGraph
	if !math.IsInf(m.stats(m.st.Series()[0]).Max, 1) {
		t.Fatal("histogram observation range should still include the +Inf bucket")
	}
	lines := plainLines(m.render())
	if !strings.Contains(lines[3], "histogram · interval mean") || !strings.Contains(lines[30], "cur 4.00s") {
		t.Errorf("Graph should describe the plotted interval mean: %q / %q", lines[3], lines[30])
	}
	for _, line := range lines[5:27] {
		if strings.Contains(line, "+Inf") || strings.Contains(line, "-Inf") {
			t.Errorf("Graph axis must be finite for finite interval means: %q", line)
		}
	}
}
