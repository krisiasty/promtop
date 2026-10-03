package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/krisiasty/promtop/internal/config"
	"github.com/krisiasty/promtop/internal/demo"
	"github.com/krisiasty/promtop/internal/expo"
	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/scrape"
	"github.com/krisiasty/promtop/internal/store"
)

func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

const (
	fixtureURL   = "http://10.0.3.14:9100/metrics"
	fixtureSteps = 901 // 15 minutes at 1s; the clock ends at 12:36:59
	refusedFull  = "dial tcp 10.0.3.14:9100: connect: connection refused"
)

var fixtureBase = time.Date(2026, 9, 26, 12, 21, 59, 0, time.UTC)

var defaultPins = []string{"go_goroutines", "process_resident_memory_bytes", `http_requests_total{code="200",method="GET"}`,
	"process_cpu_seconds_total", "cache_hits_total", `queue_depth{queue="emails"}`}

type fixture struct {
	t   testing.TB
	st  *store.Store
	gen *demo.Target
	n   int
	now time.Time
}

func newFixture(t testing.TB, steps int) *fixture {
	t.Helper()
	f := &fixture{t: t, st: store.New(time.Second), gen: demo.New(1), now: fixtureBase}
	for range steps {
		f.step()
	}
	return f
}

func (f *fixture) at() time.Time { return fixtureBase.Add(time.Duration(f.n) * time.Second) }

func (f *fixture) step() { f.apply(f.gen.Step()) }

func (f *fixture) apply(text string) {
	f.t.Helper()
	p, err := expo.Parse(strings.NewReader(text), nil)
	if err != nil {
		f.t.Fatal(err)
	}
	at := f.at()
	f.st.Apply(&scrape.Result{Parsed: p, Status: "200 OK", ContentType: "text/plain; version=0.0.4",
		Bytes: len(text), Duration: 150 * time.Millisecond, At: at})
	f.now, f.n = at, f.n+1
}

func (f *fixture) fail(n int) {
	for range n {
		at := f.at()
		f.st.Fail(&scrape.Error{Kind: scrape.KindRefused, Full: refusedFull, Short: "connection refused",
			Attempt: "connection refused", Duration: time.Millisecond, At: at})
		f.now, f.n = at, f.n+1
	}
}

func testConfig() config.Config {
	return config.Config{Target: config.Target{URL: fixtureURL, Timeout: 2 * time.Second}, Interval: time.Second, Window: 5 * time.Minute}
}

func (f *fixture) model(w, h int) *Model {
	m := New(testConfig(), Options{Store: f.st, Now: func() time.Time { return f.now }})
	m.w, m.h = w, h
	for _, id := range defaultPins {
		f.st.SetPinned(id, true)
	}
	m.cursor = 1
	return m
}

func plainLines(out string) []string {
	lines := strings.Split(plain(out), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return lines
}

func plainFrame(out string) string { return strings.Join(plainLines(out), "\n") + "\n" }

func assertFrame(t *testing.T, out string, w, h int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("frame has %d lines, want %d", len(lines), h)
	}
	for i, ln := range lines {
		if got := format.Width(ln); got != w {
			t.Fatalf("line %d is %d cells, want %d: %q", i, got, w, plain(ln))
		}
	}
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func press(m *Model, keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = m.Update(key(k))
	}
	return cmd
}

func rowIndex(m *Model, id string) int {
	for i, s := range m.rows() {
		if s.ID == id {
			return i
		}
	}
	return -1
}

func familyIndex(m *Model, name string) int {
	for i, f := range m.st.Families() {
		if f.Name == name {
			return i
		}
	}
	return -1
}
