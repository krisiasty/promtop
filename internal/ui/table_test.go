package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/krisiasty/promtop/internal/format"
)

func TestTableColumns(t *testing.T) {
	f := newFixture(t, 30)
	cases := map[int]string{
		146: "   NAME ▲                              TYP     CURRENT      PREV         Δ      MIN      MAX      AVG      P50      P95      P99  TREND",
		100: "   NAME ▲                              TYP     CURRENT         Δ      MIN      MAX      AVG      P99",
		80:  "   NAME ▲                            TYP     CURRENT         Δ      AVG      P99",
	}
	for w, want := range cases {
		m := f.model(w, 36)
		out := m.render()
		assertFrame(t, out, w, 36)
		lines := plainLines(out)
		if lines[3] != want {
			t.Errorf("@%d header\n got %q\nwant %q", w, lines[3], want)
		}
		if !strings.HasPrefix(lines[4], "   up ") || !strings.HasPrefix(lines[5], "▸● go_goroutines ") || !strings.HasPrefix(lines[6], "   go_threads ") {
			t.Errorf("@%d first rows:\n%s", w, strings.Join(lines[4:7], "\n"))
		}
		if !strings.Contains(lines[4], " gau ") {
			t.Errorf("@%d type column: %q", w, lines[4])
		}
	}
	lines := plainLines(f.model(146, 36).render())
	if !strings.HasPrefix(lines[11], ` ● http_requests_total{method="GET",c… ctr `) {
		t.Errorf("label truncation @146: %q", lines[11])
	}
	lines = plainLines(f.model(80, 36).render())
	if !strings.HasPrefix(lines[11], ` ● http_requests_total{method="GET"… ctr `) {
		t.Errorf("label truncation @80: %q", lines[11])
	}
}

func TestTableScrollAndStatus(t *testing.T) {
	f := newFixture(t, 30)
	m := f.model(146, 36)
	n := len(m.rows())
	lines := plainLines(m.render())
	if want := fmt.Sprintf("%d/%d series · 6 pinned · sort name · rows 1–29 of %d", n, n, n); !strings.HasPrefix(lines[34], want) {
		t.Errorf("status = %q, want prefix %q", lines[34], want)
	}
	if want := fmt.Sprintf(" ▼ %d more ─", n-29); !strings.HasSuffix(lines[33], want) {
		t.Errorf("bottom marker = %q", lines[33])
	}
	press(m, "G")
	lines = plainLines(m.render())
	if want := fmt.Sprintf(" ▲ %d more ─", n-29); !strings.HasSuffix(lines[2], want) || !strings.HasPrefix(lines[32], "▸") {
		t.Errorf("after G: top %q, last row %q", lines[2], lines[32])
	}
}

func TestTableSortAndFilter(t *testing.T) {
	f := newFixture(t, 30)
	m := f.model(146, 36)
	press(m, "s")
	lines := plainLines(m.render())
	if !strings.Contains(lines[3], "CURRENT ▼") || !strings.HasPrefix(lines[3], "   NAME   ") {
		t.Errorf("sort indicator: %q", lines[3])
	}
	rows := m.rows()
	if m.stats(rows[0]).Cur < m.stats(rows[1]).Cur {
		t.Error("current sort must be descending")
	}
	press(m, "s")
	lines = plainLines(m.render())
	if !strings.Contains(lines[3], "Δ ▼") || strings.Contains(lines[3], "Δ%") || !strings.Contains(lines[34], "sort Δ%") {
		t.Errorf("absolute delta heading and percentage sort status: %q / %q", lines[3], lines[34])
	}
	press(m, "s", "s", "/", "q", "u", "e", "u", "e", "enter")
	lines = plainLines(m.render())
	if !strings.HasPrefix(lines[34], `filter "queue" · 4 match · esc to clear`) {
		t.Errorf("filter status = %q", lines[34])
	}
	press(m, "/", "z", "z", "enter")
	lines = plainLines(m.render())
	if lines[5] != "   No series match. Esc clears the filter, a shows all series." {
		t.Errorf("empty filter result = %q", lines[5])
	}
}

func TestTableStaleAndReset(t *testing.T) {
	f := newFixture(t, 30)
	m := f.model(146, 36)
	s := m.rows()[0]
	c := tableLayout(146)
	fresh := m.tableRow(s, false, false, f.now, c, 146)
	stale := m.tableRow(s, false, true, f.now, c, 146)
	if plain(fresh) != plain(stale) || fresh == stale {
		t.Error("stale rows must differ only in styling (faint)")
	}
	f.gen.Restart()
	f.step()
	lines := plainLines(m.render())
	cpu := lines[11] // process_cpu_seconds_total (banner shifts body by 2)
	if !strings.Contains(cpu, "process_cpu_seconds_total") || !strings.Contains(cpu, "↺ reset") {
		t.Errorf("reset marker: %q", cpu)
	}
	if !strings.HasPrefix(lines[6], "   up ") || strings.Contains(lines[6], "↺ reset") {
		t.Error("gauges never show ↺ reset")
	}
}

func TestTableWideLabelKeepsWidth(t *testing.T) {
	f := newFixture(t, 3)
	f.apply("cjk{name=\"日本語のラベル値がとても長い場合\"} 1\nemoji{e=\"🔥🔥🔥🔥🔥🔥🔥🔥🔥🔥🔥🔥🔥🔥🔥🔥\"} 2\nup 1\n")
	for _, w := range []int{80, 100, 146} {
		out := f.model(w, 36).render()
		assertFrame(t, out, w, 36)
		for _, ln := range strings.Split(out, "\n") {
			if format.Width(ln) != w {
				t.Fatalf("@%d wide label broke the row width", w)
			}
		}
	}
}
