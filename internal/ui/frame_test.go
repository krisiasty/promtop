package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/krisiasty/promtop/internal/format"
)

func TestHeaderNarrowing(t *testing.T) {
	f := newFixture(t, 10)
	m := f.model(146, 36)
	samples := format.Int(f.st.Scrape.Samples)
	line := plain(m.header(146))
	want := " promtop   http://10.0.3.14:9100/metrics  ● live  every 1s · 150ms · " + samples + " samples · scrape #10"
	if !strings.HasPrefix(line, want) || !strings.HasSuffix(line, "12:22:08") || format.Width(line) != 146 {
		t.Errorf("146 header = %q", line)
	}
	line = plain(m.header(100))
	if !strings.HasPrefix(line, " promtop   http://10.0.3.14:9100/metrics  ● live  every 1s · 150ms · "+samples+" samples ") ||
		strings.Contains(line, "scrape #") {
		t.Errorf("100 header = %q", line)
	}
	line = plain(m.header(80))
	if !strings.HasPrefix(line, " promtop   http://10.0.3.14:9100/metrics  ● live  every 1s · 150ms ") || !strings.HasSuffix(line, "12:22:08") {
		t.Errorf("80 header = %q", line)
	}
	m.paused = true
	if !strings.Contains(plain(m.header(146)), "❚❚ paused") {
		t.Error("paused header")
	}
}

func TestTabsAndSeparators(t *testing.T) {
	m := newFixture(t, 3).model(146, 36)
	if got := strings.TrimRight(plain(m.tabs(146)), " "); got != " 1 Table   2 History   3 Graph   4 Heatmap   5 Series   6 Raw" {
		t.Errorf("tabs = %q", got)
	}
	if got := plain(m.separator(146, "▼", 54)); got != strings.Repeat("─", 134)+" ▼ 54 more ─" {
		t.Errorf("separator = %q", got)
	}
	if got := plain(m.separator(80, "▲", 20056)); got != strings.Repeat("─", 64)+" ▲ 20,056 more ─" {
		t.Errorf("separator = %q", got)
	}
	if got := plain(m.separator(20, "▲", 0)); got != strings.Repeat("─", 20) {
		t.Errorf("plain separator = %q", got)
	}
}

func TestHintsDropFromTheEnd(t *testing.T) {
	f := newFixture(t, 3)
	m := f.model(146, 36)
	cases := map[int]string{
		146: "↑ ↓ move   PgUp/Dn page   +/- window   space pin   a all/pinned   / filter   s sort   r rate   enter details   p pause   ← → view   q quit",
		100: "↑ ↓ move   PgUp/Dn page   +/- window   space pin   a all/pinned   p pause   ← → view   q quit",
		80:  "↑ ↓ move   PgUp/Dn page   +/- window   p pause   ← → view   q quit",
	}
	for w, want := range cases {
		if got := strings.TrimRight(plain(m.hintLine(w)), " "); got != want {
			t.Errorf("hints @%d = %q\nwant       %q", w, got, want)
		}
	}
	f.fail(1)
	if got := plain(m.hintLine(146)); !strings.HasPrefix(got, "e error   ↑ ↓ move") {
		t.Errorf("failing hints = %q", got)
	}
}

func TestBanners(t *testing.T) {
	f := newFixture(t, 10)
	f.fail(3)
	m := f.model(146, 36)
	line := plain(m.bannerLine(146, m.banner(146)))
	if !strings.HasPrefix(line, " ⚠ 3 failed scrapes: "+refusedFull+" · last success 12:22:08 · retrying every 1s ") ||
		!strings.HasSuffix(line, "e details ") || format.Width(line) != 146 {
		t.Errorf("degraded banner = %q", line)
	}
	f.fail(4)
	if got := m.banner(80).text; got != "✕ target down: connection refused · retry in 8s" {
		t.Errorf("down banner @80 = %q", got)
	}
	if got := plain(m.header(146)); !strings.Contains(got, "✕ down  last ok 7s ago · 7 failed · retry in 8s") {
		t.Errorf("down header = %q", got)
	}

	r := newFixture(t, 10)
	r.gen.Restart()
	r.step()
	rm := r.model(146, 36)
	got := rm.banner(146).text
	if !strings.HasPrefix(got, "↺ target restarted 12:22:09 (process_start_time_seconds changed) · ") ||
		!strings.HasSuffix(got, " counters reset · rates computed across the reset") {
		t.Errorf("reset banner = %q", got)
	}
	if newFixture(t, 3).model(146, 36).banner(146) != nil {
		t.Error("healthy target has no banner")
	}
}

func TestStatusLineRules(t *testing.T) {
	m := newFixture(t, 3).model(146, 36)
	m.filtering, m.filter = true, "http"
	if got := plain(m.statusLine(146, body{}, 3)); !strings.HasPrefix(got, "/http▌") {
		t.Errorf("typing = %q", got)
	}
	m.filtering = false
	got := plain(m.statusLine(146, body{}, 3))
	if !strings.HasPrefix(got, `filter "http" · 3 match · esc to clear`) || !strings.HasSuffix(got, "window 5m · counters as rate/s") {
		t.Errorf("filtered = %q", got)
	}
	m.filter, m.showRate = "", false
	if got := plain(m.statusLine(146, body{}, 3)); !strings.HasSuffix(got, "counters raw") {
		t.Errorf("raw = %q", got)
	}
	if got := strings.TrimRight(plain(m.statusLine(80, body{status: strings.Repeat("x", 60)}, 0)), " "); got != strings.Repeat("x", 60) {
		t.Errorf("right side must drop first: %q", got)
	}
	if got := plain(m.statusLine(80, body{status: strings.Repeat("x", 100)}, 0)); got != strings.Repeat("x", 77)+"…  " {
		t.Errorf("left side truncates to w-2: %q", got)
	}
}

func TestSpecialScreens(t *testing.T) {
	m := New(testConfig(), Options{Now: func() time.Time { return fixtureBase }})
	m.w, m.h = 60, 20
	out := m.render()
	assertFrame(t, out, 60, 20)
	if !strings.Contains(plain(out), "terminal too small · need 80×24, have 60×20") {
		t.Error("too small message missing")
	}
	m.w, m.h = 146, 36
	out = plain(m.render())
	if !strings.Contains(out, "● connecting") || !strings.Contains(out, "waiting for the first successful scrape of "+fixtureURL) {
		t.Errorf("connecting screen:\n%s", out)
	}
}

func TestEmptyResponsePanel(t *testing.T) {
	f := newFixture(t, 3)
	f.apply("")
	m := f.model(146, 36)
	lines := plainLines(m.render())
	if !strings.Contains(lines[0], "● no data  HTTP 200 · text/plain; version=0.0.4 · 0 bytes · 0 samples") {
		t.Errorf("header = %q", lines[0])
	}
	found := false
	for _, ln := range lines {
		if strings.Index(ln, "╭─ no metrics ─") == 34 {
			found = true
		}
	}
	if !found || !strings.HasPrefix(lines[34], "waiting for series · still scraping every 1s") {
		t.Errorf("empty panel not centred / status wrong:\n%s", strings.Join(lines, "\n"))
	}
}
