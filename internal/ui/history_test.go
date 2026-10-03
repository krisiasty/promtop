package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/krisiasty/promtop/internal/format"
)

const historyHeader = "   AGE  TIME             VALUE          Δ       Δ%  RANGE (window)"

func TestHistoryLayout(t *testing.T) {
	f := newFixture(t, fixtureSteps)
	m := f.model(146, 36)
	m.view = ViewHistory
	out := m.render()
	assertFrame(t, out, 146, 36)
	lines := plainLines(out)
	if !strings.HasPrefix(lines[3], "go_goroutines  window 5m · 300 samples · gauge") {
		t.Errorf("title = %q", lines[3])
	}
	if !strings.HasPrefix(lines[5], historyHeader) || !strings.HasPrefix(lines[6], "   now  12:36:59 ") || !strings.HasPrefix(lines[7], "   -1s  12:36:58 ") {
		t.Errorf("rows:\n%s", strings.Join(lines[5:8], "\n"))
	}
	if i := strings.Index(lines[4], "╭─ stats · window 5m ─"); i < 0 || format.Width(lines[4][:i]) != 83 {
		t.Errorf("stats panel = %q", lines[4])
	}
	if !strings.Contains(lines[5], "│ current ") || !strings.Contains(lines[12], "╭─ distribution · window ─") {
		t.Errorf("panels:\n%s", strings.Join(lines[4:14], "\n"))
	}
	if !strings.HasPrefix(lines[32], "  -26s  ") || !strings.Contains(lines[33], "▼ 273 more") {
		t.Errorf("first 27 of 300 rows expected (screen rows 6..32): %q", lines[32])
	}
	if !strings.HasPrefix(lines[34], "▸ go_goroutines") {
		t.Errorf("status = %q", lines[34])
	}

	m = f.model(80, 36)
	m.view = ViewHistory
	lines = plainLines(m.render())
	if !strings.HasPrefix(lines[3], "go_goroutines  window 5m · 300 · gauge  enter stats") || !strings.HasPrefix(lines[5], historyHeader) {
		t.Errorf("narrow:\n%s", strings.Join(lines[3:6], "\n"))
	}
	press(m, "down")
	lines = plainLines(m.render())
	if !strings.HasPrefix(lines[6], "   -1s  ") || !strings.Contains(lines[2], "▲ 1 more") {
		t.Errorf("down scrolls to older samples: %q / %q", lines[2], lines[6])
	}
	press(m, "pgdown")
	lines = plainLines(m.render())
	if !strings.HasPrefix(lines[6], "  -28s  ") {
		t.Errorf("pgdown scrolls one page: %q", lines[6])
	}
	press(m, "g")
	if m.histOff != 0 {
		t.Errorf("g returns to newest sample: %d", m.histOff)
	}
	press(m, "+")
	lines = plainLines(m.render())
	if !strings.HasPrefix(lines[3], "go_goroutines  window 10m · 600 · gauge") || m.histOff != 0 {
		t.Errorf("+ changes the window and resets scroll: %q / %d", lines[3], m.histOff)
	}
	press(m, "down", "]")
	if m.selID != "go_threads" || m.histOff != 0 {
		t.Errorf("] selects the next series and resets scroll: %q / %d", m.selID, m.histOff)
	}
	press(m, "[")
	if m.selID != "go_goroutines" {
		t.Errorf("[ selects the previous series: %q", m.selID)
	}
}

func TestHistoryFillsTallTerminal(t *testing.T) {
	f := newFixture(t, fixtureSteps)
	m := f.model(146, 60)
	m.view = ViewHistory
	lines := plainLines(m.render())
	if !strings.HasPrefix(lines[3], "go_goroutines  window 5m · 300 samples · gauge") ||
		!strings.HasPrefix(lines[56], "  -50s  ") {
		t.Errorf("60-row History should show 51 of 300 samples: title %q, last row %q", lines[3], lines[56])
	}
	press(m, "G")
	lines = plainLines(m.render())
	if !strings.HasPrefix(lines[6], " -4m9s  ") || !strings.HasPrefix(lines[56], "-4m59s  ") {
		t.Errorf("G should reach the oldest page: first %q, last %q", lines[6], lines[56])
	}
	press(m, "pgup")
	lines = plainLines(m.render())
	if !strings.HasPrefix(lines[6], "-3m18s  ") {
		t.Errorf("pgup should move a full page toward newest: %q", lines[6])
	}
}

func TestHistoryGapRow(t *testing.T) {
	f := newFixture(t, 30)
	f.fail(3)
	f.step()
	m := f.model(146, 36)
	m.view = ViewHistory
	lines := plainLines(m.render())
	if i := strings.Index(lines[7], "⋯ 3 scrapes failed"); i < 0 || format.Width(lines[7][:i]) != 52 {
		t.Errorf("gap row = %q", lines[7])
	}
	if !strings.HasPrefix(lines[8], "   -4s  ") {
		t.Errorf("row after the gap = %q", lines[8])
	}
}

func TestHistoryPanelsUseEntireScrollableWindow(t *testing.T) {
	f := newFixture(t, 0)
	f.apply("value 100\n")
	for range 39 {
		f.apply("value 1\n")
	}
	m := f.model(146, 24)
	m.view = ViewHistory
	text := plain(m.render())
	if !strings.Contains(text, "window 5m · 40 samples") || !strings.Contains(text, "max                      100") {
		t.Errorf("the right panel must include older, scrollable values:\n%s", text)
	}
	press(m, "G")
	text = plain(m.render())
	if !strings.Contains(text, "value  window 5m · 40 samples") || !strings.Contains(text, "max                      100") {
		t.Errorf("scrolling must keep the right panel on the same window:\n%s", text)
	}
	press(m, "-")
	for range 3 {
		press(m, "-")
	}
	text = plain(m.render())
	if m.window != 30*time.Second || !strings.Contains(text, "window 30s · 30 samples") ||
		!strings.Contains(text, "max                     1.00") {
		t.Errorf("a shorter window must update the list and panels together:\n%s", text)
	}
}

// historyText renders series id in History and returns the plain frame.
func historyText(f *fixture, id string) string {
	m := f.model(146, 36)
	m.view, m.selID = ViewHistory, id
	return plain(m.render())
}

func TestHistoryGapRowsCountOnlyFailedScrapes(t *testing.T) {
	f := newFixture(t, 0)
	for _, v := range []string{"3", "7", "NaN", "NaN", "9"} {
		f.apply("# TYPE g gauge\ng " + v + "\nh 1\n")
	}
	f.apply("h 1\n") // g is absent
	f.apply("# TYPE g gauge\ng 4\nh 1\n")
	f.n += 10 // paused: no attempts were made
	f.apply("# TYPE g gauge\ng 5\nh 1\n")
	if text := historyText(f, "g"); strings.Contains(text, "failed") {
		t.Errorf("exposed NaN, absent samples and pauses are not failed scrapes:\n%s", text)
	}
}

func TestHistoryGapRowCountsAttemptsDuringBackoff(t *testing.T) {
	f := newFixture(t, 30)
	f.fail(7)
	f.n += 23 // backoff: 7 attempts over 30s
	f.step()
	if text := historyText(f, "go_goroutines"); !strings.Contains(text, "⋯ 7 scrapes failed") {
		t.Errorf("gap row must count the failed attempts:\n%s", text)
	}
}
